# E12 — Sandbox runner

> **GitHub issue**: [#12](https://github.com/jangraefen/agenty/issues/12) — status, progress, and stories are tracked there
> **Milestone**: [M4](../ROADMAP.md#m4--sandbox-skills--schedules)
> **Depends on**: [E01](E01-project-foundation.md)

## Goal

Untrusted code runs isolated on any container host.

## Capabilities covered

- Sandbox runtime for skill scripts, custom tools, local MCP servers, and document parsing

## In scope

- `agenty-sandbox` binary with Connect protocol (definitions in `api/`) and mTLS.
- Docker/Podman backend with gVisor `runsc`; explicit `insecure_dev_mode` with hardened `runc`.
- One-shot mode and session mode with stdio relay.
- Egress proxy with per-sandbox hostname allowlists and connection logs.
- Resource limits and read-only root filesystem.
- Maintained runtime image (Bash, Python, Node.js, curated packages); registry allowlist and digest pinning.
- `sandboxclient` package in the server.
- Verify and record protobuf/Connect code-generation tooling (§16).

## Out of scope

- Kubernetes backend, warm pools (Later).

## References

- ARCHITECTURE D11, §9; invariant 14

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- **AC-E12-1** — The runner accepts only mTLS clients presenting a valid client certificate. *Verified by:* Module tests.
- **AC-E12-2** — One-shot executions of Bash, Python, and Node.js scripts return stdout, stderr, exit code, and output files within size limits; no container remains afterwards. *Verified by:* Module tests.
- **AC-E12-3** — Sessions relay stdio in both directions and are destroyed at run end or idle timeout. *Verified by:* Module tests.
- **AC-E12-4** — With `runsc` required but missing, startup fails; `insecure_dev_mode` starts with a prominent warning. *Verified by:* Module tests.
- **AC-E12-5** — Sandboxes have no network by default; allowlisted hosts are reachable through the egress proxy, all others are blocked, and connections are logged. *Verified by:* Invariant 14 suite.
- **AC-E12-6** — Exceeding CPU, memory, process, time, or tmpfs limits terminates the execution with a clear error; the root filesystem is read-only. *Verified by:* Module tests.
- **AC-E12-7** — Images from non-allowlisted registries or without a digest are rejected. *Verified by:* Module tests.
- **AC-E12-8** — Sandboxed code cannot reach the container runtime socket, the host filesystem, or cloud metadata endpoints. *Verified by:* Invariant 14 suite.
- **AC-E12-9** — The maintained runtime image builds with Bash, Python, Node.js, and the curated packages, versioned by digest. *Verified by:* `task build:images`.
- **AC-E12-10** — The module tests pass inside a Colima or Lima VM on macOS. *Verified by:* Running `task test:module` in the VM.
- **AC-E12-11** — CI runs the sandbox module tests with real `runsc` on Linux runners, not `insecure_dev_mode`. *Verified by:* CI job log showing the `runsc` runtime.

## Stories

Stories are tracked as sub-issues of [#12](https://github.com/jangraefen/agenty/issues/12).
