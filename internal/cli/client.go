package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/jangraefen/agenty/internal/api"
)

// client calls the API of an agenty server as a user, in a workspace.
type client struct {
	base      string
	token     string
	workspace string
	http      *http.Client
}

func newClient(flags clientFlags) (*client, error) {
	u, err := url.Parse(flags.server)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("--server %q: must be an http or https URL", flags.server)
	}
	return &client{base: strings.TrimSuffix(flags.server, "/"), token: flags.token, workspace: flags.workspace, http: http.DefaultClient}, nil
}

// path returns the API path of a path in the client's workspace.
func (c *client) path(elems ...string) string {
	p := "/v1/workspaces/" + url.PathEscape(c.workspace)
	for _, e := range elems {
		p += "/" + url.PathEscape(e)
	}
	return p
}

// newRequest returns a request that signs in with the client's token.
func (c *client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return req, nil
}

// do sends in as JSON, if not nil, and decodes the response into out, if not
// nil. A response with an error status is returned as an error carrying the
// server's message.
func (c *client) do(ctx context.Context, method, path string, in, out any) (err error) {
	var body io.Reader
	if in != nil {
		b, merr := json.Marshal(in)
		if merr != nil {
			return fmt.Errorf("%s %s: %w", method, path, merr)
		}
		body = bytes.NewReader(b)
	}
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("%s %s: %w", method, path, cerr))
		}
	}()
	if resp.StatusCode >= http.StatusBadRequest {
		return responseError(resp)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("%s %s: response: %w", method, path, err)
		}
	}
	return nil
}

func responseError(resp *http.Response) error {
	var e api.Error
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil || e.Error == "" {
		return fmt.Errorf("server: %s", resp.Status)
	}
	return fmt.Errorf("server: %s", e.Error)
}

// events opens a run's event stream. Close it when done.
func (c *client) events(ctx context.Context, runID string) (*eventStream, error) {
	path := c.path("runs", runID, "events")
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Join(responseError(resp), resp.Body.Close())
	}
	return newEventStream(resp.Body), nil
}

// eventStream reads server-sent events. Lines have no length limit: an audit
// event carries a tool's whole result.
type eventStream struct {
	body io.ReadCloser
	r    *bufio.Reader
}

func newEventStream(body io.ReadCloser) *eventStream {
	return &eventStream{body: body, r: bufio.NewReader(body)}
}

// next returns the next named event's name and data, or io.EOF when the
// stream ends. Data lines are joined with newlines; an event without a name,
// and one the stream ends in the middle of, are dropped.
func (s *eventStream) next() (string, []byte, error) {
	var name string
	var data []string
	for {
		line, err := s.r.ReadString('\n')
		if errors.Is(err, io.EOF) {
			// A last line without its newline is part of an event the stream
			// ended in the middle of.
			return "", nil, io.EOF
		}
		if err != nil {
			return "", nil, fmt.Errorf("event stream: %w", err)
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			if name != "" {
				return name, []byte(strings.Join(data, "\n")), nil
			}
			name, data = "", nil
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			name = value
		case "data":
			data = append(data, value)
		}
	}
}

func (s *eventStream) Close() error {
	return s.body.Close()
}
