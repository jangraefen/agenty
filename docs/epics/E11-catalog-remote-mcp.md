# E11 — Catalog & remote MCP

> **Status**: Proposed
> **Milestone**: [M3](../ROADMAP.md#m3--connected-agents)
> **Depends on**: [E08](E08-toolgateway-policy.md), [E10](E10-connections-credentials.md)

## Goal

The enablement team publishes governed building blocks, and agents use remote MCP and OpenAPI tools.

## Capabilities covered

- Catalog with visibility scopes
- Review and publishing flow
- MCP client: remote servers
- OpenAPI 3.x import
- Per-harness model configuration (catalog)

## In scope

- Catalog items, versions, scopes (workspace, enterprise), and reviews.
- Tool onboarding with metadata (effect, trust, idempotency).
- Remote MCP client over streamable HTTP, including MCP authorization through E10.
- OpenAPI 3.x import into typed tools with an executor.
- Model configurations become catalog items.
- Catalog UI: browse, publish, review.

## Out of scope

- Local MCP and custom tools (E13).
- Starter kit curation (E21).

## References

- ARCHITECTURE D6, §10

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Integration tests with mock remote MCP servers using OAuth and static credentials.

## Stories

_To be defined._
