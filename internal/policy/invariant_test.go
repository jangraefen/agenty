package policy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// TestInvariant_StrictestWins guards trust-model guarantee 4: policy results
// combine as deny > require_approval > allow, across central and harness
// policy alike.
func TestInvariant_StrictestWins(t *testing.T) {
	rules := map[toolgateway.Decision]string{
		toolgateway.Allow:           ``,
		toolgateway.RequireApproval: `require_approval contains "needs a human" if true`,
		toolgateway.Deny:            `deny contains "forbidden" if true`,
	}
	rank := map[toolgateway.Decision]int{toolgateway.Allow: 0, toolgateway.RequireApproval: 1, toolgateway.Deny: 2}

	for central, centralRules := range rules {
		for harness, harnessRules := range rules {
			want := central
			if rank[harness] > rank[central] {
				want = harness
			}
			t.Run(string(central)+"+"+string(harness), func(t *testing.T) {
				e, err := policy.New(context.Background(), layer("central", centralRules), layer("harness", harnessRules))
				require.NoError(t, err)

				v, err := e.Evaluate(context.Background(), input())

				require.NoError(t, err)
				assert.Equal(t, want, v.Decision)
			})
		}
	}
}

// TestInvariant_PolicyCannotLoosen guards trust-model guarantee 4: builders
// can tighten central policy, never loosen it. Each layer is compiled on its
// own, so a harness layer cannot reach into central rules.
func TestInvariant_PolicyCannotLoosen(t *testing.T) {
	central := layer("central", `exempt contains "tickets.read"

deny contains "writes are frozen" if not exempt[input.tool]`)

	tests := []struct {
		name    string
		harness string
	}{
		{"adds to a central helper set", `exempt contains "tickets.label"`},
		{"declares an allow", `allow := true`},
		{"redefines deny as empty", `deny := set()`},
		{"redefines the helper", `exempt := {"tickets.label"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := policy.New(context.Background(), central, layer("harness", tt.harness))
			require.NoError(t, err)

			v, err := e.Evaluate(context.Background(), input())

			require.NoError(t, err)
			assert.Equal(t, toolgateway.Deny, v.Decision)
			assert.Equal(t, []string{"writes are frozen"}, v.Reasons)
		})
	}
}
