package toolgateway

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/jangraefen/agenty/internal/must"
)

const redacted = "[redacted]"

// MinSecretLength is the shortest secret the gateway redacts: shorter values
// would also match ordinary text.
const MinSecretLength = 8

// Redactor replaces known secrets with "[redacted]". The gateway uses one for
// everything it hands on: tool results, tool errors and audit records.
type Redactor struct {
	replacer *strings.Replacer
}

// validateSecrets checks that every secret is long enough to redact without
// also matching ordinary text.
func validateSecrets(secrets []string) error {
	for i, s := range secrets {
		if len(s) < MinSecretLength {
			return fmt.Errorf("toolgateway: secret %d is shorter than %d characters", i, MinSecretLength)
		}
	}
	return nil
}

// NewRedactor returns a Redactor for secrets, each at least MinSecretLength
// characters long.
func NewRedactor(secrets []string) (*Redactor, error) {
	if err := validateSecrets(secrets); err != nil {
		return nil, err
	}
	// Longest first: the replacer tries pairs in order, so a secret that
	// contains another one is replaced whole.
	sorted := slices.SortedFunc(slices.Values(secrets), func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	var pairs []string
	for _, s := range sorted {
		for _, form := range jsonForms(s) {
			pairs = append(pairs, form, redacted)
		}
	}
	return &Redactor{replacer: strings.NewReplacer(pairs...)}, nil
}

// jsonForms returns s as it may appear in text: as is, and escaped inside a
// JSON string, with and without HTML escaping.
func jsonForms(s string) []string {
	quoted := string(must.Value(json.Marshal(s)))
	escaped := quoted[1 : len(quoted)-1]
	forms := []string{s, escaped, unescapeHTML.Replace(escaped)}
	slices.Sort(forms)
	return slices.Compact(forms)
}

// unescapeHTML undoes the HTML escaping json.Marshal adds.
var unescapeHTML = strings.NewReplacer(`\u003c`, "<", `\u003e`, ">", `\u0026`, "&")

// String redacts s.
func (r *Redactor) String(s string) string {
	return r.replacer.Replace(s)
}

// json redacts raw JSON. If redaction breaks the JSON, for example a secret
// that was a number, the redacted text is returned as a JSON string.
func (r *Redactor) json(raw json.RawMessage) json.RawMessage {
	out := r.replacer.Replace(string(raw))
	if out == string(raw) {
		return raw
	}
	if json.Valid([]byte(out)) {
		return json.RawMessage(out)
	}
	return must.Value(json.Marshal(out))
}

// error redacts err's message and keeps err in the chain, so callers can still
// match it with errors.Is. The wrapped error's own message is not redacted:
// print the returned error, never what it unwraps to.
func (r *Redactor) error(err error) error {
	if err == nil {
		return nil
	}
	msg := r.String(err.Error())
	if msg == err.Error() {
		return err
	}
	return &redactedError{msg: msg, err: err}
}

var _ error = (*redactedError)(nil) //nolint:errcheck // an interface guard, not a discarded error

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }
