// Package secret redacts credentials. Values read from the environment are
// secrets: the tool gateway redacts them from everything it hands on, and the
// command line from everything it prints and logs, through Redactor.Handler.
//
// # Role in the architecture
//
// internal/config builds the one Redactor of the server from every value it
// reads from the environment, among them the model provider's API key, the
// users' API tokens and, where they are set from the environment, the
// database URL and the MCP servers' environment variables. That
// redactor goes to every tool gateway, which redacts tool results, errors,
// denial reasons, approval requests and audit records; to the server, which
// redacts run output, errors, transcripts and conversation history before
// they are stored or sent; and, through Handler, to the server's logger. The
// command-line client builds one from its own API token for its logger. The
// package depends on nothing in Agenty but internal/must.
//
// # What it contains
//
// Redactor replaces each secret, in every form it can take in text and JSON,
// with "[redacted]". Handler wraps a log/slog handler so every log record
// passes through the same Redactor before it is written.
//
// # Trust-model guarantees
//
// This package is the mechanism behind guarantee 5, credentials never reach
// the model, prompts, logs or tool output. Redaction is by known value, not
// by pattern: Agenty knows every credential it holds, as each comes from the
// environment, so it can redact them exactly, with no false negatives for
// credentials that look unlike any pattern. A secret Agenty never read is
// not its to redact; tools still receive their arguments unchanged.
package secret

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/jangraefen/agenty/internal/must"
)

// redacted replaces every secret. It is a fixed marker, not a hash or a
// prefix of the secret, so nothing about the secret can be learnt from it.
const redacted = "[redacted]"

// MinLength is the shortest secret a Redactor redacts: shorter values would
// also match ordinary text.
const MinLength = 8

// Redactor replaces known secrets with "[redacted]". It is immutable and safe
// for concurrent use, so one Redactor serves every run and the logger at once.
type Redactor struct {
	replacer *strings.Replacer
}

// NewRedactor returns a Redactor for secrets, each at least MinLength
// characters long. A Redactor built from none redacts nothing, which callers
// use where no secret is known, rather than leave the Redactor out.
//
// It builds one strings.Replacer for every form of every secret, so redacting
// is a single pass over the text however many secrets there are.
func NewRedactor(secrets []string) (*Redactor, error) {
	// Refuse short secrets rather than skip them: a secret that is silently
	// not redacted would break guarantee 5 without anyone noticing.
	for i, s := range secrets {
		if len(s) < MinLength {
			return nil, fmt.Errorf("secret %d is shorter than %d characters", i, MinLength)
		}
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
// JSON string, with and without HTML escaping. Tool results and audit records
// are JSON, so a secret holding a quote, a backslash or "<" would otherwise
// slip through in its escaped form. Go's encoder escapes <, > and & by
// default and other encoders do not, hence both forms.
func jsonForms(s string) []string {
	quoted := string(must.Value(json.Marshal(s)))
	escaped := quoted[1 : len(quoted)-1]
	forms := []string{s, escaped, unescapeHTML.Replace(escaped)}
	slices.Sort(forms)
	return slices.Compact(forms)
}

// unescapeHTML undoes the HTML escaping json.Marshal adds.
var unescapeHTML = strings.NewReplacer(`\u003c`, "<", `\u003e`, ">", `\u0026`, "&")

// String redacts s: every form of every secret becomes "[redacted]".
func (r *Redactor) String(s string) string {
	return r.replacer.Replace(s)
}

// JSON redacts raw JSON. If redaction breaks the JSON, for example a secret
// that was a number, the redacted text is returned as a JSON string.
//
// Redacting the raw text, rather than decoding and walking the value, also
// catches a secret inside an object key, and keeps the JSON byte for byte
// when there is nothing to redact. Whatever happens, the result is valid
// JSON, as callers store it and hand it on as JSON, such as a tool result
// for the model.
func (r *Redactor) JSON(raw json.RawMessage) json.RawMessage {
	out := r.replacer.Replace(string(raw))
	if out == string(raw) {
		return raw
	}
	if json.Valid([]byte(out)) {
		return json.RawMessage(out)
	}
	return must.Value(json.Marshal(out))
}

// Error redacts err's message and keeps err in the chain, so callers can still
// match it with errors.Is. The wrapped error's own message is not redacted:
// print the returned error, never what it unwraps to.
func (r *Redactor) Error(err error) error {
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

// redactedError is an error whose message has been redacted. It keeps the
// original error for errors.Is and errors.As, so callers can still tell, for
// example, a context cancellation from a tool failure.
type redactedError struct {
	msg string
	err error
}

// Error returns the redacted message.
func (e *redactedError) Error() string { return e.msg }

// Unwrap returns the original, unredacted error; see Redactor.Error.
func (e *redactedError) Unwrap() error { return e.err }
