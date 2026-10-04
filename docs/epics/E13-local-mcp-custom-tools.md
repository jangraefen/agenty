# E13 — Local MCP & custom tools

> **Status**: Proposed
> **Depends on**: [E11](E11-catalog-remote-mcp.md), [E12](E12-sandbox-runner.md)

## Goal

Local MCP servers and image-based custom tools run governed inside the sandbox.

## Capabilities covered

- MCP client: local servers
- Sandboxed custom tools (Python, Node.js)
- Scoped tool publishing

## In scope

- Catalog entries for local MCP servers: image digest, credential-to-environment mapping, egress allowlist.
- Sessions per run and identity with lifecycle management.
- Custom tools as images with typed schemas; executors registered with the gateway.
- Publishing review when moving from workspace to enterprise scope.

## Out of scope

- Instance reuse across runs (Later).

## References

- ARCHITECTURE D7, §9, §10; invariant 14

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- End-to-end tests with a sample local MCP server image and a sample custom tool.

## Stories

_To be defined._
