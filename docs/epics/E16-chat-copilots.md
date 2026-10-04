# E16 — Chat copilots

> **Status**: Proposed
> **Depends on**: [E09](E09-approvals.md)

## Goal

Users talk to purpose-built copilots in the web portal.

## Capabilities covered

- Copilots in a web portal
- Conversation memory
- Interactive autonomy mode

## In scope

- Chat entry point; `waiting_input` state; one run per conversation.
- Streaming responses via server-sent events; chat UI.
- Interactive mode: confirmations shown inline to the user.
- Conversation history as memory within the run.
- Audience-based access to copilots.

## Out of scope

- Slack and Teams, embeddable widget, long-term memory (Later).

## References

- ARCHITECTURE §6.1, §13.1

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- UI tests of a full conversation including an inline confirmation and a server restart mid-conversation.

## Stories

_To be defined._
