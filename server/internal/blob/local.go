package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Local stores objects in a directory on a local volume.
//
// Layout: an object lives at objects/<h[:2]>/<h>, where h is the hex SHA-256
// of its key, and is written to tmp/ first and then renamed into place, so
// writes are atomic. Hashing keeps file names independent of the key: no key
// can address a path outside the root, keys that differ only in case stay
// distinct on case-insensitive file systems, and a key and its "child"
// ("a" and "a/b") never collide as file and directory.
type Local struct {
	objects string
	tmp     string
}

var _ Blob = (*Local)(nil)

// NewLocal returns a store rooted at the absolute directory root, creating it
// if needed.
func NewLocal(root string) (*Local, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("blob: local root %q is not an absolute path", root)
	}
	l := &Local{objects: filepath.Join(root, "objects"), tmp: filepath.Join(root, "tmp")}
	for _, dir := range []string{l.objects, l.tmp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("blob: create local root: %w", err)
		}
	}
	return l, nil
}

func (l *Local) path(key string) (dir, file string, err error) {
	if err := ValidateKey(key); err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(sum[:])
	dir = filepath.Join(l.objects, name[:2])
	return dir, filepath.Join(dir, name), nil
}

// Put implements Blob.
func (l *Local) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	dir, file, err := l.path(key)
	if err != nil {
		return err
	}
	if err := checkSize(size); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(l.tmp, "put-*")
	if err != nil {
		return fmt.Errorf("blob: put %q: %w", key, err)
	}
	if err := l.write(tmp, newSizedReader(ctx, r, size), dir, file); err != nil {
		_ = os.Remove(tmp.Name()) // best effort; the write already failed
		return fmt.Errorf("blob: put %q: %w", key, err)
	}
	return nil
}

// write copies r into tmp, makes it durable, and renames it to file, syncing
// every directory whose entries change.
func (l *Local) write(tmp *os.File, r io.Reader, dir, file string) error {
	_, err := io.Copy(tmp, r)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := l.mkShard(dir); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		return err
	}
	return syncDir(dir)
}

// mkShard creates the shard directory dir if needed; a new directory entry is
// made durable by syncing the objects directory.
func (l *Local) mkShard(dir string) error {
	err := os.Mkdir(dir, 0o700)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncDir(l.objects)
}

// syncDir makes a change of an entry in dir (rename, create, remove) durable.
// Like a failed file sync, a failed directory sync fails the operation, except
// where the platform or file system cannot sync directories at all (see
// dirSyncError).
func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // dir is derived from the root and a hash, never from the key
	if err != nil {
		return err
	}
	err = dirSyncError(d.Sync())
	if closeErr := d.Close(); err == nil {
		err = closeErr
	}
	return err
}

// dirSyncError filters the error of syncing a directory. Some platforms and
// file systems do not support syncing a directory and report EINVAL or an
// unsupported operation; the entry is then as durable as that file system
// makes it, and there is nothing more to do. Every other error is returned.
func dirSyncError(err error) error {
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, errors.ErrUnsupported) {
		return nil
	}
	return err
}

// Get implements Blob.
func (l *Local) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	_, file, err := l.path(key)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(file) //nolint:gosec // file is derived from the root and a hash, never from the key
	if err != nil {
		return nil, localError("get", key, err)
	}
	return f, nil
}

// Stat implements Blob.
func (l *Local) Stat(ctx context.Context, key string) (Info, error) {
	_, file, err := l.path(key)
	if err != nil {
		return Info{}, err
	}
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	fi, err := os.Stat(file)
	if err != nil {
		return Info{}, localError("stat", key, err)
	}
	return Info{Size: fi.Size()}, nil
}

// Delete implements Blob.
func (l *Local) Delete(ctx context.Context, key string) error {
	dir, file, err := l.path(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(file); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return localError("delete", key, err)
	}
	if err := syncDir(dir); err != nil {
		return localError("delete", key, err)
	}
	return nil
}

func localError(op, key string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %q", ErrNotFound, key)
	}
	return fmt.Errorf("blob: %s %q: %w", op, key, err)
}
