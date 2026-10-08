package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/jangraefen/agenty/internal/auditlog"
	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/secret"
)

const auditUsage = `usage: agenty audit export [flags] > FILE
       agenty audit verify [--anchor ID:HASH]... FILE

export writes the audit log, as an auditor, as JSON lines: every event with
the hashes that chain them. verify checks such a file without the server:
that no event in it was changed, removed or moved. It prints the last
event's id and hash: keep that anchor apart from Agenty. A later export
verified with --anchor must still hold it, which shows that the log was not
written anew in between. Removing the newest events, or adding forged ones
after the last anchor, does not show: keep anchors often.

The server marks a complete export with an HTTP trailer; a proxy between
that drops trailers makes export fail as cut short.
`

func audit(ctx context.Context, args []string, env Env) int {
	if len(args) == 0 {
		return printAuditUsage(env.Stderr, "", exitUsage)
	}
	switch args[0] {
	case "export":
		return auditExport(ctx, args[1:], env)
	case "verify":
		return auditVerify(args[1:], env)
	case "help", "-h", "--help":
		return printAuditUsage(env.Stderr, "", exitOK)
	default:
		return printAuditUsage(env.Stderr, fmt.Sprintf("unknown audit command %q\n", args[0]), exitUsage)
	}
}

func printAuditUsage(w io.Writer, msg string, code int) int {
	if _, err := io.WriteString(w, msg+auditUsage); err != nil {
		return exitFailure
	}
	return code
}

func auditExport(ctx context.Context, args []string, env Env) int {
	var after int64
	flags, _, code, ok := parseFlags(command{name: "audit export", usage: auditUsage, client: true, signer: "an auditor's", define: func(fs *flag.FlagSet) {
		fs.Int64Var(&after, "after", 0, "start after the event with this `id`")
	}}, args, env)
	if !ok {
		return code
	}
	logger, err := clientLogger(env, flags)
	if err != nil {
		return fail(logger, "export failed", err)
	}
	c, err := newClient(flags)
	if err != nil {
		return fail(logger, "export failed", err)
	}
	if err := c.export(ctx, after, env.Stdout); err != nil {
		return fail(logger, "export failed", err)
	}
	return exitOK
}

// export copies the audit log after the given event to w, failing unless
// the server said it was complete.
func (c *client) export(ctx context.Context, after int64, w io.Writer) (err error) {
	path := "/v1/audit/export?after=" + strconv.FormatInt(after, 10)
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("GET %s: %w", path, cerr))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return responseError(resp)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	if resp.Trailer.Get("Audit-Export-Complete") != "true" {
		return errors.New("the export was cut short: the server did not finish it")
	}
	return nil
}

// anchors collects the --anchor flags.
type anchors []auditlog.Anchor

func (a *anchors) String() string { return fmt.Sprint(*a) }

func (a *anchors) Set(s string) error {
	anchor, err := auditlog.ParseAnchor(s)
	if err != nil {
		return err
	}
	*a = append(*a, anchor)
	return nil
}

func auditVerify(args []string, env Env) int {
	var want anchors
	_, rest, code, ok := parseFlags(command{name: "audit verify", usage: auditUsage, nargs: 1, define: func(fs *flag.FlagSet) {
		fs.Var(&want, "anchor", "an event's `ID:HASH` kept from an earlier export, which this one must hold; repeatable")
	}}, args, env)
	if !ok {
		return code
	}
	logger := newLogger(env.Stderr, slog.LevelInfo, must.Value(secret.NewRedactor(nil)))
	summary, err := verifyFile(rest[0], want)
	if err != nil {
		return fail(logger, "verify failed", err)
	}
	note := ""
	if summary.First > 1 && len(want) == 0 {
		note = fmt.Sprintf("\nnote: the export starts at event %d; verify it with --anchor %d:HASH from an earlier export to link it", summary.First, summary.First-1)
	}
	if _, err := fmt.Fprintf(env.Stdout, "verified %d events, %d to %d\nanchor: %s%s\n", summary.Events, summary.First, summary.Last,
		auditlog.Anchor{ID: summary.Last, Hash: summary.LastHash}, note); err != nil {
		return fail(logger, "verify failed", err)
	}
	return exitOK
}

func verifyFile(name string, want []auditlog.Anchor) (summary auditlog.Summary, err error) {
	f, err := os.Open(name) //nolint:gosec // G304: the user names the file to verify.
	if err != nil {
		return auditlog.Summary{}, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return auditlog.Verify(f, want...)
}
