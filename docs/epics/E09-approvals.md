# E09 — Approvals

> **Status**: Proposed
> **Milestone**: [M3](../ROADMAP.md#m3--connected-agents)
> **Depends on**: [E08](E08-toolgateway-policy.md)

## Goal

People approve gated actions while runs pause and resume durably.

## Capabilities covered

- Approvals inbox (web)
- Centrally mandated approval gates (user experience)

## In scope

- Approval requests created by the gateway; routing for personal and workspace contexts.
- Inbox in API and UI showing tool, arguments, policy reasons, and a link to the run trace.
- Approve, or reject with a comment returned to the model as a tool error.
- In-app notifications for pending approvals.

## Out of scope

- Slack and Teams approvals, approval evidence, designated approvers, four-eyes, SLAs (Later).

## References

- ARCHITECTURE D15, §7.6

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- End-to-end approval flow including a server restart while the run waits.

## Stories

_To be defined._
