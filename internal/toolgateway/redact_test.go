package toolgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

const (
	apiKey   = "sk-ant-key-0123456789"
	apiKeyV2 = "sk-ant-key-0123456789-v2"
	quoted   = `pa"ss\word-123`
)

func newRedactingRun(t *testing.T, tool *gatewaytest.Tool, audit *gatewaytest.Audit) *toolgateway.Run {
	t.Helper()
	gw, err := toolgateway.New(toolgateway.Config{
		Granted:      []string{tool.Name},
		Tools:        []toolgateway.Tool{tool},
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Audit:        audit,
		Secrets:      []string{apiKey, apiKeyV2, quoted},
	})
	require.NoError(t, err)
	return gw.Start()
}

func TestCall_RedactsSecretsFromResults(t *testing.T) {
	quotedJSON, err := json.Marshal(map[string]string{"token": quoted})
	require.NoError(t, err)
	tests := []struct {
		name   string
		result json.RawMessage
		want   string
	}{
		{"plain secret", json.RawMessage(`{"key":"` + apiKey + `"}`), `{"key":"[redacted]"}`},
		{"the longer of overlapping secrets wins", json.RawMessage(`{"key":"` + apiKeyV2 + `"}`), `{"key":"[redacted]"}`},
		{"secret escaped inside JSON", quotedJSON, `{"token":"[redacted]"}`},
		{"a result without secrets is left alone", json.RawMessage(`{"n":` + "1234567890" + `}`), `{"n":1234567890}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			audit := &gatewaytest.Audit{}
			run := newRedactingRun(t, &gatewaytest.Tool{Name: "vault_read", Result: tt.result}, audit)

			got, err := run.Call(context.Background(), toolgateway.ToolCall{Name: "vault_read"})

			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
			require.Len(t, audit.Records, 2)
			assert.JSONEq(t, tt.want, string(audit.Records[1].Result))
		})
	}
}

func TestCall_RedactedResultThatIsNoLongerJSONBecomesAString(t *testing.T) {
	gw, err := toolgateway.New(toolgateway.Config{
		Granted:      []string{"vault_read"},
		Tools:        []toolgateway.Tool{&gatewaytest.Tool{Name: "vault_read", Result: json.RawMessage(`{"pin":12345678}`)}},
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Audit:        &gatewaytest.Audit{},
		Secrets:      []string{"12345678"},
	})
	require.NoError(t, err)
	run := gw.Start()

	got, err := run.Call(context.Background(), toolgateway.ToolCall{Name: "vault_read"})

	require.NoError(t, err)
	assert.JSONEq(t, `"{\"pin\":[redacted]}"`, string(got))
}

func TestCall_RedactsSecretsFromToolErrorsAndArgs(t *testing.T) {
	cause := errors.New("login failed for " + apiKey)
	audit := &gatewaytest.Audit{}
	run := newRedactingRun(t, &gatewaytest.Tool{Name: "vault_read", Err: cause}, audit)

	_, err := run.Call(context.Background(), toolgateway.ToolCall{Name: "vault_read", Args: json.RawMessage(`{"key":"` + apiKey + `"}`)})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), apiKey)
	assert.Contains(t, err.Error(), "login failed for [redacted]")
	require.ErrorIs(t, err, cause, "redaction keeps the error chain")
	require.Len(t, audit.Records, 2)
	for _, r := range audit.Records {
		assert.NotContains(t, string(r.Args), apiKey)
		assert.NotContains(t, r.Err, apiKey)
	}
	assert.Equal(t, "login failed for [redacted]", audit.Records[1].Err)
}

func TestNew_RejectsSecretsTooShortToRedact(t *testing.T) {
	for _, secret := range []string{"", "1234567"} {
		_, err := toolgateway.New(toolgateway.Config{
			MaxToolCalls: 100,
			Policy:       &gatewaytest.Policy{},
			Audit:        &gatewaytest.Audit{},
			Secrets:      []string{apiKey, secret},
		})
		require.ErrorContains(t, err, "secret 1 is shorter than 8 characters")
	}
}

func TestCall_RedactsSecretsWithHTMLCharacters(t *testing.T) {
	secret := "a<b>c&d-0123"
	for name, result := range map[string]json.RawMessage{
		"HTML-escaped by json.Marshal": json.RawMessage(`{"v":"a<b>c&d-0123"}`),
		"not HTML-escaped":             json.RawMessage(`{"v":"a<b>c&d-0123"}`),
	} {
		t.Run(name, func(t *testing.T) {
			gw, err := toolgateway.New(toolgateway.Config{
				Granted:      []string{"vault_read"},
				Tools:        []toolgateway.Tool{&gatewaytest.Tool{Name: "vault_read", Result: result}},
				MaxToolCalls: 100,
				Policy:       &gatewaytest.Policy{},
				Audit:        &gatewaytest.Audit{},
				Secrets:      []string{secret},
			})
			require.NoError(t, err)
			run := gw.Start()

			got, err := run.Call(context.Background(), toolgateway.ToolCall{Name: "vault_read"})

			require.NoError(t, err)
			assert.JSONEq(t, `{"v":"[redacted]"}`, string(got))
		})
	}
}
