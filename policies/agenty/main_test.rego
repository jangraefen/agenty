package agenty.authz_test

import data.agenty.authz

deny := {"outcome": "deny", "reason": "no policy matches"}

outcomes := {"allow", "require_approval", "deny"}

test_denies_without_input if {
	authz.decision == deny
}

test_denies_any_tool_call if {
	authz.decision == deny with input as {"tool": {"name": "http.get"}, "args": {}}
}

# The TypeScript side rejects anything else; the policy must only produce known outcomes.
test_outcome_is_known if {
	outcomes[authz.decision.outcome] with input as {"tool": {"name": "http.get"}, "args": {}}
}
