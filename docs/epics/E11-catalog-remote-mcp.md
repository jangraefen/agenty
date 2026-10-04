# E11 — Catalog & remote MCP

> **GitHub issue**: [#11](https://github.com/jangraefen/agenty/issues/11) — status, progress, and stories are tracked there
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

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- **AC-E11-1** — Catalog items have versions and a scope (workspace or enterprise); visibility follows the scope. *Verified by:* Module tests.
- **AC-E11-2** — Publishing from workspace to enterprise scope requires review by the enablement team; reviews can approve or reject. *Verified by:* Module tests.
- **AC-E11-3** — Tools listed by an MCP server can be referenced by harnesses only after their metadata (effect, trust, idempotency) is set. *Verified by:* Module tests.
- **AC-E11-4** — Remote MCP calls use per-user OAuth tokens for on-behalf-of execution and static tokens for service accounts; servers with static credentials cannot be used on behalf of users. *Verified by:* Integration tests with mock MCP servers.
- **AC-E11-5** — Imported OpenAPI operations become typed tools; arguments are validated and calls use broker credentials. *Verified by:* Integration tests.
- **AC-E11-6** — Model configurations are catalog items referenced by harnesses; configurations created in E07 are migrated. *Verified by:* Module tests.
- **AC-E11-7** — A harness calls a mock remote MCP tool through the gateway with policy applied. *Verified by:* Integration test.
- **AC-E11-8** — The catalog UI supports browsing, publishing, and reviewing. *Verified by:* Playwright with axe-core.

## Stories

Stories are tracked as sub-issues of [#11](https://github.com/jangraefen/agenty/issues/11).
