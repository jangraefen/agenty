# E10 — Connections & credentials

> **Status**: Proposed
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

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Invariant 6 suite: credentials never appear in model context, prompts, logs, traces, or skill scripts.
- 100 % branch coverage for `credentials`.

## Stories

_To be defined._
