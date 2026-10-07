package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"slices"

	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/store"
)

// priorRun is an earlier run of the conversation a run continues.
type priorRun struct {
	// digest is the run's prompt digest; see agent.Agent.PromptDigest.
	digest string
	// historyDigest is the historyDigest of what the run was sent before
	// its input.
	historyDigest string
	messages      []store.TranscriptMessage
}

// conversationHistory returns the conversation of the prior runs as a run
// with the prompt digest sends it to the model: redacted again, with the
// secrets known now, and with the model's replies in their provider form
// only where that form is still valid.
//
// Some providers bind a reply's reasoning, such as Anthropic's thinking
// blocks, to everything sent before it: the model, the instructions, the
// tools and every earlier message. Such a reply is refused once any of these
// differs, and dropping its provider form leaves its text and tool calls.
// Providers accept reasoning left out of the start of a conversation, not
// out of its middle, so the form is kept for the latest runs only: each of
// them must have run with the same digest, been sent exactly what it is sent
// now before its input, and have its own messages sent as the model saw
// them, not altered when stored nor changed by redacting them again.
func conversationHistory(redact *secret.Redactor, prior []priorRun, digest string) []model.Message {
	var (
		history []model.Message
		// from[i] is the index in history of prior run i's first message.
		from = make([]int, len(prior)+1)
		// unchanged[i] says that prior run i ran with digest and that its
		// messages are sent as the model saw them.
		unchanged = make([]bool, len(prior))
	)
	for i, p := range prior {
		from[i] = len(history)
		unchanged[i] = p.digest != "" && p.digest == digest
		for _, m := range p.messages {
			msg, changed := redactMessage(redact, m.Message)
			unchanged[i] = unchanged[i] && !changed && !m.Altered
			history = append(history, msg)
		}
	}
	from[len(prior)] = len(history)

	// Each message's sum, as sent with its provider form and without.
	with, without := make([][sha256.Size]byte, len(history)), make([][sha256.Size]byte, len(history))
	for i, msg := range history {
		with[i] = messageSum(msg)
		msg.Provider = nil
		without[i] = messageSum(msg)
	}
	// Keep the forms of runs keep and later, if each of these runs was sent
	// what it would be sent so.
	sentAsBefore := func(keep int) bool {
		h := newHistoryHash()
		j := keep
		for i := range history {
			for ; j < len(prior) && from[j] == i; j++ {
				if h.digest() != prior[j].historyDigest {
					return false
				}
			}
			if i < from[keep] {
				h.add(without[i])
			} else {
				h.add(with[i])
			}
		}
		return true
	}
	// Keeping more forms may be valid where keeping fewer is not: a run sent
	// an earlier run's form must be sent it again. Keeping none always is.
	keepFrom := 0
	valid := func(keep int) bool {
		return !slices.Contains(unchanged[keep:], false) && sentAsBefore(keep)
	}
	for keepFrom < len(prior) && !valid(keepFrom) {
		keepFrom++
	}
	for i := range history[:from[keepFrom]] {
		history[i].Provider = nil
	}
	return history
}

// historyDigest identifies a history as it is sent to the model.
func historyDigest(history []model.Message) string {
	h := newHistoryHash()
	for _, msg := range history {
		h.add(messageSum(msg))
	}
	return h.digest()
}

// historyHash hashes a history from the sums of its messages, so the digest
// of each of its beginnings is known along the way.
type historyHash struct{ hash.Hash }

func newHistoryHash() historyHash {
	return historyHash{sha256.New()}
}

func (h historyHash) add(sum [sha256.Size]byte) {
	must.Value(h.Write(sum[:]))
}

func (h historyHash) digest() string {
	return hex.EncodeToString(h.Sum(nil))
}

// messageSum hashes what of msg is sent to the model. Everything read back
// from the store is valid UTF-8, which JSON keeps as is.
func messageSum(msg model.Message) [sha256.Size]byte {
	type call struct{ ID, Name, Args string }
	sent := struct {
		Role                   model.Role
		Text                   string
		ToolCalls              []call
		ToolResults            []model.ToolResult
		Provider, ProviderData string
	}{Role: msg.Role, Text: msg.Text, ToolResults: msg.ToolResults}
	for _, c := range msg.ToolCalls {
		sent.ToolCalls = append(sent.ToolCalls, call{c.ID, c.Name, string(c.Args)})
	}
	if p := msg.Provider; p != nil {
		sent.Provider, sent.ProviderData = p.Name, string(p.Data)
	}
	// Strings and structs of them always marshal.
	return sha256.Sum256(must.Value(json.Marshal(sent)))
}
