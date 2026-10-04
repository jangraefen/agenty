package blob

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A file system that cannot sync directories reports EINVAL or an
// unsupported-operation error; syncDir then has nothing more to do. Every
// other error, such as an I/O error, still fails the write.
func TestDirSyncError(t *testing.T) {
	wrap := func(err error) error { return &fs.PathError{Op: "sync", Path: "/root/objects", Err: err} }

	for name, err := range map[string]error{
		"EINVAL":         wrap(syscall.EINVAL),
		"ENOTSUP":        wrap(syscall.ENOTSUP),
		"ErrUnsupported": fmt.Errorf("sync: %w", errors.ErrUnsupported),
	} {
		t.Run("Ignored/"+name, func(t *testing.T) {
			assert.NoError(t, dirSyncError(err))
		})
	}
	for name, err := range map[string]error{
		"EIO":   wrap(syscall.EIO),
		"Other": errors.New("disk on fire"),
	} {
		t.Run("Reported/"+name, func(t *testing.T) {
			assert.ErrorIs(t, dirSyncError(err), err)
		})
	}
	assert.NoError(t, dirSyncError(nil))
}
