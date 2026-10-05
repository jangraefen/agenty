// Package anthropictest provides a fake Anthropic Messages API for tests.
package anthropictest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// API is a fake Anthropic Messages API. It answers with queued responses and
// records every request. Point the provider's BaseURL at URL.
type API struct {
	URL string

	srv *httptest.Server

	mu        sync.Mutex
	requests  []Request
	responses []Response
}

// Request is a request the fake received.
type Request struct {
	Header http.Header
	Body   json.RawMessage
}

// Response is a response the fake sends.
type Response struct {
	Status int
	Body   string
}

// New starts a fake that answers with responses, in order, and stops it when
// the test ends. Without a queued response it answers 400.
func New(t *testing.T, responses ...Response) *API {
	t.Helper()
	f := &API{responses: responses}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, "/v1/messages", r.URL.Path)
		f.mu.Lock()
		f.requests = append(f.requests, Request{Header: r.Header.Clone(), Body: body})
		if len(f.responses) == 0 {
			f.mu.Unlock()
			http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"no response queued"}}`, http.StatusBadRequest)
			return
		}
		resp := f.responses[0]
		f.responses = f.responses[1:]
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.Status)
		_, err = io.WriteString(w, resp.Body)
		assert.NoError(t, err)
	}))
	t.Cleanup(f.srv.Close)
	f.URL = f.srv.URL
	return f
}

// Requests returns the requests received so far.
func (f *API) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

// Reply is a successful Messages API response with the given stop reason and
// content blocks.
func Reply(t *testing.T, stopReason string, content ...map[string]any) Response {
	t.Helper()
	if content == nil {
		content = []map[string]any{}
	}
	body, err := json.Marshal(map[string]any{
		"id":            "msg_1",
		"type":          "message",
		"role":          "assistant",
		"model":         "claude-test",
		"content":       content,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage":         map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
	require.NoError(t, err)
	return Response{Status: http.StatusOK, Body: string(body)}
}

// TextBlock is a text content block.
func TextBlock(text string) map[string]any {
	return map[string]any{"type": "text", "text": text}
}

// ToolUseBlock is a tool_use content block.
func ToolUseBlock(id, name string, input any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}
