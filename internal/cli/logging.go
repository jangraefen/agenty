package cli

import (
	"io"
	"log/slog"

	"github.com/charmbracelet/log"

	"github.com/jangraefen/agenty/internal/secret"
)

// newLogger returns a logger that writes to w at level and redacts r's
// secrets from every record.
func newLogger(w io.Writer, level slog.Level, r *secret.Redactor) *slog.Logger {
	// charmbracelet/log levels have the same values as slog's.
	h := log.NewWithOptions(w, log.Options{Level: log.Level(level), ReportTimestamp: true})
	return slog.New(r.Handler(h))
}
