# E07 — Agent loop & models

> **GitHub issue**: [#7](https://github.com/jangraefen/agenty/issues/7) — status, progress, and stories are tracked there
> **Milestone**: [M2](../ROADMAP.md#m2--first-governed-run)
> **Depends on**: [E06](E06-run-engine.md)

## Goal

Runs execute a real, model-driven agent loop with structured results.

## Capabilities covered

- Major commercial providers
- OpenAI-compatible endpoints
- Per-harness model configuration
- Output contracts (enforcement)
- Typed parameters and prompt templates (rendering)

## In scope

- `Model` interface; providers for OpenAI and OpenAI-compatible endpoints, Anthropic, Google, AWS Bedrock via official SDKs.
- Model configurations (admin-managed until the catalog in E11 takes them over).
- Scripted model as Go implementation and as fake OpenAI-compatible HTTP server.
- Context building: preamble, instructions, entry point templates with parameters in untrusted data blocks.
- `final_answer` tool with schema validation and bounded corrective retries.
- Step and token limits.
- Tool calls routed to a `ToolGateway` interface (deny-all stub until E08).

## Out of scope

- Tool execution and policy (E08).
- Budgets and cost (E19).

## References

- ARCHITECTURE D2, §7.1, §7.2; invariant 7

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- **AC-E07-1** — Providers for OpenAI and OpenAI-compatible endpoints, Anthropic, Google, and AWS Bedrock pass one shared provider contract suite (streaming, tool calls, usage reporting). *Verified by:* Module tests against fakes or recorded fixtures.
- **AC-E07-2** — The scripted model exists as a Go implementation and as a fake OpenAI-compatible HTTP server, and drives the integration stack deterministically. *Verified by:* Integration tests.
- **AC-E07-3** — The system prompt contains the platform preamble, instructions, and skill index; parameters appear only inside data blocks and cannot escape them (e.g., a value containing a closing tag). *Verified by:* Invariant 7 suite.
- **AC-E07-4** — A valid `final_answer` completes the run with the structured result; an invalid one triggers corrective retries up to the limit, then fails the run with `output_invalid`. *Verified by:* Module tests.
- **AC-E07-5** — Exceeding the step or token limit fails the run with `step_limit`. *Verified by:* Module tests.
- **AC-E07-6** — Tool calls reach the `ToolGateway` interface; with the deny-all stub, the model receives a tool error and the run continues. *Verified by:* Module tests.
- **AC-E07-7** — Model configurations can be managed by administrators and referenced by harnesses; provider API keys are never logged or returned by the API. *Verified by:* Module tests and redaction tests.
- **AC-E07-8** — `task test:nightly` runs a scenario against a configured real model; it is not part of `task check` and runs in CI only on schedule, using a repository secret that pull-request workflows cannot access. *Verified by:* Manual run and the first scheduled CI run.

## Stories

Stories are tracked as sub-issues of [#7](https://github.com/jangraefen/agenty/issues/7).
