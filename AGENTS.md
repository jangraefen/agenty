# AGENTS.md

Guidance for AI coding agents working in this repository.

## Project

Agenty is a fully open-source, self-hostable enterprise platform for building governed AI agent harnesses. The repository currently contains the product and architecture documents only; no code has been written yet.

## Read before working

1. [docs/VISION.md](docs/VISION.md) — why Agenty exists, principles (ranked), core concepts, trust model, non-goals.
2. [docs/CAPABILITIES.md](docs/CAPABILITIES.md) — what ships in v1 versus Later / Explore.
3. [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — how it is built: decision record (§2), invariants (§3), layout, components, testing, dependencies, conventions (§17).

These documents are the source of truth for product, scope, and architecture. Do not reopen decisions recorded in ARCHITECTURE.md §2 without recording a new decision there.

## Non-negotiables

- **Scope**: build only v1 capabilities unless the task explicitly says otherwise.
- **Languages**: Go (`server/`, `sandbox/`) and TypeScript (`web/`). No Python in platform code; Python runs only inside sandboxes.
- **Invariants**: every change preserves ARCHITECTURE.md §3 — above all, every side effect goes through `toolgateway`, ungranted tools are denied, and credentials never reach model context, logs, or skill scripts.
- **Contracts first**: API and protocol changes start in `api/` (OpenAPI, protobuf); regenerate, never hand-edit generated code.
- **Tests first**: write the failing test before the implementation. Security-relevant changes extend the matching invariant suites. Coverage and mutation gates are defined in ARCHITECTURE.md §15.3.
- **Dependencies**: check every new dependency against the dependency rule and record it in ARCHITECTURE.md §16 before use.
- **No logic in the database**: no triggers or stored procedures.
- **Logging**: use the `log/slog` API (handler: `charmbracelet/log`).
- **Package manager**: the repository uses pnpm, never npm, for `web/` and any Node tooling (`pnpm install`, `pnpm run`, `pnpm exec`; no `npm` or `npx`).

## Commands

All entry points live in `Taskfile.yml`; run `task --list` to see them. Prerequisites are Go, [Task](https://taskfile.dev) (not Taskwarrior: on macOS `brew install go-task`), Lefthook, golangci-lint, go-licenses (`go install github.com/google/go-licenses/v2@v2.0.1`), Gremlins (`brew tap go-gremlins/tap && brew install gremlins`), Node.js, and pnpm (web tools come from `web/pnpm-lock.yaml`); `task setup` verifies their versions and installs the git hooks. Targets: `test:unit`, `test:module`, `test:integration`, `test:e2e`, `test:ui`, `test:mutation`, `test:gates`, `test:milestone:Mx`, `test:nightly` (not gating), `test:images` (container smoke test, not in `check`), `check:licenses`, `check:coverage`, and `check`. CI (GitHub Actions) runs the same targets on every pull request; run `task check` locally before opening one.

## Planning and tracking

Planning is split between the repository and GitHub (`jangraefen/agenty`). Each kind of information has exactly one home:

| What | Where | How to find or change it |
|---|---|---|
| Epic scope and acceptance-criteria definitions | `docs/epics/Exx-*.md` | Edit the file; mirror the change to the epic issue body in the same change |
| Milestone order, contents, and acceptance-criteria definitions | `docs/ROADMAP.md` | Edit the file; mirror the change to the GitHub milestone description in the same change |
| Epic status and acceptance-criteria progress | GitHub issue labeled `epic` (E01 = #1 … E21 = #21) | `gh issue view <n>`; check off criteria in the issue body only after their verification has passed |
| Stories | GitHub sub-issues of the epic issue, labeled `story`, same milestone as the epic | See below |
| Milestone status | GitHub milestones M1–M6 | `gh api repos/jangraefen/agenty/milestones` |

Never record status, progress, or stories in the repository files.

**Mirroring definitions to GitHub**
- Epic issue bodies and milestone descriptions are copies of the files plus progress checkboxes. Absolute links replace relative ones, and dependencies appear as `#n (Exx)`.
- When a definition changes, edit only the affected lines on GitHub (`gh issue view <n> --json body --jq .body`, change, `gh issue edit <n> --body-file -`). **Never regenerate a whole body from the file** once any checkbox is ticked: that would erase progress.

**Labels**: `epic`, `story`, `spike` (time-boxed, throwaway investigation; findings go to `docs/spikes/`, its code is never merged).

**Working with stories**
- Find work: `gh issue list --label story --milestone "M1 — Foundations"`; epics: `gh issue list --label epic`.
- Story format: title `<Exx>: <story title>`; body sections `Part of epic #n (Exx).`, `## Context`, `## Acceptance criteria` (checkboxes), `**Contributes to:** AC-Exx-n, …`, `## Verification`, `## Depends on` (`#n` or `None`). A story is sized for roughly one pull request.
- Create a story: `gh issue create --label story --milestone "<milestone title>" --title "<Exx>: <story title>" --body-file <file>`, then attach it to its epic: `gh api repos/jangraefen/agenty/issues/<epic number>/sub_issues -F sub_issue_id=$(gh api repos/jangraefen/agenty/issues/<story number> --jq .id)`.
- Every story references the acceptance criteria it contributes to. Close a story only when its tests pass under `task check`.
- Reference the story in commits and pull requests (e.g., `Closes #42`).

## Git

- **Never commit directly to `main`.** Work on a branch named `<type>/<issue>-<slug>` (e.g., `feat/42-entrypoint-api`) and open a pull request.
- One story per pull request where practical. The PR title uses conventional-commit format (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`), because pull requests are squash-merged and the title becomes the commit message.
- The PR description references the story (`Closes #42`), lists the acceptance criteria it addresses, and states how they were verified.
- Merge only when all required CI checks pass. Do not merge, push, or rewrite history unless asked.
- Do not repeat `Co-Authored-By` trailers in PR descriptions: GitHub carries them over from the branch commits into the squash commit.
- Commit author email is the GitHub no-reply address configured in the repository; do not change git identity settings.
- The repository is public but not yet licensed (see docs/VISION.md §13); do not add a license or accept external contributions without the maintainer's decision.
- Never put secrets in the repository, workflow files, or logs; CI secrets are only used by scheduled workflows.

## Keeping documents current

When a task changes a decision, a capability's phase, or an invariant, update VISION.md, CAPABILITIES.md, or ARCHITECTURE.md in the same change.
