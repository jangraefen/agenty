# E12 — Sandbox runner

> **Status**: Proposed
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

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Isolation tests: no network by default, allowlist enforced, limits enforced, no host access.
- Runs in a Linux VM on macOS.

## Stories

_To be defined._
