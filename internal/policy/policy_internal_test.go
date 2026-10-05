package policy

import (
	"testing"

	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/stretchr/testify/require"
)

func TestRuleSets_RequiresExactlyOneResult(t *testing.T) {
	one := rego.Result{Bindings: rego.Vars{"deny": []any{}, "require_approval": []any{}}}
	for name, rs := range map[string]rego.ResultSet{"none": {}, "two": {one, one}} {
		t.Run(name, func(t *testing.T) {
			_, _, err := ruleSets(rs)
			require.ErrorContains(t, err, "expected one result")
		})
	}
}
