package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/secret"
)

const applyUsage = `usage: agenty apply [flags] FILE

Stores the harness in FILE, with its policy files, on the server. A changed
harness becomes its next version; an unchanged one is left as it is.
`

func apply(ctx context.Context, args []string, env Env) int {
	flags, rest, code, ok := parseClientFlags("apply", applyUsage, 1, args, env.Stderr)
	if !ok {
		return code
	}
	// The client knows no secret; the server redacts what it sends.
	logger := newLogger(env.Stderr, flags.logLevel, must.Value(secret.NewRedactor(nil)))
	c, err := newClient(flags.server)
	if err != nil {
		return fail(logger, "apply failed", err)
	}
	h, err := harness.Load(rest[0])
	if err != nil {
		return fail(logger, "apply failed", err)
	}
	var v api.HarnessVersion
	if err := c.do(ctx, http.MethodPut, "/v1/harnesses/"+url.PathEscape(h.Name), h, &v); err != nil {
		return fail(logger, "apply failed", err)
	}
	if _, err := fmt.Fprintf(env.Stdout, "%s version %d\n", h.Name, v.Version); err != nil {
		return fail(logger, "apply failed", err)
	}
	return exitOK
}
