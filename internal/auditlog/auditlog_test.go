package auditlog_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/auditlog"
)

// chain links events as the store appends them, from the given previous
// event's hash.
func chain(t *testing.T, prev auditlog.Hash, firstID int64, details ...string) []auditlog.Event {
	t.Helper()
	at := time.Date(2026, 10, 8, 12, 0, 0, 123456000, time.UTC)
	out := make([]auditlog.Event, len(details))
	for i, d := range details {
		canonical, err := auditlog.Canonical([]byte(d))
		require.NoError(t, err)
		e := auditlog.Event{
			ID:         firstID + int64(i),
			RecordedAt: at.Add(time.Duration(i) * time.Second),
			Actor:      "alice",
			Action:     "tool.result",
			Workspace:  "home",
			RunID:      "R1",
			Details:    canonical,
			PrevHash:   prev,
		}
		e.Hash = e.Sum()
		prev = e.Hash
		out[i] = e
	}
	return out
}

func export(t *testing.T, events []auditlog.Event) string {
	t.Helper()
	var b bytes.Buffer
	for _, e := range events {
		require.NoError(t, auditlog.WriteLine(&b, e))
	}
	return b.String()
}

func TestCanonical_IsStableThroughAnExport(t *testing.T) {
	for _, details := range []string{
		`{"result":"<p>a & b</p>"}`,
		"{\"result\":\"line\u2028separator\u2029\"}",
		`{"result":"nul \u0000 char"}`,
		`{"a":1,"a":2}`,
		`{"result":"Grüße, 日本"}`,
		`{"n":123456789012345678901234567890,"f":1.50}`,
		`{ "spaced" : [ 1, 2 ] }`,
	} {
		t.Run(details, func(t *testing.T) {
			events := chain(t, auditlog.Hash{}, 1, details)
			got, err := auditlog.Verify(strings.NewReader(export(t, events)))
			require.NoError(t, err)
			assert.Equal(t, events[0].Hash, got.LastHash)
			again, err := auditlog.Canonical(events[0].Details)
			require.NoError(t, err)
			assert.Equal(t, string(events[0].Details), string(again), "canonical details are canonical")
		})
	}
	_, err := auditlog.Canonical([]byte(`{"a":`))
	require.Error(t, err)
}

func TestVerify(t *testing.T) {
	events := chain(t, auditlog.Hash{}, 1, `{"a":1}`, `{"b":2}`, `{"c":3}`, `{"d":4}`)
	whole := export(t, events)

	got, err := auditlog.Verify(strings.NewReader(whole))
	require.NoError(t, err)
	assert.Equal(t, auditlog.Summary{Events: 4, First: 1, Last: 4, LastHash: events[3].Hash}, got)

	lines := strings.SplitAfter(whole, "\n")
	tests := []struct {
		name    string
		export  string
		anchors []auditlog.Anchor
		wantErr string
	}{
		{"changed details", strings.Replace(whole, `{"b":2}`, `{"b":3}`, 1), nil, "event 2"},
		{"changed actor", strings.Replace(whole, `"actor":"alice"`, `"actor":"mallory"`, 1), nil, "event 1"},
		{"removed event", lines[0] + lines[2] + lines[3], nil, "event 3"},
		{"reordered", lines[0] + lines[2] + lines[1] + lines[3], nil, "event 3"},
		{"a first event that does not start the chain", lines[0][:0] + strings.Replace(lines[0], `"prev_hash":"`+events[0].PrevHash.String(), `"prev_hash":"`+events[1].Hash.String(), 1), nil, "event 1"},
		{"not JSON", "nonsense\n", nil, "line 1"},
		{"empty", "", nil, "no events"},
		{"an anchor that is not in it", whole, []auditlog.Anchor{{ID: 9, Hash: events[0].Hash}}, "anchor 9"},
		{"an anchor whose hash differs", whole, []auditlog.Anchor{{ID: 2, Hash: events[0].Hash}}, "anchor 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auditlog.Verify(strings.NewReader(tt.export), tt.anchors...)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}

	t.Run("a partial export against an anchor", func(t *testing.T) {
		rest := lines[2] + lines[3]
		got, err := auditlog.Verify(strings.NewReader(rest), auditlog.Anchor{ID: 2, Hash: events[1].Hash})
		require.NoError(t, err, "the anchor before its first event links it")
		assert.Equal(t, int64(3), got.First)
		_, err = auditlog.Verify(strings.NewReader(rest), auditlog.Anchor{ID: 2, Hash: events[0].Hash})
		require.ErrorContains(t, err, "anchor 2")
		_, err = auditlog.Verify(strings.NewReader(rest), auditlog.Anchor{ID: 3, Hash: events[2].Hash})
		require.NoError(t, err)
	})

	t.Run("a rewrite that recomputes every hash is caught by an anchor", func(t *testing.T) {
		forged := chain(t, auditlog.Hash{}, 1, `{"a":1}`, `{"b":"forged"}`, `{"c":3}`, `{"d":4}`)
		_, err := auditlog.Verify(strings.NewReader(export(t, forged)))
		require.NoError(t, err, "without an anchor a consistent rewrite verifies")
		_, err = auditlog.Verify(strings.NewReader(export(t, forged)), auditlog.Anchor{ID: 3, Hash: events[2].Hash})
		require.ErrorContains(t, err, "anchor 3")
	})
}

func TestAnchor_Parse(t *testing.T) {
	events := chain(t, auditlog.Hash{}, 1, `{}`)
	a, err := auditlog.ParseAnchor("1:" + events[0].Hash.String())
	require.NoError(t, err)
	assert.Equal(t, auditlog.Anchor{ID: 1, Hash: events[0].Hash}, a)
	assert.Equal(t, "1:"+events[0].Hash.String(), a.String())
	for _, bad := range []string{"", "1", "x:00", "1:zz", "1:00", "0:" + events[0].Hash.String()} {
		_, err := auditlog.ParseAnchor(bad)
		assert.Error(t, err, bad)
	}
}

func TestWriteLine_IsTheExportFormat(t *testing.T) {
	events := chain(t, auditlog.Hash{}, 1, `{"a":1}`)
	line := export(t, events)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(line), &fields))
	for _, key := range []string{"id", "recorded_at", "actor", "action", "workspace", "run_id", "target", "details", "prev_hash", "hash"} {
		assert.Contains(t, fields, key)
	}
	assert.True(t, strings.HasSuffix(line, "\n"))
	assert.Equal(t, `"0000000000000000000000000000000000000000000000000000000000000000"`, string(fields["prev_hash"]))
}
