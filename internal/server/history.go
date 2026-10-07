package server

import (
	"reflect"

	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/store"
)

// priorRun is an earlier run of the conversation a run continues.
type priorRun struct {
	// digest is the run's prompt digest; see agent.Agent.PromptDigest.
	digest   string
	messages []store.TranscriptMessage
}

// conversationHistory returns the conversation of the prior runs as a run
// with the prompt digest sends it to the model: redacted again, with the
// secrets known now, and with the model's replies in their provider form
// only where that form is still valid.
//
// Some providers bind a reply's reasoning, such as Anthropic's thinking
// blocks, to everything sent before it: the model, the instructions, the
// tools and every earlier message. Such a reply is refused once any of these
// differs, and dropping its provider form leaves its text and tool calls. The
// form is kept for the latest prior runs that ran with the same digest and
// whose messages are as the model saw them, and dropped for all runs before
// those: providers accept reasoning left out of the start of a conversation,
// not out of its middle. If redacting again changes a message, every run
// since may have sent it unredacted, so no provider form is kept. Each rule
// gives the same history on every later follow-up with the same digest, so
// what was valid for one run stays valid for the next.
func conversationHistory(redact *secret.Redactor, prior []priorRun, digest string) []model.Message {
	var history []model.Message
	// from[i] is the index in history of prior run i's first message.
	from := make([]int, len(prior)+1)
	redactedAgain := false
	for i, p := range prior {
		from[i] = len(history)
		for _, m := range p.messages {
			msg := redactMessage(redact, m.Message)
			redactedAgain = redactedAgain || !reflect.DeepEqual(msg, m.Message)
			history = append(history, msg)
		}
	}
	from[len(prior)] = len(history)

	keepFrom := len(prior)
	for i := len(prior) - 1; i >= 0 && !redactedAgain && asSeen(prior[i], digest); i-- {
		keepFrom = i
	}
	for i := range history[:from[keepFrom]] {
		history[i].Provider = nil
	}
	return history
}

// asSeen reports whether the model saw the messages of p as stored, with the
// prompt digest.
func asSeen(p priorRun, digest string) bool {
	if p.digest == "" || p.digest != digest {
		return false
	}
	for _, m := range p.messages {
		if m.Altered {
			return false
		}
	}
	return true
}
