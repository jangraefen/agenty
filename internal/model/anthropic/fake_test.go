package anthropic_test

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

// fakeAPI is a fake Anthropic Messages API. It answers with queued responses
// and records every request.
type fakeAPI struct {
	srv *httptest.Server

	mu        sync.Mutex
	requests  []recorded
	responses []response
}

type recorded struct {
	Header http.Header
	Body   json.RawMessage
}

type response struct {
	status int
	body   string
}

func newFakeAPI(t *testing.T, responses ...response) *fakeAPI {
	t.Helper()
	f := &fakeAPI{responses: responses}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, "/v1/messages", r.URL.Path)
		f.mu.Lock()
		f.requests = append(f.requests, recorded{Header: r.Header.Clone(), Body: body})
		if len(f.responses) == 0 {
			f.mu.Unlock()
			http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"no response queued"}}`, http.StatusBadRequest)
			return
		}
		resp := f.responses[0]
		f.responses = f.responses[1:]
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.status)
		_, err = io.WriteString(w, resp.body)
		assert.NoError(t, err)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) recorded() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.requests...)
}

// reply is a successful Messages API response with the given stop reason and
// content blocks.
func reply(t *testing.T, stopReason string, content ...map[string]any) response {
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
	return response{status: http.StatusOK, body: string(body)}
}

func textBlock(text string) map[string]any {
	return map[string]any{"type": "text", "text": text}
}

func toolUseBlock(id, name string, input any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}
