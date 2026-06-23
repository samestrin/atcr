# User Story 2: Embeddable Public API Module Scaffold

**Plan:** [8.0: Reconciler Library Module Extraction](../plan.md)

## User Story

**As an** external tool author building a code-review or findings-aggregation product
**I want** to import ATCR's deterministic reconciler as a standalone Go module with a stable public API (`Reconcile(sources []Source, opts Options) Result`) without importing the ATCR binary or its file I/O
**So that** I can embed multi-reviewer merge into my own tool with a single `import` and a clear licensing path

## Story Context

- **Background:** The reconciler currently lives under `internal/reconcile`, entangled with ATCR's file I/O (`emit.go`, `discover.go`) and path-validation machinery (`gate.go`, `validate.go`). An external tool author cannot import it today without pulling in the whole ATCR binary. Epic 8.0 extracts the pure types + logic into a nested module at `./reconcile/` with its own `go.mod`, consumed via a root `replace` directive, lifting the existing public API as-is so the surface is stable and embeddable. The existing `internal/reconcile` package is already a black-box — one public entry (`Reconcile`) plus public types (`Source`/`Merged`/`Result`/`Options`/`Summary`) plus private helpers, importing only `internal/stream` — so the extraction is mechanical-move-dominant and is validated by the existing test corpus rather than new RED tests.
- **Assumptions:** Go 1.25 toolchain. The lift-as-is mandate keeps `Reconcile`/`Source`/`Merged`/`Options`/`Result`/`Summary` shapes identical to the existing internal package. The library is strictly stdlib-only in non-test files (`sort`, `strings`, `encoding/json`); testify is allowed only in `*_test.go`. Separate-repo publication follows extraction and is out of scope for this story. The epic's proposed clean API (`(*Result, error)`, `ReconciledFinding`, `Options{LineTolerance, SimilarityThreshold}`) is deferred to a follow-on epic.
- **Constraints:** Zero behavioral change to ATCR — existing tests and byte-identical fixtures must stay green (verified by story 3). The public/private boundary splits `emit.go`/`discover.go` types from their file I/O: public types + pure logic move to the library; `gate.go`, `validate.go`, file I/O, and path-validation fields (`PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged`) stay ATCR-internal behind the boundary adapter. `levenshtein` stays in `internal/stream` (used only by ATCR path validation, not by dedupe).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | None — foundational scaffold; the reconciler surface in `internal/reconcile` is stable per Epic 1.0 completion. Blocks stories 3 (reference-impl no-behavioral-change), 4 (JSON adapter), 5 (docs/example), 6 (licensing), 7 (module CI), 8 (leaderboard citation). |

## Success Criteria (SMART Format)

- **Specific:** A standalone Go module exists at `./reconcile/` with its own `go.mod` declaring `github.com/samestrin/atcr/reconcile`, wired into the root module via `replace github.com/samestrin/atcr/reconcile => ./reconcile`, exposing the lifted-as-is public API `Reconcile(sources []Source, opts Options) Result` that an external tool can import.
- **Measurable:** `go build ./reconcile/...` succeeds; `go test ./reconcile/...` passes; `gofmt -l ./reconcile` and `golangci-lint run ./reconcile/...` are clean; the exported symbols (`Reconcile`, `Source`, `Finding`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, `VerdictConfirmed`/`VerdictRefuted`/`VerdictUnverifiable`) appear in `go doc github.com/samestrin/atcr/reconcile`.
- **Achievable:** The existing `internal/reconcile` is already a self-contained black-box package (one public entry + types + private helpers, importing only `internal/stream`); extraction is mechanical-move-dominant and is validated by the existing test corpus.
- **Relevant:** Turns the core architectural moat into a separable, inspectable, embeddable asset; unblocks the licensing path (story 6), the JSON adapter (story 4), and the leaderboard reference-implementation citation (story 8).
- **Time-bound:** Delivered as the foundational story of epic 8.0; all downstream stories depend on this module + API landing first.

## Acceptance Criteria Overview

