---
id: mem-2026-06-28-b9b91a
question: "Does the internal/reconcile layer need its own golden-layer regression test for the PageRank asymmetric promotion path, or is lib-layer coverage sufficient?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [internal/reconcile/lib.go, internal/reconcile/golden_corpus_test.go, reconcile/pagerank_confidence_test.go]
tags: [clarifications, epic-13.3_signal_pagerank, testing, internal-reconcile, shim-architecture, coverage, golden-fixture]
retrievals: 0
status: active
type: clarifications resolve-td 2026-06-28
---

# Does the internal/reconcile layer need its own golden-layer 

## Decision

Lib-layer coverage is sufficient. internal/reconcile is a pure shim — it delegates everything to reclib.Reconcile() with no PageRank logic of its own (internal/reconcile/lib.go:138-178 only does struct conversion, SkippedSources bookkeeping, and AmbiguousHash rebinding). TestGoldenCorpus_ByteIdentical at the internal layer already guards the symmetric/backward-compat path. The lib-layer tests (reconcile/pagerank_confidence_test.go) comprehensively cover asymmetric promotion, symmetric non-promotion, cycle/disconnected invariants, never-agreed model, and full-pipeline determinism — all where the logic actually lives. Adding a hypothetical asymmetric golden fixture at the internal layer creates maintenance burden for a case that doesn't exist yet, with no safety benefit over the lib-layer tests.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/lib.go
- internal/reconcile/golden_corpus_test.go
- reconcile/pagerank_confidence_test.go
