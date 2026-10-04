# E04 — Identity & workspaces

> **Status**: Proposed
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

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Integration tests against Dex (OIDC) and a SAML test identity provider.
- Tests for invariant 5 (authenticated caller) at the API boundary.

## Stories

_To be defined._
