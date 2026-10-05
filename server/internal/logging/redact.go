package logging

import (
	"cmp"
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	charmlog "github.com/charmbracelet/log"
)

// Redacted replaces every occurrence of a registered secret.
const Redacted = "[REDACTED]"

// Secrets is a registry of secret values that must never appear in log
// output. It is safe for concurrent use; values registered at any time are
// redacted from every record handled afterwards, including attributes bound
// earlier with Logger.With.
type Secrets struct {
	mu     sync.Mutex
	values atomic.Pointer[[]string] // longest first; replaced, never mutated
}

// NewSecrets returns an empty registry.
func NewSecrets() *Secrets {
	return &Secrets{}
}

// Register adds secret values. Empty values are ignored.
func (s *Secrets) Register(values ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var next []string
	if cur := s.values.Load(); cur != nil {
		next = slices.Clone(*cur)
	}
	for _, v := range values {
		if v != "" && !slices.Contains(next, v) {
			next = append(next, v)
		}
	}
	slices.SortFunc(next, func(a, b string) int {
		return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(a, b))
	})
	s.values.Store(&next)
}

// Redact returns str with every occurrence of a registered secret replaced
// by Redacted. Overlapping or adjacent occurrences are replaced as one span,
// so no fragment of any secret remains.
func (s *Secrets) Redact(str string) string {
	values := s.values.Load()
	if values == nil || len(*values) == 0 || str == "" {
		return str
	}
	type span struct{ start, end int }
	var spans []span
	for _, v := range *values {
		for off := 0; off < len(str); {
			i := strings.Index(str[off:], v)
			if i < 0 {
				break
			}
			spans = append(spans, span{off + i, off + i + len(v)})
			off += i + 1
		}
	}
	if len(spans) == 0 {
		return str
	}
	slices.SortFunc(spans, func(a, b span) int { return cmp.Compare(a.start, b.start) })
	var b strings.Builder
	last := 0
	for i := 0; i < len(spans); {
		start, end := spans[i].start, spans[i].end
		for i++; i < len(spans) && spans[i].start <= end; i++ {
			end = max(end, spans[i].end)
		}
		b.WriteString(str[last:start])
		b.WriteString(Redacted)
		last = end
	}
	b.WriteString(str[last:])
	return b.String()
}

// redactingHandler removes registered secrets from records before passing
// them to the next handler. It keeps attributes and groups added with
// WithAttrs and WithGroup itself, instead of forwarding them, so that they
// are redacted at handling time against the secrets registered by then.
type redactingHandler struct {
	next    slog.Handler
	secrets *Secrets
	scopes  []scope
}

// scope is either a group opened with WithGroup or attributes added with
// WithAttrs.
type scope struct {
	group string
	attrs []slog.Attr
}

// NewRedactingHandler returns a handler that redacts every value registered
// in secrets from the message, attribute keys, and attribute values
// (including nested groups, LogValuer results, and values that would be
// formatted with fmt) of each record, then passes it to next. Values of
// kind Any are rendered to strings first, so the text that is checked is the
// text that is written whatever next's format; durations, times, and floats
// are checked in both their text and their JSON form.
//
// The handler follows the slog.Handler contract for the next handler: it
// drops empty attributes and groups without attributes and inlines groups
// with an empty key into their parent. A top-level attribute whose key is
// one charmbracelet/log writes itself (time, level, msg, prefix, caller) is
// renamed to ReservedKeyPrefix followed by the key, so that it is neither
// dropped nor duplicated.
func NewRedactingHandler(next slog.Handler, secrets *Secrets) slog.Handler {
	return &redactingHandler{next: next, secrets: secrets}
}

func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return h.with(scope{attrs: slices.Clone(attrs)})
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.with(scope{group: name})
}

func (h *redactingHandler) with(s scope) *redactingHandler {
	return &redactingHandler{next: h.next, secrets: h.secrets, scopes: append(slices.Clip(h.scopes), s)}
}

