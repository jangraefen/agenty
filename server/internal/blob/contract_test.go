package blob_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/blob"
)

// runContract is the one contract test suite every Blob implementation must pass
// (AC-E02-7). newStore returns a fresh, empty store for each subtest.
func runContract(t *testing.T, newStore func(t *testing.T) blob.Blob) {
	t.Helper()
	ctx := t.Context()

	t.Run("PutThenGetReturnsContent", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "runs/r1/artifacts/report.txt", "hello blob")
		assert.Equal(t, "hello blob", get(t, s, "runs/r1/artifacts/report.txt"))
	})

	t.Run("PutOverwritesExistingObject", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "k", "first")
		put(t, s, "k", "second, longer")
		assert.Equal(t, "second, longer", get(t, s, "k"))
	})

	t.Run("EmptyObject", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "empty", "")
		assert.Empty(t, get(t, s, "empty"))
		info, err := s.Stat(ctx, "empty")
		require.NoError(t, err)
		assert.Zero(t, info.Size)
	})

	t.Run("LargeObjectRoundTrips", func(t *testing.T) {
		s := newStore(t)
		data := make([]byte, 3<<20+17)
		_, err := rand.Read(data)
		require.NoError(t, err)
		// A reader that is neither seekable nor returns everything at once.
		r := iotest.HalfReader(bytes.NewReader(data))
		require.NoError(t, s.Put(ctx, "large", r, int64(len(data))))
		assert.Equal(t, string(data), get(t, s, "large"))
	})

	t.Run("StatReportsSize", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "a/b", "12345")
		info, err := s.Stat(ctx, "a/b")
		require.NoError(t, err)
		assert.Equal(t, int64(5), info.Size)
	})

	t.Run("MissingObjectIsNotFound", func(t *testing.T) {
		s := newStore(t)
		_, err := s.Get(ctx, "missing")
		require.ErrorIs(t, err, blob.ErrNotFound, "Get")
		_, err = s.Stat(ctx, "missing")
		assert.ErrorIs(t, err, blob.ErrNotFound, "Stat")
	})

	t.Run("DeleteRemovesObject", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "gone", "x")
		require.NoError(t, s.Delete(ctx, "gone"))
		_, err := s.Get(ctx, "gone")
		assert.ErrorIs(t, err, blob.ErrNotFound)
	})

	t.Run("DeleteMissingObjectSucceeds", func(t *testing.T) {
		s := newStore(t)
		assert.NoError(t, s.Delete(ctx, "never-written"))
	})

	t.Run("KeyAndChildKeyCoexist", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "a", "parent")
		put(t, s, "a/b", "child")
		assert.Equal(t, "parent", get(t, s, "a"))
		assert.Equal(t, "child", get(t, s, "a/b"))
	})

	t.Run("KeysAreCaseSensitive", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "Report.txt", "upper")
		put(t, s, "report.txt", "lower")
		assert.Equal(t, "upper", get(t, s, "Report.txt"))
		assert.Equal(t, "lower", get(t, s, "report.txt"))
	})

	t.Run("InvalidKeysAreRejected", func(t *testing.T) {
		s := newStore(t)
		for _, key := range invalidKeys() {
			assert.ErrorIs(t, s.Put(ctx, key, strings.NewReader("x"), 1), blob.ErrInvalidKey, "Put %q", key)
			_, err := s.Get(ctx, key)
			assert.ErrorIs(t, err, blob.ErrInvalidKey, "Get %q", key)
			_, err = s.Stat(ctx, key)
			assert.ErrorIs(t, err, blob.ErrInvalidKey, "Stat %q", key)
			assert.ErrorIs(t, s.Delete(ctx, key), blob.ErrInvalidKey, "Delete %q", key)
		}
	})

	t.Run("NegativeSizeIsRejected", func(t *testing.T) {
		s := newStore(t)
		err := s.Put(ctx, "k", strings.NewReader(""), -1)
		require.ErrorIs(t, err, blob.ErrSizeMismatch)
		_, err = s.Stat(ctx, "k")
		assert.ErrorIs(t, err, blob.ErrNotFound)
	})

	// A failed Put never leaves a partial object and keeps the previous one.
	t.Run("FailedPutKeepsPreviousObject", func(t *testing.T) {
		failures := map[string]struct {
			body func() io.Reader
			size int64
			want error
		}{
			"BodyShorterThanSize": {func() io.Reader { return strings.NewReader("short") }, 100, blob.ErrSizeMismatch},
			"BodyLongerThanSize": {
				func() io.Reader { return strings.NewReader("much longer than declared") }, 4, blob.ErrSizeMismatch,
			},
			"ReaderFails": {func() io.Reader {
				return io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(errBoom))
			}, 100, errBoom},
		}
		for name, f := range failures {
			t.Run(name, func(t *testing.T) {
				s := newStore(t)
				put(t, s, "k", "previous")
				require.ErrorIs(t, s.Put(ctx, "k", f.body(), f.size), f.want)
				assert.Equal(t, "previous", get(t, s, "k"))

				require.ErrorIs(t, s.Put(ctx, "fresh", f.body(), f.size), f.want)
				_, err := s.Stat(ctx, "fresh")
				assert.ErrorIs(t, err, blob.ErrNotFound, "no partial object")
			})
		}
	})

	t.Run("CanceledContextFails", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "k", "v")
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		require.ErrorIs(t, s.Put(canceled, "k", strings.NewReader("new"), 3), context.Canceled, "Put")
		_, err := s.Get(canceled, "k")
		require.ErrorIs(t, err, context.Canceled, "Get")
		_, err = s.Stat(canceled, "k")
		require.ErrorIs(t, err, context.Canceled, "Stat")
		require.ErrorIs(t, s.Delete(canceled, "k"), context.Canceled, "Delete")
		assert.Equal(t, "v", get(t, s, "k"), "nothing changed")
	})
}

var errBoom = errors.New("boom")

// invalidKeys returns keys that every implementation must reject, above all
// those that could escape a local root directory.
func invalidKeys() []string {
	return []string{
		"",
		"/abs",
		"trailing/",
		"a//b",
		"..",
		"../escape",
		"a/../../escape",
		"a/..",
		".",
		"./a",
		".hidden",
		"a/.hidden",
		`a\b`,
		`..\escape`,
		"a b",
		"nul\x00",
		"ümlaut",
		"a:b",
		strings.Repeat("k", blob.MaxKeyLength+1),
	}
}

func put(t *testing.T, s blob.Blob, key, content string) {
	t.Helper()
	require.NoError(t, s.Put(t.Context(), key, strings.NewReader(content), int64(len(content))), "Put %q", key)
}

func get(t *testing.T, s blob.Blob, key string) string {
	t.Helper()
	rc, err := s.Get(t.Context(), key)
	require.NoError(t, err, "Get %q", key)
	defer func() { require.NoError(t, rc.Close()) }()
	data, err := io.ReadAll(rc)
	require.NoError(t, err, "read %q", key)
	return string(data)
}
