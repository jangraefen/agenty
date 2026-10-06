package secret

import (
	"context"
	"log/slog"
)

// Handler returns a slog handler that redacts r's secrets from every record's
// message and attribute values, then passes the record to next. It wraps a
// handler rather than a logger so that nothing can reach next unredacted:
// attributes bound with Logger.With before wrapping would already be inside
// next.
func (r *Redactor) Handler(next slog.Handler) slog.Handler {
	return &handler{next: next, redact: r}
}

var _ slog.Handler = (*handler)(nil)

type handler struct {
	next   slog.Handler
	redact *Redactor
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *handler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.redact.String(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.attr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = h.attr(a)
	}
	return &handler{next: h.next.WithAttrs(redacted), redact: h.redact}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{next: h.next.WithGroup(name), redact: h.redact}
}

// attr redacts a's value. A value that contained a secret becomes a string;
// any other value keeps its kind.
func (h *handler) attr(a slog.Attr) slog.Attr {
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
