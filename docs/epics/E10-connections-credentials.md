# E10 — Connections & credentials

> **GitHub issue**: [#10](https://github.com/jangraefen/agenty/issues/10) — status, progress, and stories are tracked there
> **Milestone**: [M3](../ROADMAP.md#m3--connected-agents)
> **Depends on**: [E04](E04-identity-workspaces.md)

## Goal

Agents call downstream systems with the correct identity's credentials, which are never exposed.

## Capabilities covered

- Credential brokering
- Service account credentials (downstream)
- Agent identities and on-behalf-of execution (credential side)

## In scope

- Connection entity; credential types: user delegation, OAuth client credentials, static token or username/password (service accounts only, can be disallowed).
- Envelope encryption with master key from configuration.
- "Connect X" OAuth flows (authorization code + PKCE), including MCP authorization discovery.
- Refresh, invalidation, and owner notification on failure.
- Call-time injection API (HTTP headers, sandbox environment) and redaction registry for logs.
- UI: personal connections, workspace service-account credentials.

## Out of scope

- KMS/Vault, token exchange (Later).

## References

- ARCHITECTURE §8.3; invariant 6

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- **AC-E10-1** — Connections can be created with an auth method; static credentials are accepted only for service accounts and can be disabled by policy. *Verified by:* Module tests.
- **AC-E10-2** — The "Connect" flow (authorization code + PKCE) completes against a mock OAuth server, including MCP authorization discovery. *Verified by:* Integration tests.
- **AC-E10-3** — Credentials are stored only as ciphertext; starting with a wrong master key fails with a clear error. *Verified by:* Module tests including database inspection.
- **AC-E10-4** — Expired access tokens are refreshed transparently; refresh failure marks the credential invalid, notifies its owner, and emits an event for dependent schedules. *Verified by:* Module tests.
- **AC-E10-5** — Credential injection happens only at call time and only through the gateway. *Verified by:* `depguard` rule and module tests.
- **AC-E10-6** — Seeded canary secrets never appear in model requests, logs, traces, step logs, or API responses during full scenario runs. *Verified by:* Invariant 6 suite.
- **AC-E10-7** — Users can connect and disconnect personal accounts; workspace members can manage service-account credentials. *Verified by:* Playwright with axe-core.
- **AC-E10-8** — `credentials` has 100 % statement coverage and meets the mutation threshold. *Verified by:* `task check`.

## Stories

Stories are tracked as sub-issues of [#10](https://github.com/jangraefen/agenty/issues/10).
