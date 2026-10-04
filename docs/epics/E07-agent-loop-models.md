# E07 — Agent loop & models

> **Status**: Proposed
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

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Provider tests against recorded fixtures or fake servers for each provider.
- Optional `task test:nightly` against a real model.
- Invariant 7 (parameters are untrusted data) suite.

## Stories

_To be defined._
