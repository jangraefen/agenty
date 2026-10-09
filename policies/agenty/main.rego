# Agenty's base authorization policy. M0 denies everything; M4 adds the real rules.
package agenty.authz

default decision := {
	"allow": false,
	"reason": "no policy matches",
	"require_approval": false,
}
