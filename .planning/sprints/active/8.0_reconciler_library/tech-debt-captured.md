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

## TD-005 — Library `Merge` does not propagate input `Finding.Verification` (LOW)
**Origin:** Phase 2, task 2.2.A ADVERSARIAL REVIEW (fresh-context subagent), 2026-06-23
**File:** reconcile/merge.go:54
**Issue:** The library `Merge(group []Finding) Merged` builds the merged `Finding` without copying any input `Finding.Verification`. Harmless at the real ATCR boundary — reconcile-input findings never carry a verification block (the verify stage stamps verdicts post-reconcile onto the JSONFinding layer, where the adapter preserves *Verification pointer identity) — but a direct external embedder of the public library could be surprised that a populated input Verification is silently dropped on merge.
**Why accepted:** Out of scope for the byte-identical extraction (changing `Merge`'s contract would alter behavior). Not exercised by any ATCR path; pointer-identity at the layer that matters (JSONFindings/adapter) is verified.
**Fix in:** follow-on (clean-API epic) — either carry `Verification` through `Merge` with winning-verdict precedence (mirroring the internal `mergeVerification`), or document on `Merge`/`Finding` that core merge intentionally does not propagate input Verification (it is stamped post-reconcile).

## TD-006 — Adapter conversion duplicated in lib.go (drift risk) (LOW)
**Origin:** Phase 2, task 2.5 GATE (fresh-context hostile integrator), 2026-06-23
**File:** internal/reconcile/lib.go, internal/reconcile/adapter/adapter.go
**Issue:** The stream.Finding <-> reconcile.Finding conversion exists in two places: `internal/reconcile/lib.go` (`toLibFinding`/`fromLibFinding`, used by the Phase-2 Reconcile wrapper) and `internal/reconcile/adapter` (`ToFinding`/`FromFinding`, the public boundary). They are independent field copies that could drift. The duplication is deliberate for Phase 2 (an import cycle forbids the internal Reconcile wrapper from importing the adapter, and the adapter is the permanent Phase-3 boundary while the wrapper is transitional). The adapter IS exercised (adapter_test.go `TestBoundaryAdapter_FindingConversionRoundTrip`, 100% coverage) — the gate's "unexercised" note is inaccurate; the real issue is the duplication.
**Why accepted:** Cycle-driven; both copies are exhaustive field maps validated by tests; no consumer routes through the adapter until Phase 3.
**Fix in:** Phase 3 — when consumers flip to the library, route discovery/conversion through `internal/reconcile/adapter` and delete `lib.go`'s `toLibFinding`/`fromLibFinding` (and the transitional Reconcile wrapper), collapsing to one conversion implementation.
