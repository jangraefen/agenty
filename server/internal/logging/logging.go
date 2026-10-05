// Package logging builds the server's log/slog logger: charm.land/log/v2 as
// the output handler (text, JSON, or logfmt) behind a redacting handler that
// removes registered secret values from every record (ARCHITECTURE §3
// invariant 6, §12.2).
package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	charmlog "charm.land/log/v2"
)

// Format selects the output format of the log.
type Format string

// Supported formats: text for development, JSON or logfmt for production.
const (
	FormatText   Format = "text"
	FormatJSON   Format = "json"
	FormatLogfmt Format = "logfmt"
)

// timeFormat is RFC 3339 with milliseconds.
const timeFormat = "2006-01-02T15:04:05.000Z07:00"

// ParseFormat returns the format named s.
func ParseFormat(s string) (Format, error) {
	switch f := Format(s); f {
	case FormatText, FormatJSON, FormatLogfmt:
		return f, nil
	default:
		return "", fmt.Errorf("unknown log format %q: use text, json, or logfmt", s)
	}
}

// Options configures the logger.
type Options struct {
	// Format is the output format.
	Format Format
	// Level is the minimum level that is written.
	Level slog.Level
}

// New returns a logger that writes records at or above opts.Level to w in
// opts.Format, with every value registered in secrets redacted.
func New(w io.Writer, opts Options, secrets *Secrets) (*slog.Logger, error) {
	if secrets == nil {
		return nil, errors.New("logging: a secret registry is required")
	}
	formatter, err := charmFormatter(opts.Format)
	if err != nil {
		return nil, err
	}
	handler := charmlog.NewWithOptions(w, charmlog.Options{
		Level:           charmlog.Level(opts.Level),
		ReportTimestamp: true,
		TimeFormat:      timeFormat,
		Formatter:       formatter,
	})
	return slog.New(NewRedactingHandler(handler, secrets)), nil
}

func charmFormatter(f Format) (charmlog.Formatter, error) {
	switch f {
	case FormatText:
		return charmlog.TextFormatter, nil
	case FormatJSON:
		return charmlog.JSONFormatter, nil
	case FormatLogfmt:
		return charmlog.LogfmtFormatter, nil
	default:
		_, err := ParseFormat(string(f))
		return 0, err
	}
}
