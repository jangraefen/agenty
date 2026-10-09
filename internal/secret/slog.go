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

// handler is the redacting slog.Handler Redactor.Handler returns. Every
// path into next, Handle and WithAttrs alike, redacts first, so no attribute
// reaches the wrapped handler unredacted, however it was added.
type handler struct {
	next   slog.Handler
	redact *Redactor
}

// Enabled defers to next: redaction does not change which levels are logged.
func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle builds a new record with the message and every attribute redacted,
// and passes it to next. It builds a new record because slog offers no way
// to replace a record's attributes in place.
func (h *handler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.redact.String(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.attr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

// WithAttrs redacts attrs before binding them to next, so attributes bound
// with Logger.With are redacted once, here, and not left for Handle, which
// never sees them again.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = h.attr(a)
	}
	return &handler{next: h.next.WithAttrs(redacted), redact: h.redact}
}

// WithGroup opens the group on next and keeps redacting what follows; a
// group name is chosen by code, not data, so it is not redacted.
func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{next: h.next.WithGroup(name), redact: h.redact}
}

// attr redacts a's value. A value that contained a secret becomes a string;
// any other value keeps its kind.
//
// Values are compared through their string form, which covers every kind,
// errors and Stringers included. Only a changed value is replaced, so
// numbers, times and other typed values keep their kind in structured output.
func (h *handler) attr(a slog.Attr) slog.Attr {
	// Resolve LogValuers first: a lazy value may hold a secret only once it
	// is computed, and the wrapped handler would otherwise compute it after
	// redaction.
	a.Value = a.Value.Resolve()
	// Groups are redacted member by member, keeping their structure.
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
