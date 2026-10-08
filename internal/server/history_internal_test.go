package server

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/store"
)

// turn is one run of a simulated conversation: the prompt digest it runs
// with, whether the secret is configured, and whether its input holds the
// secret.
type turn struct {
	digest     string
	configured bool
	pasted     bool
	// noDigests stores the run without digests, as a run that never ran
	// has none.
	noDigests bool
	// tools has the run call a tool before it answers.
	tools bool
}

const simSecret = "sk-simulated-0123456789"

// TestConversationHistory_SendsOnlyValidProviderForms simulates
// conversations, run by run, as a provider that binds a reply's reasoning
// to everything sent before it sees them. A reply in its provider form is
// valid only if everything before it is exactly as when it was written:
// what its run was sent, then its run's own messages as the model saw them.
// Every run of every conversation must be sent only valid provider forms,
// and the last run keeps the forms of the runs listed.
func TestConversationHistory_SendsOnlyValidProviderForms(t *testing.T) {
	tests := []struct {
		name  string
		turns []turn
		kept  []int
	}{
		{"nothing changes", []turn{{digest: "a"}, {digest: "a"}, {digest: "a"}, {digest: "a"}}, []int{0, 1, 2}},
		{"the harness changes", []turn{{digest: "a"}, {digest: "a"}, {digest: "b"}, {digest: "b"}}, []int{2}},
		{"the harness changes back", []turn{{digest: "a"}, {digest: "b"}, {digest: "a"}, {digest: "a"}}, []int{2}},
		{"a secret is pasted", []turn{
			{digest: "a", configured: true}, {digest: "a", configured: true, pasted: true}, {digest: "a", configured: true}, {digest: "a", configured: true},
		}, []int{2}},
		{"a secret is configured later", []turn{
			{digest: "a", pasted: true}, {digest: "a", configured: true}, {digest: "a", configured: true}, {digest: "a", configured: true},
		}, []int{1, 2}},
		{"a secret is configured, then no longer", []turn{
			{digest: "a", pasted: true}, {digest: "a", configured: true}, {digest: "a"}, {digest: "a"},
		}, []int{2}},
		{"runs without digests", []turn{{digest: "a", noDigests: true}, {digest: "a"}, {digest: "a"}}, []int{1}},
		{"runs that call tools", []turn{{digest: "a", tools: true}, {digest: "a"}, {digest: "a", tools: true}, {digest: "a"}}, []int{0, 1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			history, seen := simulate(t, tt.turns)
			assert.Equal(t, tt.kept, keptRuns(history, seen[:len(seen)-1]))
		})
	}
}

// TestConversationHistory_SendsOnlyValidProviderFormsAtRandom simulates
// random conversations, which must also be sent only valid forms.
func TestConversationHistory_SendsOnlyValidProviderFormsAtRandom(t *testing.T) {
	// Seeded, so a failure replays.
	rnd := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // G404: a test simulation, not a secret.
	for range 500 {
		turns := make([]turn, 2+rnd.IntN(8))
		for i := range turns {
			turns[i] = turn{
				digest:     []string{"a", "b"}[rnd.IntN(2)],
				configured: rnd.IntN(2) == 0,
				pasted:     rnd.IntN(4) == 0,
				noDigests:  rnd.IntN(8) == 0,
				tools:      rnd.IntN(2) == 0,
			}
		}
		simulate(t, turns)
	}
}

// simulate plays the turns of a conversation, requiring that every run is
// sent only valid provider forms. It returns what the last run was sent, and
// each run's own messages as the model saw them.
func simulate(t *testing.T, turns []turn) ([]model.Message, [][]model.Message) {
	t.Helper()
	var (
		prior []priorRun
		// sent[i] is what run i was sent before its input, and seen[i] its
		// own messages as the model saw them.
		sent, seen [][]model.Message
		history    []model.Message
	)
	for i, tn := range turns {
		redact := redactorFor(t, tn.configured)
		history = conversationHistory(redact, prior, tn.digest)
		requireValid(t, history, sent, seen, i)

		input := fmt.Sprintf("input %d", i)
		if tn.pasted {
			input += " " + simSecret
		}
		own := []model.Message{{Role: model.RoleUser, Text: input}}
		if tn.tools {
			own = append(own,
				model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: fmt.Sprintf("c%d", i), Name: "files_read"}}, Provider: thinking(fmt.Sprintf("call %d", i))},
				model.Message{Role: model.RoleUser, ToolResults: []model.ToolResult{{CallID: fmt.Sprintf("c%d", i), Content: "- milk"}}},
			)
		}
		own = append(own, model.Message{Role: model.RoleAssistant, Text: fmt.Sprintf("reply %d", i), Provider: thinking(fmt.Sprintf("reply %d", i))})
		run := priorRun{digest: tn.digest, historyDigest: historyDigest(history)}
		if tn.noDigests {
			run = priorRun{}
		}
		for _, m := range own {
			stored, altered := redactMessage(redact, m)
			run.messages = append(run.messages, store.TranscriptMessage{Message: stored, Altered: altered})
		}
		prior = append(prior, run)
		sent, seen = append(sent, history), append(seen, own)
	}
	return history, seen
}

func thinking(about string) *model.ProviderPart {
	return &model.ProviderPart{Name: "sim", Data: json.RawMessage(fmt.Sprintf(`{"thinking":%q}`, about))}
}

func redactorFor(t *testing.T, configured bool) *secret.Redactor {
	t.Helper()
	var secrets []string
	if configured {
		secrets = []string{simSecret}
	}
	r, err := secret.NewRedactor(secrets)
	require.NoError(t, err)
	return r
}

// requireValid fails unless every provider form in history, the history of
// run n, is valid.
func requireValid(t *testing.T, history []model.Message, sent, seen [][]model.Message, n int) {
	t.Helper()
	from := 0
	for j := range n {
		before := history[:from]
		own := history[from : from+len(seen[j])]
		from += len(seen[j])
		if !hasForm(own) {
			continue
		}
		require.Equal(t, slices.Clip(append([]model.Message{}, sent[j]...)), slices.Clip(append([]model.Message{}, before...)), "run %d: what run %d was sent changed, but its provider form is kept", n, j)
		require.Equal(t, seen[j], own, "run %d: run %d's own messages changed, but its provider form is kept", n, j)
	}
	require.Len(t, history, from)
}

// keptRuns lists the runs whose replies history sends in their provider form.
func keptRuns(history []model.Message, seen [][]model.Message) []int {
	kept := []int{}
	from := 0
	for j, own := range seen {
		if hasForm(history[from : from+len(own)]) {
			kept = append(kept, j)
		}
		from += len(own)
	}
	return kept
}

func hasForm(messages []model.Message) bool {
	return slices.ContainsFunc(messages, func(m model.Message) bool { return m.Provider != nil })
}
