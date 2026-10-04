# E09 — Approvals

> **GitHub issue**: [#9](https://github.com/jangraefen/agenty/issues/9) — status, progress, and stories are tracked there
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

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- **AC-E09-1** — A `require_approval` decision creates an approval request with tool, arguments, reasons, and a run link; the run enters `waiting_approval` and holds no worker lease. *Verified by:* Module tests.
- **AC-E09-2** — In a personal context only the run's user can decide; in a workspace context any workspace member can; everyone else receives 403. *Verified by:* Module tests.
- **AC-E09-3** — Approval executes the tool and continues the run; rejection returns the comment to the model as a tool error. *Verified by:* Integration tests.
- **AC-E09-4** — A decision made after a server restart still resumes the waiting run. *Verified by:* Crash test.
- **AC-E09-5** — Each decision is recorded with the approver's identity; a second decision on the same request is rejected. *Verified by:* Module tests.
- **AC-E09-6** — The inbox lists pending approvals with context and lets users decide; an in-app indicator shows pending approvals. *Verified by:* Playwright with axe-core.

## Stories

Stories are tracked as sub-issues of [#9](https://github.com/jangraefen/agenty/issues/9).
