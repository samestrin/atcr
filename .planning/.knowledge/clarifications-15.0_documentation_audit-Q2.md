---
id: mem-2026-07-01-60b99d
question: "diff_smell test_only=hard flag on the architecture-doc drift guard (cmd/atcr/docs_audit_test.go:312) — does the committed test-only change resolve it?"
created: 2026-07-01
last_retrieved: ""
sprints: []
files: [cmd/atcr/docs_audit_test.go, reconcile/dedupe.go, docs/architecture.md, internal/reconcile/astgrouping.go]
tags: [clarifications, epic-15.0_documentation_audit, testing, resolve-td, diff_smell]
retrievals: 0
status: active
type: clarifications
---

# diff_smell test_only=hard flag on the architecture-doc drift

## Decision

Yes — the committed change fully resolves the item. This is a legitimate test-only drift guard by design (AC3's deliverable is itself a Go test cross-checking docs/architecture.md against live code), and it now asserts against `reconcile.MergeThreshold`/`GrayLow` pulled live via reflection into the constants (not hardcoded literals), so no corresponding non-test source change was ever required.

General pattern (same as clarifications-15.0_documentation_audit-Q1): a doc-drift guard test that formats its expected values LIVE from the actual exported constants (e.g. `strconv.FormatFloat(reclib.MergeThreshold, ...)`) rather than hardcoding literal strings is idiomatically stronger than a normal assertion — a future constant change breaks the test until the doc catches up, which is the intended regression-guard behavior, not a smell. When `diff_smell` flags such a test as `test_only=hard`, verify the asserted values are sourced from the real production constants/env-vars (grep their definition) rather than duplicated literals before concluding the guard is safe.

Justification:
- reconcile/dedupe.go:17-18 defines `MergeThreshold = 0.7` and `GrayLow = 0.4` as real exported constants; the test imports them as `reclib "github.com/samestrin/atcr/reconcile"` (cmd/atcr/docs_audit_test.go:24) and formats them live via `strconv.FormatFloat(reclib.MergeThreshold, ...)` / `strconv.FormatFloat(reclib.GrayLow, ...)` (cmd/atcr/docs_audit_test.go:516-517).
- `ATCR_DISABLE_AST_GROUPING` is a real production env var referenced in cmd/atcr/reconcile.go:34, internal/reconcile/gate.go:223, and defined as `astGroupingDisabledEnv` in internal/reconcile/astgrouping.go:27; the test asserts the doc names it verbatim (cmd/atcr/docs_audit_test.go:518).
- docs/architecture.md:73 and :77-78 state the `ATCR_DISABLE_AST_GROUPING` env var and the 0.7/0.4 thresholds in prose, matching what the test checks.
- `go test ./cmd/atcr/... -run TestArchitectureDocDescribesReconciler -v` passes; full `go test ./cmd/atcr/...` package is green.
- Epic 15.0's recorded Clarifications explicitly design T1/T4 as "Go tests that parse the docs and compare against the compiled binary / config schema" — a test-only deliverable by design.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/docs_audit_test.go
- reconcile/dedupe.go
- docs/architecture.md
- internal/reconcile/astgrouping.go
