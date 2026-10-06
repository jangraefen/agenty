// Package audit writes the audit log: one JSON record per line, appended to a
// file only its owner can read.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jangraefen/agenty/internal/toolgateway"
)

var _ toolgateway.Audit = (*File)(nil)

// File is an audit log file. Every record is synced to disk before Record
// returns, so a recorded decision survives a crash.
type File struct {
	mu   sync.Mutex
	file *os.File
}

// Open opens path for appending, creating it with permissions 0600.
func Open(path string) (*File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600) //nolint:gosec // G304: the operator chooses the audit log path.
	if err != nil {
		return nil, fmt.Errorf("audit: %w", err)
	}
	return &File{file: f}, nil
}

// line is a record as written: the record's fields plus when it was written.
type line struct {
	Time time.Time `json:"time"`
	toolgateway.Record
}

// Record appends rec. An error means the record may not be stored, and the
// gateway does not execute a call it could not record.
func (f *File) Record(_ context.Context, rec toolgateway.Record) error {
	b, err := json.Marshal(line{Time: time.Now().UTC(), Record: rec})
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.file.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	if err := f.file.Sync(); err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	return nil
}

// Close closes the file.
func (f *File) Close() error {
	if err := f.file.Close(); err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	return nil
}
