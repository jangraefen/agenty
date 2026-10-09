package cli

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/harness"
)

// applyUsage is the usage text of "agenty apply".
const applyUsage = `usage: agenty apply [flags] FILE

Stores the harness in FILE, with its policy files, on the server. A changed
harness becomes its next version; an unchanged one is left as it is.
Harnesses belong to the workspace they are applied to.
`

// apply implements "agenty apply": it loads a harness file and stores it in
// the workspace with PUT /v1/workspaces/{ws}/harnesses/{name}, then prints
// the version the server assigned.
//
// The file is loaded and validated locally with harness.Load, which also
// reads the harness's policy files relative to it, so a broken harness fails
// before any request; the server validates it again, as the API is not only
// the CLI's. Versioning is the server's decision: an unchanged harness keeps
// its version, so apply is safe to repeat.
func apply(ctx context.Context, args []string, env Env) int {
	flags, rest, code, ok := parseFlags(command{name: "apply", usage: applyUsage, nargs: 1, client: true, workspace: true}, args, env)
	if !ok {
		return code
	}
	logger, err := clientLogger(env, flags)
	if err != nil {
		return fail(logger, "apply failed", err)
	}
	c, err := newClient(flags)
	if err != nil {
		return fail(logger, "apply failed", err)
	}
	h, err := harness.Load(rest[0])
	if err != nil {
		return fail(logger, "apply failed", err)
	}
	var v api.HarnessVersion
	if err := c.do(ctx, http.MethodPut, c.path("harnesses", h.Name), api.FromHarness(*h), &v); err != nil {
		return fail(logger, "apply failed", err)
	}
	if _, err := fmt.Fprintf(env.Stdout, "%s version %d\n", h.Name, v.Version); err != nil {
		return fail(logger, "apply failed", err)
	}
	return exitOK
}
