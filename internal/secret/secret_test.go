package secret_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/secret"
)

const (
	apiKey   = "sk-ant-key-0123456789"
	apiKeyV2 = "sk-ant-key-0123456789-v2"
	quoted   = `pa"ss\word-123`
	html     = "a<b>c&d-0123"
)

func newRedactor(t *testing.T) *secret.Redactor {
	t.Helper()
	r, err := secret.NewRedactor([]string{apiKey, apiKeyV2, quoted, html})
	require.NoError(t, err)
	return r
}

func TestNewRedactor_RejectsSecretsTooShortToRedact(t *testing.T) {
	for _, s := range []string{"", "1234567"} {
		r, err := secret.NewRedactor([]string{apiKey, s})
		require.ErrorContains(t, err, "secret 1 is shorter than 8 characters")
		assert.Nil(t, r)
	}
	_, err := secret.NewRedactor([]string{"12345678"})
	assert.NoError(t, err, "MinLength characters are enough")
}

func TestRedactor_String(t *testing.T) {
	r := newRedactor(t)
	tests := []struct {
		name, in, want string
	}{
		{"no secret", "nothing to hide", "nothing to hide"},
		{"plain", "key=" + apiKey + " ok", "key=[redacted] ok"},
		{"a secret containing another is replaced whole", "key=" + apiKeyV2, "key=[redacted]"},
		{"JSON-escaped", `{"p":"pa\"ss\\word-123"}`, `{"p":"[redacted]"}`},
		{"HTML-escaped by json.Marshal", `"a<b>c&d-0123"`, `"[redacted]"`},
		{"not HTML-escaped", html, "[redacted]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, r.String(tt.in))
		})
	}
}

func TestRedactor_String_WithoutSecrets(t *testing.T) {
	r, err := secret.NewRedactor(nil)
	require.NoError(t, err)

	assert.Equal(t, "key="+apiKey, r.String("key="+apiKey))
}

func TestRedactor_JSON(t *testing.T) {
	r := newRedactor(t)
	quotedJSON, err := json.Marshal(map[string]string{"token": quoted})
	require.NoError(t, err)
	tests := []struct {
		name string
		in   json.RawMessage
		want string
	}{
		{"no secret", json.RawMessage(`{"ok":true}`), `{"ok":true}`},
		{"string value", json.RawMessage(`{"token":"` + apiKey + `"}`), `{"token":"[redacted]"}`},
		{"escaped string value", quotedJSON, `{"token":"[redacted]"}`},
		{"empty", nil, ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(r.JSON(tt.in)))
		})
	}
}

func TestRedactor_JSON_ThatIsNoLongerJSONBecomesAString(t *testing.T) {
	r, err := secret.NewRedactor([]string{"12345678"})
	require.NoError(t, err)

	got := r.JSON(json.RawMessage(`{"pin":12345678}`))

	assert.JSONEq(t, `"{\"pin\":[redacted]}"`, string(got))
}

func TestRedactor_Error(t *testing.T) {
	r := newRedactor(t)
	cause := errors.New("login failed for " + apiKey)

	err := r.Error(cause)

	require.EqualError(t, err, "login failed for [redacted]")
	require.ErrorIs(t, err, cause, "redaction keeps the error chain")
	assert.Same(t, cause, errors.Unwrap(err))

	clean := errors.New("nothing to hide")
	assert.Same(t, clean, r.Error(clean), "an error without a secret is returned as is")
	assert.NoError(t, r.Error(nil))
}
