package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/anthropic"
	"github.com/jangraefen/agenty/internal/model/anthropic/anthropictest"
	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

// TestInvariant_CredentialsNeverReachModel guards trust-model guarantee 5:
// credentials are used only at call time and never reach the model, the audit
// log or the run's result, even when a tool echoes one back.
func TestInvariant_CredentialsNeverReachModel(t *testing.T) {
	const (
		apiKey   = "sk-ant-api-key-0123456789"
		mcpToken = "mcp-token-abcdef0123"
	)
	secrets := []string{apiKey, mcpToken}
	api := anthropictest.New(t,
		anthropictest.Reply(t, "tool_use",
			anthropictest.ToolUseBlock("toolu_1", "vault_read", map[string]any{}),
			anthropictest.ToolUseBlock("toolu_2", "vault_login", map[string]any{}),
		),
		anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("done")),
	)
	m, err := anthropic.New(anthropic.Config{APIKey: apiKey, Model: "claude-test", MaxTokens: 1024, BaseURL: api.URL})
	require.NoError(t, err)
	leakyResult := &gatewaytest.Tool{Name: "vault_read", Result: json.RawMessage(`{"token":"` + mcpToken + `","key":"` + apiKey + `"}`)}
	leakyError := &gatewaytest.Tool{Name: "vault_login", Err: errors.New("login with " + mcpToken + " failed")}
	audit := &gatewaytest.Audit{}
	f := newFixture(3)
	f.harness.Tools = []string{"vault_read", "vault_login"}
	a, err := agent.New(context.Background(), agent.Config{
		Harness: f.harness,
		Model:   m,
		Tools:   []toolgateway.Tool{leakyResult, leakyError},
		Audit:   audit,
		Secrets: secrets,
	})
	require.NoError(t, err)

	res, err := a.Run(context.Background(), "rotate the token")
	require.NoError(t, err)

	assertNoSecret := func(what string, b []byte) {
		t.Helper()
		for _, s := range secrets {
			assert.NotContains(t, string(b), s, what)
		}
	}
	reqs := api.Requests()
	require.Len(t, reqs, 2)
	for _, r := range reqs {
		assert.Equal(t, apiKey, r.Header.Get("X-Api-Key"), "the API key travels only in its header")
		assertNoSecret("model request body", r.Body)
	}
	assert.Contains(t, string(reqs[1].Body), "[redacted]", "the tool results reached the model, redacted")
	auditJSON, err := json.Marshal(audit.Records)
	require.NoError(t, err)
	assertNoSecret("audit records", auditJSON)
	resultJSON, err := json.Marshal(res)
	require.NoError(t, err)
	assertNoSecret("run result", resultJSON)
}

func TestNew_RejectsSecretsTooShortToRedact(t *testing.T) {
	f := newFixture(3)
	cfg := f.config(model.NewScripted())
	cfg.Secrets = []string{"short"}

	a, err := agent.New(context.Background(), cfg)

	require.ErrorContains(t, err, "secret 0 is shorter than 8 characters")
	assert.Nil(t, a)
}
