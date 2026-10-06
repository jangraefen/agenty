package cli

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/harness"
)

const applyUsage = `usage: agenty apply [flags] FILE

Stores the harness in FILE, with its policy files, on the server. A changed
harness becomes its next version; an unchanged one is left as it is.
Harnesses belong to the workspace they are applied to.
`

func apply(ctx context.Context, args []string, env Env) int {
	flags, rest, code, ok := parseClientFlags("apply", applyUsage, 1, args, env)
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
	if err := c.do(ctx, http.MethodPut, c.path("harnesses", h.Name), h, &v); err != nil {
		return fail(logger, "apply failed", err)
	}
	if _, err := fmt.Fprintf(env.Stdout, "%s version %d\n", h.Name, v.Version); err != nil {
		return fail(logger, "apply failed", err)
	}
	return exitOK
}
