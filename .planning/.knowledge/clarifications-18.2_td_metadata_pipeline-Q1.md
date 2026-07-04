---
id: mem-2026-07-04-5a9642
question: "Test-only cross-package guard vs. shared leaf package for reviewFileName/reviewFile drift (internal/reconcile/justification.go:17)"
created: 2026-07-04
last_retrieved: ""
sprints: []
files: [internal/reconcile/justification.go, internal/reconcile/justification_test.go, internal/fanout/artifacts.go, internal/boundaries_test.go, .planning/technical-debt/README.md]
tags: [clarifications, epic-18.2_td_metadata_pipeline, implementation, package-boundaries, adversarial-gate]
retrievals: 0
status: active
type: clarifications
---

# Test-only cross-package guard vs. shared leaf package for re

## Decision

The test-only guard is acceptable as implemented — it is one of the two remedies the originating TD item itself explicitly prescribed ("Extract the shared literal to a tiny leaf package both import, OR add a cross-package test asserting ... drift fails CI"), and it satisfies the item's own verification bar (a fanout rename now breaks the test). The diff-smell adversarial gate's "test-only" flag is a generic heuristic that can't see the TD item text sanctioned this exact approach, so this is a defensible override rather than a genuine over-simplification. Extracting a shared leaf package remains a valid stronger alternative but is not required — and is architecturally non-trivial here, since internal/boundaries_test.go's allowedInternalImports map does not currently permit internal/reconcile and internal/fanout to import each other or a new shared package without also registering it there.

Evidence:
- .planning/technical-debt/README.md:69 — TD item text names both remedies as equally acceptable.
- internal/reconcile/justification_test.go:279-303 — TestReviewFileName_MatchesFanout reads both source files, extracts the reviewFileName/reviewFile string literals, and require.Equal's them; a rename of either constant (internal/reconcile/justification.go:17 or internal/fanout/artifacts.go:22) fails this test.
- internal/boundaries_test.go — allowedInternalImports excludes fanout from reconcile's allowed imports and vice versa; sharing the literal via a new leaf package (precedent: stream, atomicfs, log, metrics, version) requires adding a new package and registering it in this map for both consumers — a bigger change than "tiny leaf package" framing suggests.
- .planning/epics/completed/18.2_td_metadata_pipeline.md — no Clarifications section or Out-of-Scope language contradicts keeping the test-only fix.

Reusable takeaway: when a deterministic adversarial/diff-smell gate flags a test-only change as over-simplified, check whether the originating TD item's own Fix text already named the test-only approach as an accepted remedy — if so, the gate's generic heuristic can be overridden rather than forcing a heavier structural refactor.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/justification.go
- internal/reconcile/justification_test.go
- internal/fanout/artifacts.go
- internal/boundaries_test.go
- .planning/technical-debt/README.md
