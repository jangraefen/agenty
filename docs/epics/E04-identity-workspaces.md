# E04 — Identity & workspaces

> **GitHub issue**: [#4](https://github.com/jangraefen/agenty/issues/4) — status, progress, and stories are tracked there
> **Milestone**: [M2](../ROADMAP.md#m2--first-governed-run)
> **Depends on**: [E02](E02-server-core.md), [E03](E03-web-foundation.md)

## Goal

People and automations can authenticate, and workspaces with members, roles, and service accounts exist.

## Capabilities covered

- OIDC and SAML single sign-on
- Workspaces
- Workspace service accounts
- RBAC
- Harness audience (model and check)
- Service account credentials (inbound: Agenty-issued tokens, client credentials)

## In scope

- OIDC (authorization code + PKCE) and SAML 2.0 login; server-side sessions with HttpOnly Secure cookies; configurable allowed origins for cross-origin UI hosting.
- Principals (`user`, `service_account`); platform roles (platform admin, compliance, FinOps, enablement, workspace admin, workspace member).
- Workspaces and memberships; workspace administration.
- Personal access tokens; service accounts with Agenty-issued tokens and an OAuth client-credentials endpoint; tokens scoped, expiring, stored as hashes.
- Group claims captured from OIDC/SAML for harness audiences; audience check as a reusable function.
- Signals for departure detection (login failure) consumed by E17.
- UI: login, workspace management, members, service accounts, personal tokens.

## Out of scope

- Downstream credentials (E10).
- SCIM (Later).

## References

- ARCHITECTURE D13, D16, §8.1, §8.2; VISION §6

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- **AC-E04-1** — OIDC login against Dex completes; the session cookie is HttpOnly, Secure, and SameSite; no tokens are stored in browser storage. *Verified by:* Integration and Playwright tests.
- **AC-E04-2** — SAML 2.0 login against a test identity provider completes. *Verified by:* Integration test.
- **AC-E04-3** — Logout invalidates the session on the server. *Verified by:* Integration test.
- **AC-E04-4** — Credentialed requests from configured origins succeed; requests from other origins are rejected. *Verified by:* Module test.
- **AC-E04-5** — Every API route requires authentication except an explicit allowlist of public routes. *Verified by:* Invariant 5 suite (enumerates all OpenAPI routes).
- **AC-E04-6** — Each platform role's permissions are enforced on every endpoint. *Verified by:* Table-driven tests per endpoint and role.
- **AC-E04-7** — Workspace admins create workspaces and manage members; non-members cannot see workspace resources. *Verified by:* Module and invariant 13 tests.
- **AC-E04-8** — Personal access tokens can be created with scopes and expiry, revoked, and are stored only as hashes; expired or revoked tokens are rejected. *Verified by:* Module tests including database inspection.
- **AC-E04-9** — Service accounts can be created in a workspace with Agenty-issued tokens and client credentials; the client-credentials endpoint issues short-lived tokens; every workspace member can use them, non-members cannot. *Verified by:* Module tests.
- **AC-E04-10** — Group claims from OIDC and SAML are stored; the audience check returns correct results for workspace, group, and all-authenticated audiences. *Verified by:* Unit tests.
- **AC-E04-11** — UI flows for login, workspace management, members, service accounts, and personal tokens work and pass accessibility checks. *Verified by:* Playwright with axe-core.

## Stories

Stories are tracked as sub-issues of [#4](https://github.com/jangraefen/agenty/issues/4).
