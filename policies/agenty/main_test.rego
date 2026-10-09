package agenty.authz_test

import data.agenty.authz

deny := {"allow": false, "reason": "no policy matches", "require_approval": false}

test_denies_without_input if {
	authz.decision == deny
}

test_denies_any_tool_call if {
	authz.decision == deny with input as {"tool": {"name": "http.get"}, "args": {}}
}
