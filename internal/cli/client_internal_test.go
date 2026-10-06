package cli

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventStream_Next(t *testing.T) {
	type ev struct{ name, data string }
	long := strings.Repeat("x", 1<<20)
	tests := []struct {
		name   string
		stream string
		want   []ev
	}{
		{"one event", "event: audit\ndata: {\"a\":1}\n\n", []ev{{"audit", `{"a":1}`}}},
		{"no space after the colon", "event:audit\ndata:{}\n\n", []ev{{"audit", `{}`}}},
		{"multi-line data is joined with newlines", "event: finished\ndata: a\ndata: b\n\n", []ev{{"finished", "a\nb"}}},
		{"only one leading space is removed", "event: e\ndata:  x\n\n", []ev{{"e", " x"}}},
		{"data larger than a scanner's buffer", "event: audit\ndata: " + long + "\n\n", []ev{{"audit", long}}},
		{"an unnamed event does not leak into the next", "data: stray\n\nevent: e\ndata: x\n\n", []ev{{"e", "x"}}},
		{"comments and unknown fields are ignored", ": keep-alive\nid: 7\nevent: e\ndata: x\n\n", []ev{{"e", "x"}}},
		{"CRLF line ends", "event: e\r\ndata: x\r\n\r\n", []ev{{"e", "x"}}},
		{"a stream that ends mid-event drops it", "event: e\ndata: x\n\nevent: f\ndata: y", []ev{{"e", "x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newEventStream(io.NopCloser(strings.NewReader(tt.stream)))
			var got []ev
			for {
				name, data, err := s.next()
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				got = append(got, ev{name, string(data)})
			}
			assert.Equal(t, tt.want, got)
			require.NoError(t, s.Close())
		})
	}
}

func TestEventStream_ReadError(t *testing.T) {
	r, w := io.Pipe()
	require.NoError(t, w.CloseWithError(assert.AnError))
	s := newEventStream(r)

	_, _, err := s.next()

	require.ErrorIs(t, err, assert.AnError)
}
