# E15 — Knowledge

> **Status**: Proposed
> **Milestone**: [M5](../ROADMAP.md#m5--copilots--knowledge)
> **Depends on**: [E10](E10-connections-credentials.md), [E11](E11-catalog-remote-mcp.md), [E12](E12-sandbox-runner.md)

## Goal

Agents retrieve from uploaded documents and S3 with citations.

## Capabilities covered

- Knowledge connectors (file upload, S3-compatible storage)
- Permission-aware retrieval (audience-based for uploads and S3)
- Citations

## In scope

- Knowledge sources for uploads and S3 (credentials via E10).
- Document parsing as one-shot sandbox jobs; chunking; embeddings via model configurations.
- pgvector hybrid search with Postgres full-text and reciprocal rank fusion.
- Access based on the knowledge source's audience.
- `search_knowledge` built-in tool; retrieved passages taint the run.
- Citations in results and UI; knowledge source management UI.

## Out of scope

- Live search for Confluence and SharePoint (Later).
- Indexed connectors with ACL sync (Later).

## References

- ARCHITECTURE D12, §11.2

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Retrieval quality tests on a fixed corpus; access tests for audience restrictions.

## Stories

_To be defined._
