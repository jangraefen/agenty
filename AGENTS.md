# AGENTS.md

Guidance for AI coding agents working in this repository.

## Project

Agenty is an open-source, self-hostable platform for building governed AI agent harnesses. It is at the proof-of-concept stage: [docs/IDEA.md](docs/IDEA.md) describes the idea, the settled technology choices, and the PoC scope. Read it before working.

## Non-negotiables

- **Scope**: build the PoC described in IDEA.md. Add a component only when the PoC needs it.
- **Language**: Go. No agent framework: Agenty owns its agent loop on the official model provider SDKs.
- **Policy**: OPA, embedded, is the only policy engine.
- **Trust model**: every change preserves the guarantees in IDEA.md, above all: every side effect goes through the Tool Gateway, ungranted tools are denied, and credentials never reach model context or logs.
- **Tests first**: write the failing test before the implementation. Each trust-model guarantee has a named test; extend it when touching security-relevant code. The gateway and policy code get the most thorough tests.
- **Go tests**: use testify; `require` for preconditions where the test cannot continue, `assert` for independent checks; prefer table-driven tests. Use the scripted model, never a real one, in gating tests.
- **Lint**: `golangci-lint run ./...` must pass. Its `forbidigo` rules encode trust-model guarantees; never exclude or `//nolint` them. Every `//nolint` names its linter and says why.
- **Logging**: use the `log/slog` API (handler: `charmbracelet/log`).
- **Dependencies**: keep them few; every new dependency must be open source and free to self-host.

## Git

- Never commit directly to `main`. Work on a branch named `<type>/<slug>` and open a pull request.
- PR titles use conventional-commit format (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`); pull requests are squash-merged.
- Do not merge, push, or rewrite history unless asked.
- The repository is public but not yet licensed; do not add a license or accept external contributions without the maintainer's decision.
- Never put secrets in the repository or logs.

## Keeping documents current

When a task changes a technology choice, a trust-model guarantee, or the PoC scope, update IDEA.md in the same change.
