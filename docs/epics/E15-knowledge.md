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

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- [ ] **AC-E15-1** — Upload and S3 sources can be added (S3 credentials via the broker); ingestion jobs are durable and retried on failure. *Verified by:* Module tests.
- [ ] **AC-E15-2** — PDF, DOCX, HTML, and Markdown are parsed in one-shot sandboxes; parser failures are reported per document. *Verified by:* Integration tests.
- [ ] **AC-E15-3** — Hybrid search fuses vector and full-text results; on a fixed labeled corpus, recall@5 is at least 0.8. *Verified by:* Retrieval quality test.
- [ ] **AC-E15-4** — Search returns only documents from sources whose audience includes the acting identity. *Verified by:* Module tests.
- [ ] **AC-E15-5** — `search_knowledge` results taint the run with the knowledge source. *Verified by:* Module tests.
- [ ] **AC-E15-6** — Results include source and location for citations, and the UI displays them. *Verified by:* Integration tests and Playwright.
- [ ] **AC-E15-7** — Knowledge sources can be managed in the UI. *Verified by:* Playwright with axe-core.

## Stories

_To be defined._
