---
id: mem-2026-06-20-592612
question: "Should atcr's diff caching apply to the verification/skeptic stage as well as the review fan-out stage?"
created: 2026-06-20
last_retrieved: ""
sprints: []
files: [internal/verify/skeptic.go, internal/verify/pipeline.go, internal/verify/invoke.go]
tags: [clarifications, epic-5.2_diff_caching_incremental_reviews, scope, architecture, caching, verification]
retrievals: 0
status: active
type: clarifications
---

# Should atcr's diff caching apply to the verification/skeptic

## Decision

No — cache scope must be restricted to the review fan-out stage only. The verification/skeptic stage is structurally incompatible with content-hash caching for three reasons: (1) its prompt includes an intentionally non-deterministic sentinel (internal/verify/skeptic.go:24, rand.Uint32()) that makes hash-based keying unreliable by design; (2) it invokes live code reads via the tool loop (internal/verify/pipeline.go:571-589), making its full input space broader than the static diff; (3) when review findings are partially cached and partially fresh, the reconciled findings fed into verification have mixed provenance — a cached skeptic verdict computed against prior findings could be stale or contradictory with no safe composite key.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/skeptic.go
- internal/verify/pipeline.go
- internal/verify/invoke.go
