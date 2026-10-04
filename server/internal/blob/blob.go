// Package blob stores binary objects (uploads, skill resources, run artifacts)
// by key in a local volume or an S3-compatible object store (ARCHITECTURE §11.1).
// All implementations pass one shared contract test suite.
package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Blob is an object store addressed by key.
//
// Keys are validated with ValidateKey by every method. A Put replaces the
// object atomically: readers see either the previous or the complete new
// object, and a failed Put leaves the previous object in place.
type Blob interface {
	// Put stores exactly size bytes read from r under key. It fails with
	// ErrSizeMismatch if r yields fewer or more bytes.
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	// Get opens the object under key; the caller closes it. A missing object
	// is ErrNotFound.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Stat describes the object under key. A missing object is ErrNotFound.
	Stat(ctx context.Context, key string) (Info, error)
	// Delete removes the object under key; deleting a missing object succeeds.
	Delete(ctx context.Context, key string) error
}

// Info describes a stored object.
type Info struct {
	Size int64
}

// Errors reported by every implementation; match them with errors.Is.
var (
	ErrNotFound     = errors.New("blob: object not found")
	ErrInvalidKey   = errors.New("blob: invalid key")
	ErrSizeMismatch = errors.New("blob: content does not match the declared size")
)

// MaxKeyLength is the maximum key length in bytes (the S3 limit).
const MaxKeyLength = 1024

// ValidateKey reports whether key is a portable object key: one or more
// segments separated by "/", each made of ASCII letters, digits, ".", "_", and
// "-" and not starting with ".". This rules out absolute paths, empty segments,
// "." and "..", and backslashes, so a key can never leave a local root.
func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty", ErrInvalidKey)
	}
	if len(key) > MaxKeyLength {
		return fmt.Errorf("%w: longer than %d bytes", ErrInvalidKey, MaxKeyLength)
	}
	for segment := range strings.SplitSeq(key, "/") {
		if err := validateSegment(segment); err != nil {
			return fmt.Errorf("%w %q: %w", ErrInvalidKey, key, err)
		}
	}
	return nil
}

func validateSegment(segment string) error {
	if segment == "" {
		return errors.New("empty segment")
	}
	if segment[0] == '.' {
		return errors.New(`segment starts with "."`)
	}
	for _, c := range []byte(segment) {
		if !isKeyChar(c) {
			return fmt.Errorf("character %q not allowed", c)
		}
	}
	return nil
}

func isKeyChar(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
		c == '.' || c == '_' || c == '-'
}

// sizedReader yields the bytes of r and fails with ErrSizeMismatch unless r
// holds exactly size bytes. It reports io.EOF only after it has verified that
// r is exhausted, so a consumer that stops at EOF never accepts wrong content.
// It also stops when ctx is done. Errors are sticky: once Read has failed, it
// keeps failing, so a retrying consumer cannot resume a corrupted stream.
type sizedReader struct {
	ctx       context.Context //nolint:containedctx // bound to one Put call
	r         io.Reader
	size      int64
	remaining int64
	err       error // first error other than io.EOF
}

func newSizedReader(ctx context.Context, r io.Reader, size int64) *sizedReader {
	return &sizedReader{ctx: ctx, r: r, size: size, remaining: size}
}

func (s *sizedReader) Read(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	n, err := s.read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		s.err = err
	}
	return n, err
}

func (s *sizedReader) read(p []byte) (int, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	if s.remaining == 0 {
		return 0, s.checkExhausted()
	}
	if int64(len(p)) > s.remaining {
		p = p[:s.remaining]
	}
	n, err := s.r.Read(p)
	s.remaining -= int64(n)
	if s.remaining == 0 && err == nil {
		// Probe for excess content before handing out the last bytes, so a
		// consumer that stops after size bytes never completes an upload.
		err = s.checkExhausted()
		if !errors.Is(err, io.EOF) {
			return 0, err
		}
	}
	switch {
	case errors.Is(err, io.EOF) && s.remaining > 0:
		return n, fmt.Errorf("%w: got %d of %d bytes", ErrSizeMismatch, s.size-s.remaining, s.size)
	case errors.Is(err, io.EOF):
		if n > 0 {
			return n, nil
		}
		return 0, io.EOF
	}
	return n, err
}

// checkExhausted reads past the declared size to make sure nothing is left.
func (s *sizedReader) checkExhausted() error {
	var probe [1]byte
	for {
		n, err := s.r.Read(probe[:])
		if n > 0 {
			return fmt.Errorf("%w: more than %d bytes", ErrSizeMismatch, s.size)
		}
		if errors.Is(err, io.EOF) {
			return io.EOF
		}
		if err != nil {
			return err
		}
	}
}

func checkSize(size int64) error {
	if size < 0 {
		return fmt.Errorf("%w: negative size %d", ErrSizeMismatch, size)
	}
	return nil
}