func (h *redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	// Nest the record's attributes into the scopes, innermost first.
	for _, s := range slices.Backward(h.scopes) {
		switch {
		case s.attrs != nil:
			attrs = append(slices.Clone(s.attrs), attrs...)
		case len(attrs) > 0:
			attrs = []slog.Attr{{Key: s.group, Value: slog.GroupValue(attrs...)}}
		}
	}

	redacted := h.redactAttrs(attrs)
	for i, a := range redacted {
		if isReservedKey(a.Key) {
			redacted[i].Key = ReservedKeyPrefix + a.Key
		}
	}
	out := slog.NewRecord(r.Time, r.Level, h.secrets.Redact(r.Message), r.PC)
	out.AddAttrs(redacted...)
	return h.next.Handle(ctx, out)
}

// ReservedKeyPrefix is prepended to the key of a top-level attribute that
// collides with a field charmbracelet/log writes itself.
const ReservedKeyPrefix = "attr."

// isReservedKey reports whether key is one of the fields charmbracelet/log
// writes for every record; it treats a top-level attribute with such a key
// as its own field and drops or duplicates it.
func isReservedKey(key string) bool {
	switch key {
	case charmlog.TimestampKey, charmlog.LevelKey, charmlog.MessageKey, charmlog.PrefixKey, charmlog.CallerKey:
		return true
	default:
		return false
	}
}

// redactAttrs returns the redacted attributes, without empty attributes and
// empty groups, and with the attributes of groups with an empty key inlined.
func (h *redactingHandler) redactAttrs(attrs []slog.Attr) []slog.Attr {
	out := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		out = h.appendRedacted(out, a)
	}
	return out
}

func (h *redactingHandler) appendRedacted(out []slog.Attr, a slog.Attr) []slog.Attr {
	v := a.Value.Resolve()
	switch {
	case a.Key == "" && v.Kind() == slog.KindAny && v.Any() == nil:
		// An empty attribute (a.Equal(slog.Attr{}), checked here without
		// comparing values that may be uncomparable) is ignored.
		return out
	case v.Kind() == slog.KindGroup:
		children := h.redactAttrs(v.Group())
		switch {
		case len(children) == 0:
			return out
		case a.Key == "":
			return append(out, children...)
		default:
			return append(out, slog.Attr{Key: h.secrets.Redact(a.Key), Value: slog.GroupValue(children...)})
		}
	default:
		return append(out, h.redactAttr(a.Key, v))
	}
}

// redactAttr redacts an attribute whose resolved value v is not a group.
func (h *redactingHandler) redactAttr(rawKey string, v slog.Value) slog.Attr {
	key := h.secrets.Redact(rawKey)
	switch v.Kind() {
	case slog.KindString:
		return slog.String(key, h.secrets.Redact(v.String()))
	case slog.KindAny:
		return slog.String(key, h.secrets.Redact(render(v.Any())))
	case slog.KindDuration, slog.KindTime, slog.KindFloat64:
		// The JSON format writes these with encoding/json, the text formats
		// with Value.String; the two differ (1500000000 versus 1.5s), so
		// both are checked.
		s := v.String()
		if r := h.secrets.Redact(s); r != s {
			return slog.String(key, r)
		}
		if j, err := json.Marshal(v.Any()); err == nil && h.secrets.Redact(string(j)) != string(j) {
			return slog.String(key, s)
		}
		return slog.Attr{Key: key, Value: v}
	default:
		// Integers and booleans are written as Value.String in every format
		// and keep their kind unless that text contains a secret.
		if s := v.String(); h.secrets.Redact(s) != s {
			return slog.String(key, h.secrets.Redact(s))
		}
		return slog.Attr{Key: key, Value: v}
	}
}

// render returns the text of a value of kind Any, as log/slog's text handler
// would format it. A nil pointer is written as <nil> instead of calling its
// methods, which could panic inside the log call.
func render(v any) string {
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && rv.IsNil() {
		return "<nil>"
	}
	switch t := v.(type) {
	case error:
		return t.Error()
	case encoding.TextMarshaler:
		if b, err := t.MarshalText(); err == nil {
			return string(b)
		}
	case []byte:
		return string(t)
	}
	return fmt.Sprintf("%+v", v)
}
