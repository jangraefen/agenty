package blob_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/blob"
)

func newLocal(t *testing.T) blob.Blob {
	t.Helper()
	s, err := blob.NewLocal(t.TempDir())
	require.NoError(t, err)
	return s
}

func TestLocalContract(t *testing.T) {
	runContract(t, newLocal)
}

// Keys that try to leave the root are rejected, and nothing is ever written
// outside the root directory.
func TestLocalRejectsPathTraversal(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	s, err := blob.NewLocal(root)
	require.NoError(t, err)
	ctx := context.Background()

	for _, key := range []string{"../escape", "a/../../escape", "/etc/passwd", `..\escape`, "..", "a/.."} {
		require.ErrorIs(t, s.Put(ctx, key, strings.NewReader("x"), 1), blob.ErrInvalidKey, "Put %q", key)
		require.ErrorIs(t, s.Delete(ctx, key), blob.ErrInvalidKey, "Delete %q", key)
	}

	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the root exists in its parent")
	assert.Equal(t, "root", entries[0].Name())
}

func TestLocalLeavesNoTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	s, err := blob.NewLocal(root)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, s.Put(ctx, "ok", strings.NewReader("data"), 4))
	require.ErrorIs(t, s.Put(ctx, "short", strings.NewReader("da"), 4), blob.ErrSizeMismatch)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, s.Put(canceled, "canceled", strings.NewReader("data"), 4), context.Canceled)

	var files []string
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return err
	}))
	assert.Len(t, files, 1, "only the one stored object, no temporary files: %v", files)
}

// A crash during Put leaves its temporary file behind; NewLocal removes such
// files once they are old enough that no Put can still be writing them.
func TestNewLocalRemovesStaleTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	_, err := blob.NewLocal(root)
	require.NoError(t, err)

	tmp := filepath.Join(root, "tmp")
	stale := filepath.Join(tmp, "put-stale")
	fresh := filepath.Join(tmp, "put-fresh")
	other := filepath.Join(tmp, "other")
	for _, f := range []string{stale, fresh, other} {
		require.NoError(t, os.WriteFile(f, []byte("partial"), 0o600))
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, f := range []string{stale, other} {
		require.NoError(t, os.Chtimes(f, old, old))
	}

	_, err = blob.NewLocal(root)
	require.NoError(t, err)

	tests := map[string]struct {
		path string
		kept bool
	}{
		"StalePut":  {stale, false},
		"FreshPut":  {fresh, true},
		"OtherFile": {other, true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := os.Stat(tt.path)
			if tt.kept {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, fs.ErrNotExist)
			}
		})
	}
}

func TestLocalPersistsAcrossInstances(t *testing.T) {
	root := t.TempDir()
	first, err := blob.NewLocal(root)
	require.NoError(t, err)
	put(t, first, "runs/r1/out", "kept")

	second, err := blob.NewLocal(root)
	require.NoError(t, err)
	assert.Equal(t, "kept", get(t, second, "runs/r1/out"))
}

func TestNewLocalCreatesRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "a", "b")
	_, err := blob.NewLocal(root)
	require.NoError(t, err)
	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNewLocalRejectsInvalidRoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	for name, root := range map[string]string{
		"Empty":    "",
		"Relative": "relative/dir",
		"File":     file,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := blob.NewLocal(root)
			assert.Error(t, err)
		})
	}
}

// Errors from the file system other than "not found" are reported as such.
func TestLocalReportsFileSystemErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}
	root := t.TempDir()
	s, err := blob.NewLocal(root)
	require.NoError(t, err)
	put(t, s, "k", "v")
	ctx := context.Background()

	require.NoError(t, os.Chmod(root, 0o000))
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	_, err = s.Get(ctx, "k")
	assertFSError(t, err, "Get")
	_, err = s.Stat(ctx, "k")
	assertFSError(t, err, "Stat")
	assertFSError(t, s.Delete(ctx, "k"), "Delete")
	assertFSError(t, s.Put(ctx, "k", strings.NewReader("v"), 1), "Put")
}

func assertFSError(t *testing.T, err error, op string) {
	t.Helper()
	if assert.Error(t, err, op) {
		assert.NotErrorIs(t, err, blob.ErrNotFound, op)
		assert.ErrorIs(t, err, fs.ErrPermission, op)
	}
}
