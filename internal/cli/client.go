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

// client calls the API of an agenty server.
type client struct {
	base string
	http *http.Client
}

func newClient(server string) (*client, error) {
	u, err := url.Parse(server)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("--server %q: must be an http or https URL", server)
	}
	return &client{base: strings.TrimSuffix(server, "/"), http: http.DefaultClient}, nil
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
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
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
	path := "/v1/runs/" + url.PathEscape(runID) + "/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
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
	return &eventStream{body: resp.Body, lines: bufio.NewScanner(resp.Body)}, nil
}

// eventStream reads server-sent events.
type eventStream struct {
	body  io.ReadCloser
	lines *bufio.Scanner
}

// next returns the next event's name and data, or io.EOF when the stream
// ends.
func (s *eventStream) next() (string, []byte, error) {
	var name string
	var data []byte
	for s.lines.Scan() {
		line := s.lines.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(line, "data:")...)
		case line == "" && name != "":
			return name, data, nil
		}
	}
	if err := s.lines.Err(); err != nil {
		return "", nil, fmt.Errorf("event stream: %w", err)
	}
	return "", nil, io.EOF
}

func (s *eventStream) Close() error {
	return s.body.Close()
}
