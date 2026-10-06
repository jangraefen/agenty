package cli

import (
	"context"
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
	return slog.New(&redactHandler{next: h, redact: r})
}

var _ slog.Handler = (*redactHandler)(nil)

// redactHandler redacts secrets from messages and attribute values before
// passing records on.
type redactHandler struct {
	next   slog.Handler
	redact *secret.Redactor
}

func (h *redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *redactHandler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.redact.String(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.attr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = h.attr(a)
	}
	return &redactHandler{next: h.next.WithAttrs(redacted), redact: h.redact}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{next: h.next.WithGroup(name), redact: h.redact}
}

// attr redacts a's value. A value that contained a secret becomes a string;
// any other value keeps its kind.
func (h *redactHandler) attr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		group := a.Value.Group()
		redacted := make([]slog.Attr, len(group))
		for i, g := range group {
			redacted[i] = h.attr(g)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(redacted...)}
	}
	s := a.Value.String()
	if r := h.redact.String(s); r != s {
		return slog.String(a.Key, r)
	}
	return a
}
