package blob

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flakyReader fails once and then continues, like a transient network error.
type flakyReader struct {
	r      io.Reader
	failed bool
}

var errTransient = errors.New("transient")

func (f *flakyReader) Read(p []byte) (int, error) {
	if !f.failed {
		f.failed = true
		return 0, errTransient
	}
	return f.r.Read(p)
}

// A consumer that retries after an error must not get the rest of the stream.
func TestSizedReaderErrorsAreSticky(t *testing.T) {
	s := newSizedReader(context.Background(), &flakyReader{r: strings.NewReader("abc")}, 3)
	buf := make([]byte, 8)

	_, err := s.Read(buf)
	require.ErrorIs(t, err, errTransient)
	n, err := s.Read(buf)
	assert.Zero(t, n)
	assert.ErrorIs(t, err, errTransient)
}

func TestSizedReaderRepeatsEOFAfterVerifiedEnd(t *testing.T) {
	s := newSizedReader(context.Background(), strings.NewReader("abc"), 3)
	data, err := io.ReadAll(s)
	require.NoError(t, err)
	assert.Equal(t, "abc", string(data))

	n, err := s.Read(make([]byte, 1))
	assert.Zero(t, n)
	assert.ErrorIs(t, err, io.EOF)
}

// The last bytes are withheld when the source holds more than size bytes, so
// a consumer that stops after size bytes never sees the complete content.
func TestSizedReaderWithholdsLastBytesOfOversizedSource(t *testing.T) {
	s := newSizedReader(context.Background(), strings.NewReader("abcd"), 3)
	n, err := s.Read(make([]byte, 3))
	assert.Zero(t, n)
	assert.ErrorIs(t, err, ErrSizeMismatch)
}

func TestSizedReaderReportsErrorWhileProbing(t *testing.T) {
	r := io.MultiReader(strings.NewReader("abc"), errorReader{errTransient})
	s := newSizedReader(context.Background(), r, 3)
	_, err := io.ReadAll(s)
	assert.ErrorIs(t, err, errTransient)
}

func TestSizedReaderStopsWhenContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := newSizedReader(ctx, strings.NewReader("abcdef"), 6)
	_, err := s.Read(make([]byte, 2))
	require.NoError(t, err)
	cancel()
	_, err = s.Read(make([]byte, 2))
	assert.ErrorIs(t, err, context.Canceled)
}

type errorReader struct{ err error }

func (e errorReader) Read([]byte) (int, error) { return 0, e.err }
