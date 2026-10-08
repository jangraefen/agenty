package cli_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAudit_ExportAndVerify(t *testing.T) {
	f := newFixture(t, toolUse(t, "c1", "files_read", map[string]string{"path": "notes.md"}), done(t))
	require.Equal(t, 0, f.run("tidy my notes"), f.stderr.String())
	f.stdout.Reset()

	require.Equal(t, 0, f.main("audit", "export", "--server", f.serverURL()), f.stderr.String())
	export := f.stdout.String()
	require.NotEmpty(t, export)
	f.writeFile(t, "audit.jsonl", export)
	f.stdout.Reset()

	require.Equal(t, 0, f.main("audit", "verify", f.path("audit.jsonl")), f.stderr.String())
	out := f.stdout.String()
	assert.Contains(t, out, "verified 2 events, 1 to 2")
	anchor := strings.TrimSpace(out[strings.Index(out, "anchor: ")+len("anchor: "):])
	assert.Regexp(t, `^2:[0-9a-f]{64}$`, anchor, "the last event's id and hash, to keep")
	f.stdout.Reset()

	require.Equal(t, 0, f.main("audit", "verify", "--anchor", anchor, f.path("audit.jsonl")), f.stderr.String())
	f.stdout.Reset()

	f.writeFile(t, "tampered.jsonl", strings.Replace(export, `"files_read"`, `"files_write"`, 1))
	assert.Equal(t, 1, f.main("audit", "verify", f.path("tampered.jsonl")))
	assert.Contains(t, f.stderr.String(), "event 1")

	f.stderr.Reset()
	other := "1:" + strings.Repeat("0", 64)
	assert.Equal(t, 1, f.main("audit", "verify", "--anchor", other, f.path("audit.jsonl")))
	assert.Contains(t, f.stderr.String(), "anchor 1")
}

func TestAudit_Failures(t *testing.T) {
	f := newFixture(t)
	for _, tt := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"no subcommand", []string{"audit"}, 2, "usage: agenty audit"},
		{"unknown subcommand", []string{"audit", "erase"}, 2, `unknown audit command "erase"`},
		{"verify without a file", []string{"audit", "verify"}, 2, "expected 1 argument"},
		{"a malformed anchor", []string{"audit", "verify", "--anchor", "x", "f"}, 2, "anchor"},
		{"a file that does not exist", []string{"audit", "verify", "missing.jsonl"}, 1, "missing.jsonl"},
		{"export by a user who is no auditor", []string{"audit", "export", "--server", f.serverURL()}, 1, "only auditors"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f.vars["AGENTY_TOKEN"] = bobToken
			f.stderr.Reset()
			assert.Equal(t, tt.code, f.main(tt.args...))
			assert.Contains(t, f.stderr.String(), tt.want)
		})
	}
}
