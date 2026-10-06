package audit_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/audit"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

func lines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // G304: a file the test created
	require.NoError(t, err)
	defer func() { assert.NoError(t, f.Close()) }()
	var out []map[string]any
	s := bufio.NewScanner(f)
	for s.Scan() {
		var m map[string]any
		require.NoError(t, json.Unmarshal(s.Bytes(), &m), "every line is one JSON record")
		out = append(out, m)
	}
	require.NoError(t, s.Err())
	return out
}

func TestFile_WritesOneJSONLinePerRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	f, err := audit.Open(path)
	require.NoError(t, err)

	before := time.Now()
	require.NoError(t, f.Record(context.Background(), toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventDecision, Tool: "tickets_read", Decision: toolgateway.Allow}))
	require.NoError(t, f.Record(context.Background(), toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventResult, Tool: "tickets_read", Decision: toolgateway.Allow, Result: json.RawMessage(`{"title":"x"}`)}))
	require.NoError(t, f.Close())

	got := lines(t, path)
	require.Len(t, got, 2)
	assert.Equal(t, "decision", got[0]["event"])
	assert.Equal(t, "result", got[1]["event"])
	assert.Equal(t, map[string]any{"title": "x"}, got[1]["result"])
	ts, err := time.Parse(time.RFC3339Nano, got[0]["time"].(string))
	require.NoError(t, err)
	assert.False(t, ts.Before(before.Truncate(time.Second)))
}

func TestOpen_AppendsAndKeepsTheFilePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	for _, run := range []string{"r1", "r2"} {
		f, err := audit.Open(path)
		require.NoError(t, err)
		require.NoError(t, f.Record(context.Background(), toolgateway.Record{RunID: run, Event: toolgateway.EventDecision}))
		require.NoError(t, f.Close())
	}

	got := lines(t, path)
	require.Len(t, got, 2, "a second run appends instead of overwriting")
	assert.Equal(t, "r2", got[1]["run_id"])
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestFile_Errors(t *testing.T) {
	_, err := audit.Open(filepath.Join(t.TempDir(), "missing", "audit.jsonl"))
	require.ErrorContains(t, err, "audit:")

	f, err := audit.Open(filepath.Join(t.TempDir(), "audit.jsonl"))
	require.NoError(t, err)
	require.NoError(t, f.Close())
	err = f.Record(context.Background(), toolgateway.Record{RunID: "r1"})
	require.ErrorContains(t, err, "audit:", "a closed file fails, so the gateway does not execute")
	require.ErrorContains(t, f.Close(), "audit:", "closing twice is an error")

	f, err = audit.Open(filepath.Join(t.TempDir(), "audit.jsonl"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, f.Close()) })
	err = f.Record(context.Background(), toolgateway.Record{RunID: "r1", Result: json.RawMessage(`{not json`)})
	require.ErrorContains(t, err, "audit:")
}
