package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/api"
)

// contract is the API's spec, and a router over its paths.
type contract struct {
	doc    *openapi3.T
	router routers.Router
}

// spec loads the spec once.
var spec = sync.OnceValues(func() (contract, error) {
	doc, err := openapi3.NewLoader().LoadFromFile(filepath.Join("..", "..", "schema", "openapi.yaml"))
	if err != nil {
		return contract{}, err
	}
	if err := doc.Validate(context.Background()); err != nil {
		return contract{}, err
	}
	// Match requests to any server URL: the tests serve on random ports.
	doc.Servers = nil
	router, err := legacy.NewRouter(doc)
	return contract{doc: doc, router: router}, err
})

// eventSchemas names the schema of each server-sent event's data.
var eventSchemas = map[string]string{
	api.EventAudit:    "AuditRecord",
	api.EventApproval: "ApprovalRequest",
	api.EventFinished: "Run",
}

// checkContract fails the test unless resp, with its body, is a response
// the spec allows for req: a declared status and a body of the declared
// schema. An event stream's events are checked one by one, by checkEvent. A
// request for no route of the spec, such as a CORS preflight or a test of
// an unknown route, must be answered by a preflight's empty 204 or an Error.
func checkContract(t *testing.T, req *http.Request, resp *http.Response, body []byte) {
	t.Helper()
	c, err := spec()
	require.NoError(t, err, "the spec loads and is valid")
	route, params, err := c.router.FindRoute(req)
	var notRouted *routers.RouteError
	if errors.As(err, &notRouted) {
		if req.Method == http.MethodOptions && resp.StatusCode == http.StatusNoContent {
			assert.Empty(t, body, "a preflight answer has no body")
			return
		}
		checkSchema(t, "Error", body)
		return
	}
	require.NoError(t, err)
	in := &openapi3filter.RequestValidationInput{
		Request:    req,
		PathParams: params,
		Route:      route,
		Options:    &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	}
	out := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: in,
		Status:                 resp.StatusCode,
		Header:                 resp.Header,
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		out.Options.ExcludeResponseBody = true
	}
	out.SetBodyBytes(body)
	require.NoError(t, openapi3filter.ValidateResponse(context.Background(), out), "%s %s: %d %s", req.Method, req.URL.Path, resp.StatusCode, body)
}

// checkEvent fails the test unless a server-sent event is one the spec
// describes, with data of its schema.
func checkEvent(t *testing.T, name, data string) {
	t.Helper()
	schema, ok := eventSchemas[name]
	require.True(t, ok, "event %q is one the spec describes", name)
	checkSchema(t, schema, []byte(data))
}

// checkSchema fails the test unless body is JSON of the spec's named schema.
func checkSchema(t *testing.T, name string, body []byte) {
	t.Helper()
	c, err := spec()
	require.NoError(t, err)
	ref, ok := c.doc.Components.Schemas[name]
	require.True(t, ok, "schema %s", name)
	var v any
	require.NoError(t, json.Unmarshal(body, &v), "%s: %s", name, body)
	require.NoError(t, ref.Value.VisitJSON(v), "%s: %s", name, body)
}

// readChecked reads resp's body, checks the response against the spec, and
// returns the body for decoding.
func readChecked(t *testing.T, req *http.Request, resp *http.Response) io.Reader {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	checkContract(t, req, resp, body)
	return bytes.NewReader(body)
}

// doChecked sends req, checks the response against the spec, and returns it
// with its body, closed.
func doChecked(t *testing.T, req *http.Request) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { assert.NoError(t, resp.Body.Close()) }()
	body, err := io.ReadAll(readChecked(t, req, resp))
	require.NoError(t, err)
	return resp, body
}
