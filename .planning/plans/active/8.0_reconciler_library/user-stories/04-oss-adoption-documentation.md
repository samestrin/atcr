# User Story 4: OSS Adoption Documentation and Apache 2.0 License

**Plan:** [8.0: Reconciler Library Module Extraction](../plan.md)

## User Story

**As an** open-source tool author evaluating merge libraries
**I want** the standalone `reconcile` module to ship a README with godoc, a runnable `example_test.go`, and an Apache 2.0 `LICENSE`
**So that** I can evaluate the reconciler's deterministic clustering, dedupe, and disagreement-preserving merge, and embed it in my own tool under a permissive license without reading ATCR internals.

## Story Context

- **Background:** The reconciler is currently buried under `internal/reconcile` inside the ATCR binary, so no external tool can import it without pulling in the whole CLI. This story is the OSS on-ramp: it produces the documentation and licensing artifacts that let an adopter discover the public API (`Reconcile(sources []Source, opts Options) Result` plus the lifted-as-is `Source`/`Finding`/`Merged`/`Options`/`Result`/`Summary`/`Verification` types), run a working example, and embed under Apache 2.0. It covers AC#5 (Go docs + runnable example in the module README) and the Apache 2.0 half of AC#6 (an Apache 2.0 `LICENSE` is present). The commercial-license half of AC#6 belongs to Story 5; this story delivers only the permissive license file.
- **Assumptions:** The module scaffold and lifted-as-is public API from Stories 1 and 2 are in place at `./reconcile/` with its own `go.mod`. The public API surface is stable and matches the lift-as-is decision (`Reconcile`, `Source`, `Merged`, `Options{ReconciledAt, Partial, Merges, Root}`, `Result`, `Summary`, `Verification`, `VerdictConfirmed/Refuted/Unverifiable`, library-owned `Finding`). The adopter reads Go and is comfortable with `go doc` / pkg.go.dev-style documentation; they do not need a SARIF or REST adapter to evaluate fitness.
- **Constraints:** Library is strictly stdlib-only in non-test files; testify is allowed only in `*_test.go`. The README and example must reflect the lifted-as-is API exactly — no documentation of the deferred clean API (`(*Result, error)`, `ReconciledFinding`, `Options{LineTolerance, SimilarityThreshold}`). The example must compile and run under `go test` without external dependencies or network access. Apache 2.0 `LICENSE` is the verbatim Apache text with the copyright holder line filled in; no enforcement code, no click-through. This story does not touch the `LICENSE-COMMERCIAL.md` placeholder (Story 5) and does not update the leaderboard methodology doc (AC#8, Story 6).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (module scaffold + `go.mod`), Story 2 (lifted-as-is public API surface) |

## Success Criteria (SMART Format)

- **Specific:** A `reconcile/README.md` documents the public `Reconcile(sources []Source, opts Options) Result` entry point and the lifted-as-is types (`Source`, `Finding`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, `Verdict*` constants), and a runnable `reconcile/example_test.go` exercises a two-reviewer merge end-to-end producing a `Result` with merged findings, confidence, and (where inputs disagree) a disagreement annotation.
- **Measurable:** `go test ./reconcile/...` runs `example_test.go` green; `go doc github.com/samestrin/atcr/reconcile` renders the package doc and the example; `reconcile/LICENSE` is present and matches the Apache 2.0 full text with a valid copyright line.
- **Achievable:** The API surface already exists from Stories 1 and 2; the work is authoring a README, one `Example` function, and dropping in the Apache 2.0 text. The testify documentation already establishes the `example_test.go` + godoc convention (AC#5).
- **Relevant:** This is the only artifact an OSS adopter reads before deciding to embed — without it the module is an unlicensed, undocumented internal package and AC#5/AC#6 fail. It is the direct path to OSS adoption and leaderboard credibility (the permissive half of the dual-license strategy).
- **Time-bound:** Land within the same sprint as Stories 1 and 2 so the module is never published in a state missing docs or a license; the README and example are authored after the API surface is frozen, not before.

## Acceptance Criteria Overview

1. `reconcile/README.md` documents the public API (`Reconcile`, `Source`, `Finding`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, `Verdict*` constants) and the deterministic clustering / Jaccard dedupe / confidence / disagreement-preserving merge behavior, with an install + quickstart snippet using the lifted-as-is signature.
2. `reconcile/example_test.go` is a runnable godoc `Example` function that constructs two `Source` values, calls `Reconcile`, and asserts on a merged `Result` — it compiles, runs under `go test ./reconcile/...` without external dependencies, and renders in `go doc` output.
3. `reconcile/LICENSE` contains the verbatim Apache 2.0 full text with the copyright holder line populated; no enforcement code is added and the file is module-root so `go list -m` and pkg.go.dev surface it automatically.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/8.0_reconciler_library/`_

## Technical Considerations

- **Implementation Notes:** The README is hand-authored Markdown at `reconcile/README.md` covering: what the reconciler is (deterministic clustering at `FILE, LINE±3`, token-set Jaccard dedupe with exact 0.7/0.4 integer-cross-multiply thresholds, max-severity merge with `<lo> vs <hi>` disagreement annotation, confidence v2 `VERIFIED/HIGH/MEDIUM/LOW`, ambiguity sidecar), install (`go get github.com/samestrin/atcr/reconcile`), the lifted-as-is `Reconcile(sources []Source, opts Options) Result` signature and `Options{ReconciledAt, Partial, Merges, Root}` fields, and a quickstart mirroring the example. The example lives in `reconcile/example_test.go` as a godoc `Example` function (standard `func Example()` or `func ExampleReconcile()`), using only stdlib assertions or plain `if`/`fmt` checks (testify is allowed in `*_test.go`, but the example should stay dependency-light so `go doc` renders cleanly). The example must reflect the real types: `Source{Name string; Findings []Finding}` and `Finding`'s wire fields (Severity, File, Line, Problem, Fix, Category, EstMinutes, Evidence). Output ordering relies on the deterministic total order (`sortMerged` — severity desc, then file, then line) so the example's expected output is stable.
- **Integration Points:** The README's signature and type names must match the library package exactly — drift between docs and code breaks adopter trust and fails AC#5. The `example_test.go` participates in the same `go test ./reconcile/...` run as the moved pure-logic tests (e.g., the relocated `emit_test.go` `TestReconcile_TwoReviewersAgreeHighConfidence`), so the example's inputs must not collide with fixture-driven tests. The Apache 2.0 `LICENSE` at module root is what pkg.go.dev and `go-licenses` surface; it must be the full Apache text, not a pointer.
- **Data Requirements:** No schema changes. The example uses inline `Source`/`Finding` literals — two reviewers with overlapping findings so the output demonstrates merge + confidence, and one disagreement case so the `<lo> vs <hi>` annotation and/or ambiguity sidecar are visible. The Apache `LICENSE` requires the copyright holder name (Sam Estrin / samestrin) and the year; the rest is verbatim Apache 2.0 boilerplate.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| README documents the deferred clean API (`(*Result, error)`, `ReconciledFinding`) instead of the lifted-as-is surface | High | Pin the README to the lift-as-is signature from Story 2; cross-check every type name against the library's `go doc` output before merge. |
| `example_test.go` drifts from the real `Finding` wire fields after Story 2 lands type changes | Medium | Author the example after Story 2's API is frozen; compile-check `go test ./reconcile/...` in the same PR. |
| Example output is non-deterministic because it depends on map iteration or unsorted input | Medium | Rely on `sortMerged`'s total order (severity desc, then file, then line); assert on sorted output and document that determinism is a feature, not a coincidence. |
| Apache 2.0 `LICENSE` missing the copyright line or using a truncated text, so pkg.go.dev / `go-licenses` misreport the license | Medium | Use the verbatim Apache 2.0 full text from apache.org with the copyright holder line populated; verify with `go-licenses` or manual inspection. |
| Adopter reads the README and expects the commercial/proprietary path to be documented here | Low | Add a one-line pointer in the README to `LICENSE-COMMERCIAL.md` (delivered by Story 5) so the dual-license intent is visible without duplicating Story 5's content. |

---

**Created:** June 23, 2026 11:48:36AM
**Status:** Draft - Awaiting Acceptance Criteria
