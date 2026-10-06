package server_test

import (
	"bytes"
	"context"
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
	"github.com/stretchr/testify/require"
)

// spec is the API's spec, loaded once, and a router over its paths.
var spec = sync.OnceValues(func() (routers.Router, error) {
	doc, err := openapi3.NewLoader().LoadFromFile(filepath.Join("..", "api", "openapi.yaml"))
	if err != nil {
		return nil, err
	}
	if err := doc.Validate(context.Background()); err != nil {
		return nil, err
	}
	// Match requests to any server URL: the tests serve on random ports.
	doc.Servers = nil
	return legacy.NewRouter(doc)
})

// checkContract fails the test unless resp, with its body, is a response
// the spec allows for req: a known route and method, a declared status, and
// a body of the declared schema. Event streams are checked for their
// content type only.
func checkContract(t *testing.T, req *http.Request, resp *http.Response, body []byte) {
	t.Helper()
	router, err := spec()
	require.NoError(t, err, "the spec loads and is valid")
	route, params, err := router.FindRoute(req)
	var notRouted *routers.RouteError
	if errors.As(err, &notRouted) {
		// A request for no route of the spec, as tests of unknown routes
		// send; the server must still answer with an Error.
		require.JSONEq(t, `{"error":"`+resp.Request.Method+` `+req.URL.Path+`: no such route"}`, string(body), "an unknown route answers a JSON Error")
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

// readChecked reads resp's body, checks the response against the spec, and
// returns the body for decoding.
func readChecked(t *testing.T, req *http.Request, resp *http.Response) io.Reader {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	checkContract(t, req, resp, body)
	return bytes.NewReader(body)
}
