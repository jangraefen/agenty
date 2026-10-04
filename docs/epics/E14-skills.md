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

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Tests that skill scripts get no network or credentials unless granted.

## Stories

_To be defined._
