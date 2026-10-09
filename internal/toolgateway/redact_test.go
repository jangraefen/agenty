package toolgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

const (
	apiKey   = "sk-ant-key-0123456789"
	apiKeyV2 = "sk-ant-key-0123456789-v2"
	quoted   = `pa"ss\word-123`
)

// newRedactingGateway returns a gateway that grants and allows tool, records
// to audit, and redacts apiKey, apiKeyV2 and quoted.
func newRedactingGateway(t *testing.T, tool *gatewaytest.Tool, audit *gatewaytest.Audit) *toolgateway.Gateway {
	t.Helper()
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		RunID:        "r1",
		Granted:      []string{tool.Name},
		Servers:      gatewaytest.Servers(tool),
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Audit:        audit,
		Redactor:     redactor(t, apiKey, apiKeyV2, quoted),
	})
	require.NoError(t, err)
	return gw
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
			gw := newRedactingGateway(t, &gatewaytest.Tool{Name: "vault_read", Result: tt.result}, audit)

			got, err := gw.Call(context.Background(), toolgateway.ToolCall{Name: "vault_read"})

			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
			require.Len(t, audit.Records, 2)
			assert.JSONEq(t, tt.want, string(audit.Records[1].Result))
		})
	}
}

func TestCall_RedactedResultThatIsNoLongerJSONBecomesAString(t *testing.T) {
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		RunID:        "r1",
		Granted:      []string{"vault_read"},
		Servers:      gatewaytest.Servers(&gatewaytest.Tool{Name: "vault_read", Result: json.RawMessage(`{"pin":12345678}`)}),
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Audit:        &gatewaytest.Audit{},
		Redactor:     redactor(t, "12345678"),
	})
	require.NoError(t, err)

	got, err := gw.Call(context.Background(), toolgateway.ToolCall{Name: "vault_read"})

	require.NoError(t, err)
	assert.JSONEq(t, `"{\"pin\":[redacted]}"`, string(got))
}

func TestCall_RedactsSecretsFromToolErrorsAndArgs(t *testing.T) {
	cause := errors.New("login failed for " + apiKey)
	audit := &gatewaytest.Audit{}
	gw := newRedactingGateway(t, &gatewaytest.Tool{Name: "vault_read", Err: cause}, audit)

	_, err := gw.Call(context.Background(), toolgateway.ToolCall{Name: "vault_read", Args: json.RawMessage(`{"key":"` + apiKey + `"}`)})

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

func TestCall_RedactsSecretsWithHTMLCharacters(t *testing.T) {
	htmlSecret := "a<b>c&d-0123"
	for name, result := range map[string]json.RawMessage{
		"HTML-escaped by json.Marshal": json.RawMessage(`{"v":"a\u003cb\u003ec\u0026d-0123"}`),
		"not HTML-escaped":             json.RawMessage(`{"v":"a<b>c&d-0123"}`),
	} {
		t.Run(name, func(t *testing.T) {
			gw, err := toolgateway.New(context.Background(), toolgateway.Config{
				RunID:        "r1",
				Granted:      []string{"vault_read"},
				Servers:      gatewaytest.Servers(&gatewaytest.Tool{Name: "vault_read", Result: result}),
				MaxToolCalls: 100,
				Policy:       &gatewaytest.Policy{},
				Audit:        &gatewaytest.Audit{},
				Redactor:     redactor(t, htmlSecret),
			})
			require.NoError(t, err)

			got, err := gw.Call(context.Background(), toolgateway.ToolCall{Name: "vault_read"})

			require.NoError(t, err)
			assert.JSONEq(t, `{"v":"[redacted]"}`, string(got))
		})
	}
}

// TestCall_ASuspensionHoldsNoSecret: the approver is a person, so the
// request a suspended call waits with names no secret, neither in its
// arguments nor in the reasons policy gave. A secret in the arguments
// denies the call instead, see TestInvariant_ACallHoldingASecretNeverWaits.
func TestCall_ASuspensionHoldsNoSecret(t *testing.T) {
	audit := &gatewaytest.Audit{}
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		RunID:        "r1",
		Granted:      []string{"vault_write"},
		Servers:      gatewaytest.Servers(&gatewaytest.Tool{Name: "vault_write"}),
		MaxToolCalls: 100,
		Policy: &gatewaytest.Policy{Verdicts: map[string]toolgateway.Verdict{
			"vault_write": {Decision: toolgateway.RequireApproval, Reasons: []string{"writes " + apiKey + " to the vault"}},
		}},
		Audit:    audit,
		Redactor: redactor(t, apiKey),
	})
	require.NoError(t, err)

	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "vault_write", Args: json.RawMessage(`{"key":"vault-key-7"}`)})

	var suspended *toolgateway.Suspended
	require.ErrorAs(t, err, &suspended)
	assert.JSONEq(t, `{"key":"vault-key-7"}`, string(suspended.Request.Args))
	assert.Equal(t, []string{"writes [redacted] to the vault"}, suspended.Reasons, "a person sees the reasons, never the secret")
	assert.NotContains(t, suspended.Error(), apiKey)
	require.Len(t, audit.Records, 1)
	assert.Equal(t, "policy: writes [redacted] to the vault", audit.Records[0].Reason)
}

// redactor returns a Redactor for secrets.
func redactor(t *testing.T, secrets ...string) *secret.Redactor {
	t.Helper()
	r, err := secret.NewRedactor(secrets)
	require.NoError(t, err)
	return r
}
