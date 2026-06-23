# User Story 1: Preserve ATCR as the Reference Implementation with Zero Behavioral Change

**Plan:** [8.0: Reconciler Library Module Extraction](../plan.md)

## User Story

**As a** ATCR maintainer
**I want** ATCR to consume the extracted reconcile library through a boundary adapter that preserves path-validation fields, with every consumer package re-importing the library's public types
**So that** ATCR stays the reference implementation with zero behavioral change and byte-identical fixtures after the reconciler moves into its own module

## Story Context

- **Background:** Epic 8.0 extracts ATCR's deterministic reconciler out of `internal/reconcile` into a standalone, stdlib-only Go module at `github.com/samestrin/atcr/reconcile` (physical `./reconcile/`, consumed via a root `replace` directive). The central Phase-0 task is a public/private split: `emit.go` and `discover.go` each mix public types with ATCR-internal file I/O. This story is the ATCR-consumer side of that split. It enforces the lift-as-is mandate — `Reconcile(sources []Source, opts Options) Result` and `Options{ReconciledAt, Partial, Merges, Root}` are kept exactly (`internal/reconcile/reconcile.go:64`) — and rewires ATCR to call the library through a dedicated boundary adapter (`internal/reconcile/adapter/adapter.go`) that converts `stream.Finding` ↔ `reconcile.Finding` and retains the ATCR-internal path-validation fields (`PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged`). The public types and pure logic have already moved into the library by a companion story; this story makes ATCR import them and proves nothing changed.
- **Assumptions:**
  - The library module `./reconcile/` exists with its `go.mod` and the lifted public API surface (`Reconcile`, `Source`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, `Verdict` constants, library-owned `Finding`, and `NormalizeSeverity`/`SeverityRank`) already moved in.
  - The lift-as-is mandate holds: no API reshaping. The epic's proposed clean API (`(*Result, error)`, `ReconciledFinding`, `Options{LineTolerance, SimilarityThreshold}`) is deferred to a follow-on epic.
  - `Verification` becomes public library API unchanged (no copy), so `Merged.Verification` pointer-identity semantics are preserved for `gate.go` and `internal/debate`, which both read/mutate it.
  - Levenshtein stays in `internal/stream` (used only by `stream/suggest.go` path validation, not by dedupe). It is NOT moved in this story.
- **Constraints:**
  - Stdlib-only in non-test library files; testify confined to `*_test.go`.
  - The library is synchronous and stateless — no `context.Context` or goroutines cross the boundary.
  - Validation is mechanical-move-dominant: most of this story is verified by the existing test corpus + byte-identical fixtures, NOT new RED tests. New RED tests apply only to the boundary adapter conversion (`stream.Finding` ↔ `reconcile.Finding`).
  - Root `go test ./...` does NOT cross the nested module's `go.mod` boundary, so the library is tested via its own job; ATCR-side regression is proven by the root test run.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | Library module scaffold + moved public types/logic (companion story); the library's `Finding`/`Verification`/`Verdict` constants/severity helpers must exist before the boundary adapter and import-flip can proceed. Epics 13.0/13.2/13.3 (dedup/confidence replacement) must NOT start until this extraction lands on a stable surface. |

## Success Criteria (SMART Format)

- **Specific:** ATCR imports `github.com/samestrin/atcr/reconcile` via a root `replace` directive and routes all reconcile usage through the `internal/reconcile/adapter` boundary, which converts `stream.Finding` ↔ `reconcile.Finding` and stamps path-validation fields onto the ATCR-internal `JSONFinding`.
- **Measurable:** All nine consumer packages — `cmd/atcr`, `internal/debate`, `internal/verify`, `internal/report`, `internal/ghaction`, `internal/mcp`, `internal/fanout`, `internal/scorecard`, `internal/registry` — import the library's public types/severity helpers; `go test ./...` is green in the root module; every existing fixture (`findings.json`, `ambiguous.json`, `disagreements.json` sidecars) is byte-identical (zero diff) against the pre-extraction baseline; the full existing test corpus passes with no behavioral change.
- **Achievable:** `internal/reconcile` is already a black-box package (one public entry `Reconcile` + public types `Source`/`Merged`/`Result`/`Options`/`Summary` + private helpers, importing only `internal/stream`), so the move is mechanical and the import-flip is a compile-driven find-and-repoint, not greenfield design.
- **Relevant:** AC#3 is the central no-regression guarantee. Without byte-identical fixtures and a green corpus, the extraction cannot merge and ATCR loses its standing as the reference implementation.
- **Time-bound:** Lands within the sprint phase allocated to the boundary adapter + consumer import-flip, ahead of the 13.x dedup-replacement epics (the stable-surface dependency).

## Acceptance Criteria Overview

