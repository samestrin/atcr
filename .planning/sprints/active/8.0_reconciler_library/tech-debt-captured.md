# Tech Debt Captured — Sprint 8.0 Reconciler Library

Findings deferred during `/execute-sprint`. Read by `/execute-code-review` (Phase 1 init) and pre-seeded into the adversarial TD stream (`SOURCE=execute-sprint`).

> Items TD-001..TD-004 are **forward-looking Phase-2 execution hazards** surfaced by the Phase-1 GATE (fresh-context hostile-integrator subagent). Phase 1's delivered code has **no defects** — these describe how Phase 2 must be executed. They are recorded here so the next `/execute-sprint` run (Phase 2) accounts for them.

## TD-001 — Phase 2 is a PORT, not a verbatim move (HIGH)
**Origin:** Phase 1, task 1.5 GATE (forward-looking), 2026-06-23
**File:** internal/reconcile/merge.go:56, cluster.go, dedupe.go, reconcile.go:64, disagree.go
**Issue:** The pure logic Phase 2 must relocate is hard-coupled to `internal/stream` — `Merged` embeds `stream.Finding`, and `Cluster`/`Merge`/`DedupeCluster`/`Reconcile`/`BuildDisagreements` take or return `[]stream.Finding` (merge.go ~21 `stream.` refs, disagree.go ~11, cluster.go ~7, dedupe.go ~6). Moving these files verbatim into the stdlib-only library would force a forbidden `reconcile -> internal/stream` import.
**Why accepted:** Phase 1 scope is scaffold + type/alias only; reducing this coupling is exactly Phase 2's job. Not a Phase-1 defect.
**Fix in:** Phase 2 — execute as a PORT: rewrite `stream.Finding` -> `reconcile.Finding` in each moved file; relocate `SeverityRank`/`NormalizeSeverity` into the library (internal/stream/severity.go is self-contained + stdlib-only, so mechanical). Verify the corpus stays byte-identical after the field-name swap (fixtures are the oracle).

## TD-002 — Two `Source` types must be bridged at the boundary (HIGH)
**Origin:** Phase 1, task 1.5 GATE (forward-looking), 2026-06-23
**File:** internal/reconcile/discover.go:25
**Issue:** `internal/reconcile` keeps its discovery `Source` (Name + `[]stream.Finding` + Skipped + SkippedFiles); the library now defines a public `Source` (Name + `[]Finding`). `Reconcile(sources []Source, ...)` currently binds the internal one. After Phase 3 flips consumers to the library, the pipeline signature must take the library `Source`.
**Why accepted:** Deliberate Phase-1 design — the library `Source` must be stdlib-only and cannot hold `stream.Finding`/`SkippedRow`, so it cannot alias the internal type. Bridging belongs to the Phase-2 adapter.
**Fix in:** Phase 2 — moved `Reconcile` takes the library `Source`; discovery output (`discover.Source`) is converted to `reconcile.Source` in the adapter/discovery layer. Document the two-Source split.

## TD-003 — Path-validation fields are a lossy library boundary (MEDIUM)
**Origin:** Phase 1, task 1.5 GATE (forward-looking), 2026-06-23
**File:** reconcile/finding.go
**Issue:** Library `Finding` intentionally omits `PathValid`/`PathWarning`/`PathSuggestion`, but `Merged`/`JSONFinding` and report.md rendering (emit.go renderMarkdown reads `m.PathWarning`/`m.PathSuggestion`) depend on them. Any reconcile path needing path-validation must NOT route those fields through the library `Finding`.
**Why accepted:** Documented design — path validation stays ATCR-internal in the adapter.
**Fix in:** Phase 2 — `FromFinding` + the ATCR I/O layer re-stamp path-validation onto `stream.Finding` AFTER the library round-trip; lock it down with `TestBoundaryAdapter_FindingConversionRoundTrip` (task 2.1).

## TD-004 — Phase-2 task list symbol-location accuracy (LOW)
**Origin:** Phase 1, task 1.5 GATE (forward-looking), 2026-06-23
**Issue:** Phase-2 task 2.2.7 lists `AmbiguousCluster` under `ambiguous.go`, but the `AmbiguousCluster` type is declared in `dedupe.go` (ambiguous.go holds `AmbiguousID`/`AmbiguousHash`). Task 2.2.9 correctly targets `internal/stream/severity.go` (note: there is no `severity.go` under internal/reconcile; the `SeverityRank` copy lives in merge.go:30, which must also be eliminated per AC 02-05).
**Why accepted:** Documentation accuracy only; does not affect Phase-1 code.
**Fix in:** Phase 2 — when moving, follow the real symbol locations (grep before moving): `AmbiguousCluster` in dedupe.go; `SeverityRank` copy in merge.go:30.
