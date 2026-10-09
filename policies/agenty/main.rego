# Agenty's base authorization policy. M0 denies everything; M4 adds the real rules.
# `decision.outcome` is one of "allow", "require_approval", "deny".
package agenty.authz

default decision := {
	"outcome": "deny",
	"reason": "no policy matches",
}