1. The nested module `github.com/samestrin/atcr/reconcile` exists at `./reconcile/` with its own `go.mod` (Go 1.25, no third-party requires) and the root `go.mod` wires it via `replace github.com/samestrin/atcr/reconcile => ./reconcile`; `go build` and `go test` for the module pass. (AC#1)
2. The public API exposes `Reconcile(sources []Source, opts Options) Result` with the lifted-as-is types (`Source`, `Finding`, `Merged`, `Options{ReconciledAt, Partial, Merges, Root}`, `Result`, `Summary`, `Verification` + `VerdictConfirmed`/`VerdictRefuted`/`VerdictUnverifiable` constants) as stable, exported library symbols; the proposed clean API is deferred. (AC#2)
3. The public/private boundary is enforced: library non-test files are stdlib-only (`sort`, `strings`, `encoding/json`); `emit.go`/`discover.go` file I/O, `gate.go`, `validate.go`, and path-validation fields stay ATCR-internal behind the `internal/reconcile/adapter` boundary package. (AC#1, AC#2)

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/8.0_reconciler_library/`_

## Technical Considerations

- **Implementation Notes:**
  - Create `./reconcile/go.mod` declaring `module github.com/samestrin/atcr/reconcile`, `go 1.25`, with no `require` block (stdlib-only). Add `replace github.com/samestrin/atcr/reconcile => ./reconcile` to the root `go.mod`.
  - Mechanically move the pure types + logic into `reconcile/`:
    - `reconcile.go` — `Reconcile` entry point (reconcile.go:64) + `Options`/`Result`/`Summary`; preserve `sortMerged` total order (severity desc, file, line) exactly for byte-identical output.
    - `merge.go` — `Merged` (embeds `Finding` + `Disagreement` + `*Verification`), `mergeVerification` (merge.go:418, verify-precedence confirmed>unverifiable>refuted, skeptic provenance unioned), `verdictRank`, and the finding-merge rules.
    - `dedupe.go` — `DedupeCluster` + `AmbiguousCluster` (dedupe.go:53); keep integer-cross-multiply thresholds (exact 0.7/0.4 boundaries), no float comparisons.
    - `disagree.go` — `BuildDisagreements` (disagree.go:102).
    - `confidence.go` — `ConfidenceForVerdict`/`ConfidenceAtOrAbove` (confidence.go:22), v2 confidence ordinal (VERIFIED/HIGH/MEDIUM/LOW).
    - `cluster.go` — `Cluster` (cluster.go:29), deterministic (FILE, LINE±3) single-linkage location clustering.
    - `ambiguous.go` — `AmbiguousID`/`AmbiguousHash` (ambiguous.go:60).
    - `attribution.go` — `EvidenceSep`/`FixAttribution` (attribution.go:10).
    - `severity.go` — `NormalizeSeverity`/`SeverityRank` (from internal/stream/severity.go:33); the library becomes the canonical owner, eliminating the redundant init-copy at merge.go:30.
  - Split `emit.go`: move `Verification` (emit.go:40), `VerdictConfirmed`/`VerdictRefuted`/`VerdictUnverifiable` (emit.go:61-63), and the library `Finding` to the library; keep the file-I/O emit layer ATCR-internal. The verdict constants are already exported, so no visibility change is required — `gate.go` simply imports them.
  - Split `discover.go`: move the `Source` type (discover.go:25) to the library; keep `Discover()` file reading ATCR-internal.
  - The library defines one `Finding` carrying the 9 wire-format fields plus `Disagreement` and `*Verification`; ATCR's `JSONFinding` wraps it and adds path-validation fields at the adapter boundary.
  - The library is synchronous and stateless: `Reconcile` takes no `context.Context` and spawns no goroutines; `context`/`sync` stay in ATCR's fan-out engine that orchestrates around calls to the library.

- **Integration Points:**
  - Root `go.mod` `replace` directive — development-only wiring; removed when the library is published to a separate repo (deferred).
  - ATCR boundary adapter `internal/reconcile/adapter/adapter.go` — converts `stream.Finding` ↔ `reconcile.Finding` and retains path-validation fields; lives beside the ATCR-internal `gate.go`/`validate.go` that stay behind.
  - `gate.go` (ATCR) imports the library's now-public `Verification` + `Verdict` constants unchanged (`IsFailing`/`CountAtOrAbove`, gate.go:96).
  - Consumer import-flip (established here as the importable surface; full no-behavioral-change verification is story 3): `cmd/atcr`, `internal/debate`, `internal/verify`, `internal/report`, `internal/ghaction`, `internal/mcp`, `internal/fanout`, `internal/scorecard`, `internal/registry` re-import `github.com/samestrin/atcr/reconcile`.

- **Data Requirements:**
  - `reconcile/go.mod`: `module github.com/samestrin/atcr/reconcile`, `go 1.25`, no `require` block.
  - Root `go.mod`: `replace github.com/samestrin/atcr/reconcile => ./reconcile`.
  - Lifted-as-is public surface:
    - `func Reconcile(sources []Source, opts Options) Result`
    - `type Options struct { ReconciledAt time.Time; Partial bool; Merges map[string]int; Root string }`
    - `type Source struct { Name string; Findings []Finding }`
    - `type Finding` — the 9 wire-format fields plus `Disagreement` and `*Verification`
    - `type Merged` — embeds `Finding` + `Disagreement` + `*Verification`
    - `type Result struct { Findings []Merged; Ambiguous []AmbiguousCluster; Summary Summary }`
    - `type Summary`
    - `type Verification struct { Skeptic, Verdict, Notes string }`
    - `VerdictConfirmed` / `VerdictRefuted` / `VerdictUnverifiable` constants

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `emit.go`/`discover.go` type/I/O split is error-prone — public types mixed with file I/O | High | Split mechanically: move `Verification`/`Finding`/`Source` types first, compile-check in both packages, then relocate I/O behind `internal/reconcile/adapter` |
| Extraction breaks ATCR behavior — byte-identical fixtures drift | High | Lift-as-is; preserve `sortMerged` total order and integer-cross-multiply thresholds exactly; run full ATCR test corpus + fixture diff before merge (story 3 verifies) |
| Library accidentally imports a third-party dep or an `internal/*` ATCR package | Medium | Enforce stdlib-only via `golangci-lint`; testify confined to `*_test.go`; `go mod tidy` in `./reconcile/` must yield an empty `require` block |
| `SeverityRank` dual-ownership drift (library canonical copy vs. residual `merge.go:30` init-copy) | Medium | Library becomes the canonical owner; eliminate the redundant init-copy or have `internal/stream` source it from the library |
| Root `replace` directive confuses downstream `go mod tidy` for external consumers | Low | Document that `replace` is development-only; separate-repo publication (deferred) removes it |

---

**Created:** June 23, 2026 11:48:36AM
**Status:** Draft - Awaiting Acceptance Criteria
