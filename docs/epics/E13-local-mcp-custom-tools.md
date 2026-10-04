# E13 — Local MCP & custom tools

> **Status**: Proposed
> **Milestone**: [M4](../ROADMAP.md#m4--sandbox-skills--schedules)
> **Depends on**: [E11](E11-catalog-remote-mcp.md), [E12](E12-sandbox-runner.md)

## Goal

Local MCP servers and image-based custom tools run governed inside the sandbox.

## Capabilities covered

- MCP client: local servers
- Sandboxed custom tools (Python, Node.js)
- Scoped tool publishing

## In scope

- Catalog entries for local MCP servers: image digest, credential-to-environment mapping, egress allowlist.
- Sessions per run and identity with lifecycle management.
- Custom tools as images with typed schemas; executors registered with the gateway.
- Publishing review when moving from workspace to enterprise scope.

## Out of scope

- Instance reuse across runs (Later).

## References

- ARCHITECTURE D7, §9, §10; invariant 14

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- [ ] **AC-E13-1** — Local MCP catalog entries require an image digest, a credential-to-environment mapping, and an egress allowlist. *Verified by:* Module tests.
- [ ] **AC-E13-2** — Each run and identity gets its own session; different users never share an instance; sessions end with their run. *Verified by:* Integration tests.
- [ ] **AC-E13-3** — Sessions receive the acting user's delegated credential in personal contexts and the service account's credential in workspace contexts. *Verified by:* Integration tests.
- [ ] **AC-E13-4** — Custom tools run as images with typed schemas and return validated results. *Verified by:* Integration tests.
- [ ] **AC-E13-5** — Publishing local MCP entries or custom tools to enterprise scope requires review. *Verified by:* Module tests.
- [ ] **AC-E13-6** — A sample local MCP server and a sample custom tool work end to end through the gateway. *Verified by:* E2E test and invariant 14 suite.

## Stories

_To be defined._
