# E21 — Starter kit & release readiness

> **Status**: Proposed
> **Depends on**: All other epics

## Goal

A fresh installation is useful on day one and can be operated with confidence.

## Capabilities covered

- Starter kit of curated connectors
- Self-hosted container deployment
- No phone-home

## In scope

- Select and verify open-source MCP servers (at least GitLab and ServiceNow for the reference scenarios; further candidates: GitHub, Jira, Microsoft 365) against the dependency rule; catalog entries and reference deployments.
- Both reference scenarios (VISION §9) as end-to-end suites.
- No-phone-home test via network capture in the integration stack.
- Production Compose example.
- Operator documentation: installation, `runsc` setup, configuration, backup and restore, upgrades, key management, the crypto-shredding limitation, hardening.
- Release process: versioning, images, changelog.

## Out of scope

- Helm chart (Later).

## References

- VISION §9; ARCHITECTURE §14, §16

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Both reference scenarios pass end to end; the no-phone-home test passes.

## Notes

- If no suitable open-source MCP server exists for a reference system, record the gap and decide between writing one in a separate repository or adjusting the scenario.

## Stories

_To be defined._
