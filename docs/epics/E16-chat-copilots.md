# E16 — Chat copilots

> **Status**: Proposed
> **Milestone**: [M5](../ROADMAP.md#m5--copilots--knowledge)
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

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- [ ] **AC-E16-1** — A chat conversation is one run that alternates between `running` and `waiting_input`; each message resumes it. *Verified by:* Module tests.
- [ ] **AC-E16-2** — Responses stream to the UI via server-sent events. *Verified by:* Playwright.
- [ ] **AC-E16-3** — In interactive mode, write effects require an inline confirmation by the user, recorded as an approval. *Verified by:* Integration tests and Playwright.
- [ ] **AC-E16-4** — History is kept within a conversation and never shared across conversations. *Verified by:* Module tests.
- [ ] **AC-E16-5** — A conversation continues after a server restart. *Verified by:* Crash test.
- [ ] **AC-E16-6** — Only the harness audience can start conversations. *Verified by:* Module tests.
- [ ] **AC-E16-7** — A full conversation including a confirmation works in the UI and passes accessibility checks. *Verified by:* Playwright with axe-core.

## Stories

_To be defined._
