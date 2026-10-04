# E14 — Skills

> **Status**: Proposed
> **Milestone**: [M4](../ROADMAP.md#m4--sandbox-skills--schedules)
> **Depends on**: [E11](E11-catalog-remote-mcp.md), [E12](E12-sandbox-runner.md)

## Goal

Builders give agents expertise through skills with instructions, scripts, and resources.

## Capabilities covered

- Skill editor
- Skill scripts (Bash, Python, Node.js)
- Agent Skills format compatibility

## In scope

- Skill storage (`SKILL.md`, scripts, resources) in blob storage; catalog integration; review required for skills with scripts beyond workspace scope.
- `SKILL.md` import and export.
- Skill index in the system prompt; `load_skill` and `run_skill_script` built-in tools through the gateway to one-shot sandboxes.
- Skill editor UI.

## Out of scope

- None

## References

- VISION §6; ARCHITECTURE §7.2, §9

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- [ ] **AC-E14-1** — Skills consist of `SKILL.md`, scripts, and resources; import and export are compatible with the Agent Skills format. *Verified by:* Tests with fixtures from the public format examples.
- [ ] **AC-E14-2** — Skills with scripts require review beyond workspace scope; instruction-only skills do not. *Verified by:* Module tests.
- [ ] **AC-E14-3** — The system prompt contains only the skill index; `load_skill` returns the instructions; `run_skill_script` executes through the gateway in a one-shot sandbox with policy applied. *Verified by:* Integration tests.
- [ ] **AC-E14-4** — Skill scripts have no network access or credentials unless explicitly granted. *Verified by:* Invariant 14 suite.
- [ ] **AC-E14-5** — Skills with files can be created and edited in the UI. *Verified by:* Playwright with axe-core.

## Stories

_To be defined._
