package cli

import (
	"io"
	"log/slog"

	"github.com/charmbracelet/log"

	"github.com/jangraefen/agenty/internal/secret"
)

// newLogger returns a logger that writes to w at level and redacts r's
// secrets from every record. It is the only way this package makes a
// logger, so no command can log around the redactor (guarantee 5).
//
// The slog API is what the code uses; charmbracelet/log only formats the
// output. The redactor wraps the handler rather than the writer, so it sees
// each record's message and attributes before they are formatted.
func newLogger(w io.Writer, level slog.Level, r *secret.Redactor) *slog.Logger {
	// charmbracelet/log levels have the same values as slog's, so the
	// --log-level flag converts directly.
	h := log.NewWithOptions(w, log.Options{Level: log.Level(level), ReportTimestamp: true})
	return slog.New(r.Handler(h))
}
