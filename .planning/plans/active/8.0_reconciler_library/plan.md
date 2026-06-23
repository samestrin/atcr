## Plan Overview
**Plan Type:** Feature Development
**Plan Goal:** Extract ATCR's deterministic reconciler from `internal/reconcile` into a standalone, stdlib-only Go module (`github.com/samestrin/atcr/reconcile`) consumed via a root `replace` directive, with a clean public API lifted as-is, one JSON adapter, and a dual-licensing path. This turns the core architectural moat into a separable, inspectable asset other tools can embed — and makes ATCR the reference implementation — with zero behavioral change to ATCR itself.
**Target Users:** ATCR maintainer (reference implementation), external tool authors / devtools vendors (embed + adapt), OSS adopters (Apache 2.0), proprietary vendors (commercial license), leaderboard consumers (reference-impl citation).
**Framework/Technology:** Go 1.25; standalone nested module (`go.mod` at `./reconcile/`); stdlib-only library + `encoding/json` adapter; testify tests; golangci-lint; GitHub Actions CI.

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Pending - generate with `/create-user-stories @.planning/plans/active/8.0_reconciler_library/`
- **Estimated Count:** 6 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/8.0_reconciler_library/`

## Feature Analysis Summary
The reconciler is genuinely unique: deterministic location clustering, token-set Jaccard dedupe with exact integer-threshold boundaries (0.7/0.4), max-severity merge with `<lo> vs <hi>` disagreement annotation, confidence scoring, an ambiguity sidecar, and verify-precedence `mergeVerification`. It is currently buried under `internal/reconcile` and entangled with ATCR's file I/O and path-validation machinery. The extraction's central task is splitting `emit.go` and `discover.go` — which mix public types (`Verification`, `JSONFinding`, `Verdict*` constants, `Source`) with file I/O — so the types + pure logic move to the library while the I/O + path-validation (`PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged`) + `gate.go` + `validate.go` stay in an ATCR adapter. The lift-as-is mandate keeps `Reconcile`/`Source`/`Merged`/`Options`/`Result`/`Summary` shapes identical, so the work is mechanical-move-dominant and validated by the existing test corpus rather than new RED tests (new RED tests apply only to the library `Finding` type, the boundary adapter, and the JSON adapter). Epics 13.0/13.2/13.3 will later replace the dedup/confidence algorithms, so this extraction must land first on a stable surface.

## Technical Planning Notes
- New module at `./reconcile/` with its own `go.mod`; root `go.mod` adds `replace github.com/samestrin/atcr/reconcile => ./reconcile`.
- Library owns the canonical `SeverityRank`/`NormalizeSeverity` (collapse `merge.go:30`'s init-copy); the levenshtein scope is decided in Phase 0 — it is currently used only by ATCR path-validation (`stream/suggest.go`), not by dedupe (which uses Jaccard), so moving it adds a dependency with no core benefit.
- `Verification` becomes public library API (chosen over interface-decoupling) so `mergeVerification`/`BuildDisagreements` stay behaviorally identical; `gate.go` (ATCR) imports the public type + `Verdict*` constants.
- The library defines one `Finding` (core wire fields + `Disagreement` + `*Verification`); ATCR's `JSONFinding` wraps it + path-validation fields at the adapter boundary; `stream.Finding` ↔ `reconcile.Finding` conversion happens at the boundary.
- The JSON adapter round-trips `atcr-findings/v1` (`docs/findings-format.md`): external stream → `[]Source` and `Result` → external stream.

## Implementation Strategy
Phase 0 resolves the public-API boundary: split `emit.go`/`discover.go` types from I/O, decide the levenshtein scope, and confirm the ATCR adapter location. Then scaffold the nested module + `go.mod`, mechanically move the pure types and logic (cluster, dedupe, merge, confidence, disagree, ambiguous, attribution, severity), swap ATCR imports to the library behind a thin boundary adapter, and verify the full test corpus stays byte-identical. Build the JSON adapter, README + godoc example, and licensing files, then add independent module CI on tag push. Each move is verified by `go test ./...` green in both modules before proceeding; the fixtures across epics must remain byte-identical.

## Recommended Packages
No high-ROI packages identified (library is intentionally stdlib-only; testify + golangci-lint already cover test/lint needs, and the JSON adapter uses `encoding/json`).

## User Story Themes
- **Persona: ATCR Maintainer** — Journey: keep ATCR as the reference implementation with no behavioral change after the reconciler moves to a module.
- **Persona: External Tool Author** — Journey: embed the reconciler via a clean public API without importing the ATCR binary.
- **Persona: External Tool Author (format)** — Journey: convert an external JSON finding stream into `[]Source` and back via the adapter.
- **Persona: OSS Adopter** — Journey: evaluate and embed the library under Apache 2.0 with docs and a runnable example.
- **Persona: Proprietary Vendor** — Journey: embed the reconciler under a clear commercial licensing path.
- **Persona: Leaderboard Maintainer / Release Engineer** — Journey: cite the standalone reference implementation and ship independent module CI on tag push.

## Planning Success Criteria
- `github.com/samestrin/atcr/reconcile` exists with its own `go.mod` and passes its own CI (AC#1, AC#7).
- Public API exposes `Reconcile(sources []Source, opts Options) Result` with stable lifted types (AC#2).
- ATCR imports the module; all existing ATCR tests pass with zero behavioral change and byte-identical fixtures (AC#3).
- JSON adapter converts an external finding stream into `[]Source` and a `Result` into an external finding stream (AC#4).
- Go docs + a runnable example are published in the module README (AC#5).
- Apache 2.0 `LICENSE` + `LICENSE-COMMERCIAL.md` placeholder present (AC#6).
- Leaderboard methodology references the extracted reconciler as the reference implementation (AC#8).

## Risk Mitigation
- **Extraction breaks ATCR behavior (Low / High):** lift-as-is; keep the full test corpus green and fixtures byte-identical before merge.
- **`emit.go`/`discover.go` type/I/O split is error-prone (Med / High):** split mechanically — move `Verification`/`Finding`/`Source` types first, compile-check in both packages, then relocate I/O behind the adapter.
- **13.x dedup-replacement epics land mid-extraction (Med / Med):** sequence extraction before them; pin module semver so ATCR can pin a version while the library evolves.

## Next Steps
1. `/find-documentation @.planning/plans/active/8.0_reconciler_library/`
2. `/create-documentation @.planning/plans/active/8.0_reconciler_library/`
3. `/create-user-stories @.planning/plans/active/8.0_reconciler_library/`
4. `/create-acceptance-criteria @.planning/plans/active/8.0_reconciler_library/`
5. `/design-sprint @.planning/plans/active/8.0_reconciler_library/`
6. `/create-sprint @.planning/plans/active/8.0_reconciler_library/`
