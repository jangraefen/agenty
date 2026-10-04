# E21 — Starter kit & release readiness

> **GitHub issue**: [#21](https://github.com/jangraefen/agenty/issues/21) — status, progress, and stories are tracked there
> **Milestone**: [M6](../ROADMAP.md#m6--governance--release)
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
- Release workflow on GitHub Actions: a version tag builds and publishes images to GitHub Container Registry and generates a changelog.
- License decision and `LICENSE` file before the first release (VISION §13).

## Out of scope

- Helm chart (Later).

## References

- VISION §9; ARCHITECTURE §14, §16

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- **AC-E21-1** — At least GitLab and ServiceNow MCP servers are verified against the dependency rule, recorded in ARCHITECTURE §16, and shipped as catalog entries with reference deployments; other candidates are documented with a decision. *Verified by:* Review of §16 and catalog entries.
- **AC-E21-2** — Both reference scenarios pass as end-to-end suites using starter-kit servers or equivalent mocks. *Verified by:* E2E suites.
- **AC-E21-3** — A network capture during a full end-to-end run shows outbound traffic only to configured endpoints. *Verified by:* Invariant 12 suite.
- **AC-E21-4** — Following the operator documentation on a clean VM installs a working production Compose deployment, including `runsc`, backup and restore, and an upgrade. *Verified by:* Manual verification checklist, recorded.
- **AC-E21-5** — Documentation covers installation, configuration, key management, backup and restore, upgrades, hardening, and the crypto-shredding limitation. *Verified by:* Documentation review.
- **AC-E21-6** — Pushing a version tag runs the release workflow, which publishes images to GitHub Container Registry and a changelog; `agenty --version` matches the tag. *Verified by:* The first release run.
- **AC-E21-7** — A license is chosen, a `LICENSE` file is present, and VISION §13 is updated before the first release. *Verified by:* Repository review.

## Notes

- If no suitable open-source MCP server exists for a reference system, record the gap and decide between writing one in a separate repository or adjusting the scenario.

## Stories

Stories are tracked as sub-issues of [#21](https://github.com/jangraefen/agenty/issues/21).
