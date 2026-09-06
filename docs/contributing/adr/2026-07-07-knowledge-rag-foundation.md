# ADR: Governance Knowledge RAG foundation

## Status

Accepted on 2026-07-07.

## Context

Governance Knowledge is a foundation for product and policy context in governance-chat, Context Packs, and IDE agent handoff. Internal testing found that search could surface unrelated project knowledge when documents and vector payloads were not workspace-scoped.

## Decision

Doc Registry remains the runtime owner of Knowledge ingestion, metadata, pgvector retrieval, and citations.

SpecGate will not adopt LightRAG or RAG-Anything as its runtime. Both are useful research references, but they add graph/multimodal orchestration before SpecGate has workspace isolation, trusted citations, and a retrieval baseline.

The implementation uses:

- Postgres metadata filters before vector ranking.
- pgvector for semantic retrieval.
- A small internal chunking interface for Markdown/text.
- Scoped vector search over chunks, then bounded section expansion for selected hits.
- A JSONL retrieval gold set before ranking changes. Rerank-model reordering IS a
  ranking change and is therefore out of scope; a follow-up may add it only
  after the gold set produces a measured baseline to compare against.
- A future spike for Docling or LangChain splitters only after scoped retrieval is correct.

## Consequences

- Knowledge search is workspace-scoped by default.
- Knowledge chunks are quoted data, never instructions.
- Delivery review does not perform broad Knowledge search.
- Search returns citations with document id, version, title, chunk index, and `specgate://knowledge/...` URI.
- If embeddings are not configured, search fails clearly instead of pretending keyword fallback is semantic retrieval.
- Ingest is queue-capable: create/upload returns `202` and runs extract → chunk →
  embed → index either inline (`QUEUE_DRIVER=sync`, the default — status walks the
  enum within the request) or on the shared queue worker (`QUEUE_DRIVER=redis`),
  with the same status contract (`uploaded … indexed | failed`) and a retry
  endpoint for `failed` documents. Queue-backed ingest was scoped as a follow-up
  in the initial design (to keep the first slice bounded) and later
  landed as part of the foundation. Rich-document parsing (Docling/PDF)
  remains the follow-up
  this gates, because long parses and large chunk counts do not fit inside one
  HTTP request.
- LLM-extracted knowledge graphs (GraphRAG/LightRAG-style) are out of scope
  indefinitely: SpecGate's graph is its own governed metadata
  graph (features, change requests, artifacts, `document_links` typed edges),
  walked deterministically. Revisit only if the retrieval gold set accumulates
  failing multi-hop rows at real corpus scale.