1. ATCR consumes `github.com/samestrin/atcr/reconcile` through the root `replace` directive, with the `internal/reconcile/adapter` boundary converting `stream.Finding` ↔ `reconcile.Finding` and retaining path-validation fields (`PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged`) in the ATCR-internal `JSONFinding`.
2. All consumer packages are flipped to import the library: `cmd/atcr` (github.go, reconcile.go, report.go, resume.go, review.go, verify.go), `internal/debate`, `internal/verify`, `internal/report`, `internal/ghaction`, `internal/mcp`, `internal/fanout` (metrics.go, postprocess.go), `internal/scorecard/reconcile.go`, and `internal/registry/config.go` (severity helpers) — no consumer re-declares verdict/severity constants locally.
3. The full existing ATCR test corpus passes with zero behavioral change and byte-identical fixtures (AC#3), verified by diff against the pre-extraction baseline.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/8.0_reconciler_library/`_

## Technical Considerations

- **Implementation Notes:** The boundary adapter lives in a new `internal/reconcile/adapter/adapter.go` package (Phase-0 resolved 2026-06-23). It isolates three responsibilities: (1) `stream.Finding` → `reconcile.Finding` conversion before calling `reconcile.Reconcile`, (2) wrapping the library `Result` back into the ATCR-internal `JSONFinding` (`emit.go:74`) with path-validation fields stamped, and (3) the file I/O that previously lived in `emit.go`/`discover.go`. The pure public types move out: `Verification` (`emit.go:40`), `VerdictConfirmed`/`Refuted`/`Unverifiable` (`emit.go:61-63`), `Source` (`discover.go:25`), and the library-owned `Finding` (9 wire fields + `Disagreement` + `*Verification`). `merge.go` — the heart of the reconciler — moves with `Merged`, `mergeVerification` (`merge.go:418`), `verdictRank`, and the finding-merge rules; its package-local `SeverityRank` copy (`merge.go:30`) collapses to the library's canonical `NormalizeSeverity`/`SeverityRank` (moved from `internal/stream/severity.go:33`). ATCR-internal pieces that stay behind the adapter: `gate.go` (`IsFailing`/`CountAtOrAbove`, `gate.go:96`), `validate.go` (`validateFindingPaths`, `validate.go:21`), and the path-validation fields — all now import the library's public `Verification` + `Verdict` constants unchanged (no visibility change, since the constants are already exported).
- **Integration Points:** CLI boundary sites at `cmd/atcr/reconcile.go:35` (`runReconcile`), `cmd/atcr/review.go:89` (`runReview`), and `cmd/atcr/resume.go:45` (`runResume`) construct `[]Source` and call `Reconcile` via the adapter. The MCP handler `internal/mcp/handlers.go:278` (`handleReconcile`) shares the same boundary adapter and gate semantics. Cross-examination mutates `Verification` via `internal/debate/emit.go:107` (`applyRulings`) and `internal/debate/debate.go:85` (`runDebate`); verify aggregates via `internal/verify/votes.go:25` (`aggregateVerdicts`). Severity-helper consumers that must re-import the library: `internal/fanout/metrics.go:107`, `internal/fanout/postprocess.go:19`, `internal/debate` (envelope.go, select.go), `internal/verify/severity.go`, `internal/report/render.go`, and `internal/registry/config.go` (the last two were added by the 2026-06-23 audit, beyond the original epic list). GitHub Action rendering at `internal/ghaction/render.go:60` (`isRefuted`/`Conclusion`) keys off the library `JSONFinding`/`IsFailing`.
- **Data Requirements:** The library defines ONE `Finding` carrying the 9 core wire-format fields (Severity, File, Line, Problem, Fix, Category, EstMinutes, Evidence, Reviewer/Reviewers, Confidence) + `Disagreement` + `*Verification`. ATCR's `JSONFinding` wraps it and adds `PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged` at the adapter boundary. No schema change to the on-disk `atcr-findings/v1` wire format — fixtures must round-trip byte-identically. The `sortMerged` total-order (severity desc, then file, then line) is preserved exactly so the same input yields byte-identical artifacts.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `emit.go`/`discover.go` type/I/O split is error-prone — public types are entangled with file I/O | High | Split mechanically: move `Verification`/`Finding`/`Source`/`Verdict` types first, compile-check in both packages, then relocate the I/O behind the `internal/reconcile/adapter` package. Source: codebase-discovery.json architecture_notes "CORE ENTANGLEMENT". |
| Extraction breaks ATCR behavior | High | Lift the API as-is (no reshape); keep the full test corpus green and fixtures byte-identical before merge. Diff every fixture against the pre-extraction baseline. Source: original-requirements.md risk table. |
| `Merged.Verification` mutability breaks across the boundary — `gate.go` and `internal/debate` both read/mutate it | Medium | `Verification` becomes public library API unchanged (no copy). Consumers operate on the same `*Verification` pointer, preserving pointer-identity semantics. Source: codebase-discovery.json integration_points "internal/reconcile/merge.go:Merged.Verification". |
| Severity-helper consumer scope is broader than the original epic list — `internal/registry/config.go` and `internal/scorecard/reconcile.go` were not originally listed | Medium | The 2026-06-23 audit is the source of truth for the consumer list, not the epic body. Treat the resolved list (9 packages, including scorecard + registry) as exhaustive; grep-verify no `internal/reconcile` or `internal/stream` severity references remain after the flip. |
| Root `go test ./...` does not cross the nested module boundary, so a library regression could hide | Medium | Run the library's own test job AND the root test job; both must be green. The corpus (e.g. `internal/reconcile/emit_test.go:20` `TestReconcile_TwoReviewersAgreeHighConfidence`) stays ATCR-internal where it exercises the adapter, proving end-to-end behavior. |

---

**Created:** June 23, 2026 11:48:36AM
**Status:** Draft - Awaiting Acceptance Criteria
