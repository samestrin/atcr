# User Story 3: SARIF Line/File Anchoring

**Plan:** [25.0: SARIF Output Integration](../plan.md)

## User Story

**As an** integrator consuming ATCR's SARIF output in GitHub Advanced Security's Code Scanning tab
**I want** every SARIF result's `physicalLocation` to carry a correct, repo-root-relative `artifactLocation.uri` and a fully-populated `region` (including a synthesized `line 1, column 1` fallback for file-level findings that have no real line number)
**So that** every finding — not just the ones with a precise line — actually renders and anchors in the GitHub Security tab instead of silently failing to display

## Story Context

- **Background:** `reconcile.JSONFinding.Line` (`internal/reconcile/emit.go`) is `0` or negative for file-level findings — ATCR's existing FILE:LINE convention already special-cases this in `internal/ghaction/render.go`'s `location(f reconcile.JSONFinding) string` helper, which omits the line reference entirely when `Line<=0` (`if f.Line <= 0 { return f.File }`). SARIF has no equivalent "unknown line" representation: the base OASIS SARIF 2.1.0 spec allows omitting `region` entirely for a file-level result, but GitHub Code Scanning's renderer requires `region.startLine`, `startColumn`, `endLine`, and `endColumn` to ALL be present for a result to display at all in the Security tab — a result with `region` omitted (or partially populated) does not error, it simply never appears. This story implements the resolution the plan's Technical Planning Notes and documentation review already settled on: synthesize `startLine: 1, startColumn: 1, endLine: 1, endColumn: 1` whenever `Line<=0`, guaranteeing every finding — file-level or line-level — is visible in GitHub.
- **Assumptions:** Story 1's `renderSarif` / SARIF struct tree in `internal/report/sarif.go` already exists as the landing point for this logic (this story depends on it existing structurally, per the plan's dependency note, but does not redo it). `reconcile.JSONFinding.File` is already a repo-root-relative path (not absolute) by the time it reaches the report layer — no path-rewriting/normalization subsystem is being introduced here, only correct pass-through into `artifactLocation.uri`. Column information is not tracked anywhere in ATCR's finding pipeline today, so `startColumn`/`endColumn` are synthesized (not derived) in both the real-line and fallback cases per the plan's resolved design.
- **Constraints:** This story covers `physicalLocation` construction only — `artifactLocation.uri` and `region.startLine/startColumn/endLine/endColumn`. It must NOT implement the base SARIF document/struct-tree scaffolding (Story 1's scope), must NOT implement severity-to-`level` mapping (Story 2's scope), and must NOT touch CI documentation (Story 4's scope). The fallback logic applies strictly to the `Line<=0` case; findings with a valid `Line>0` must anchor to that real line, never to the line-1 fallback.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | Story 1 (SARIF Formatter Core) — requires `internal/report/sarif.go`'s struct tree and `renderSarif` entry point to exist as the attachment point for this location logic |

## Success Criteria (SMART Format)

- **Specific:** For any `reconcile.JSONFinding` with `Line>0`, the emitted SARIF result's `locations[0].physicalLocation` has `artifactLocation.uri == f.File` and `region.startLine == region.endLine == f.Line`, with `startColumn`/`endColumn` populated (non-zero); for any finding with `Line<=0`, the same object has `region.startLine == region.endLine == 1` and `region.startColumn == region.endColumn == 1`, and `artifactLocation.uri` is still correctly `f.File`.
- **Measurable:** `internal/report/sarif_test.go` includes an explicit table-driven case set (per `internal/report/render_test.go` conventions) covering at minimum: `Line>0` (normal anchoring), `Line==0`, and `Line<0` (both trigger the fallback) — all asserting exact `region` field values, not just non-nil presence.
- **Achievable:** Pure data-mapping logic inside a single new helper (e.g. `sarifLocation(f reconcile.JSONFinding) ...`) in the existing `internal/report/sarif.go` file — no new dependency, no new subsystem, mirrors the pattern already used by `internal/ghaction/render.go`'s `location()` helper for the analogous FILE:LINE fallback.
- **Relevant:** Directly resolves the single most important design divergence the plan's documentation review flagged — GitHub Code Scanning's `region`-required-for-display constraint — which is the plan's own stated highest-priority technical risk (flagged `[CRITICAL]` in `documentation/sarif-schema-reference.md`) and a named item in Acceptance Criteria ("File paths and line numbers correctly anchor to the git diff").
- **Time-bound:** Deliverable in the same implementation phase as Story 2 (both extend `sarif.go` after Story 1 lands), ahead of Story 4's CI documentation which assumes correctly-anchored output already exists.

## Acceptance Criteria Overview

1. A `sarifLocation` (or equivalently named) helper in `internal/report/sarif.go` builds `physicalLocation.artifactLocation.uri` from `reconcile.JSONFinding.File` unmodified (no absolute-path leakage, no `./` prefix stripping bugs) for every finding, regardless of `Line` value.
2. For findings with `Line>0`, `region.startLine` and `region.endLine` are both set to `f.Line`, and `region.startColumn`/`endColumn` are populated with a defined, non-zero value (e.g. `1`/`1` or a wider default), satisfying GitHub's all-four-fields-required constraint.
3. For findings with `Line<=0` (covering both `Line==0` and negative `Line`), the helper synthesizes `region.startLine=1, region.startColumn=1, region.endLine=1, region.endColumn=1` rather than omitting `region` or leaving any of the four fields zero-valued/absent, and this fallback path is covered by an explicit table-driven test case distinct from the normal-line case.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/25.0_sarif_output_integration/`_

## Technical Considerations

- **Implementation Notes:** Add a dedicated helper (e.g. `func sarifLocation(f reconcile.JSONFinding) sarifLocationObj` or equivalent, matching whatever `physicalLocation`/`region` struct names Story 1 establishes in `internal/report/sarif.go`) rather than inlining the `Line<=0` branch inside `renderSarif`'s main result-building loop — keeps the fallback logic isolated, unit-testable in isolation, and easy to reference from the table-driven test alongside Story 2's severity-mapping tests in the same file. Reference (not reuse) `internal/ghaction/render.go`'s `location()` `Line<=0` convention as the precedent for this special-case pattern, since SARIF's `region` has no direct equivalent to that helper's "omit the line" string-formatting approach.
- **Integration Points:** `internal/report/sarif.go` (new/extended helper, called from each result's `locations[]` construction inside `renderSarif`); `internal/report/sarif_test.go` (table-driven cases for `Line>0`, `Line==0`, `Line<0`); `internal/report/render_test.go`'s `TestRender_GoldenFiles` SARIF fixture (`internal/report/testdata/report.sarif.json`, if introduced by Story 1) should include at least one file-level (`Line<=0`) finding so the golden file itself exercises this fallback, not just the isolated unit test.
- **Data Requirements:** Sole input remains `reconcile.JSONFinding.File` and `.Line` (`internal/reconcile/emit.go`) — no new fields, no upstream schema change. Path consistency matters beyond this story's immediate scope: `artifactLocation.uri` must be stable/deterministic across repeated runs against the same diff, since GitHub's `partialFingerprints` deduplication keys off location data — this story's `File` pass-through must not introduce any non-deterministic transformation (e.g. no timestamp- or run-relative path rewriting).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Fallback coordinates (`line 1, col 1`) collide across multiple distinct file-level findings in the same file, causing GitHub to visually stack/obscure unrelated results at the same anchor point | Low | Accepted trade-off per the plan's resolved design — displaying all file-level findings (even stacked) is strictly better than the alternative of them not displaying at all; no further mitigation needed for this story, worth a one-line code comment noting the trade-off is intentional. |
| `region.startColumn`/`endColumn` are always synthesized (no real column data exists anywhere in the finding pipeline), which could be misread later as a data-fidelity bug rather than an intentional simplification | Low | Document the synthesized-column decision directly in `sarifLocation`'s doc comment so a future maintainer does not attempt a spurious "fix" by hunting for column data that does not exist upstream. |
| `Line<=0` boundary condition mis-implemented (e.g. only checking `Line==0`, missing negative `Line` values) silently produces an invalid/zero-valued `region` for negative-line findings | Medium | Explicit table-driven test case for `Line<0` (not just `Line==0`) as called out in the Success Criteria and AC Overview above, mirroring the `Line<=0` condition exactly as written in `internal/ghaction/render.go`'s existing precedent. |

---

**Created:** July 14, 2026 04:11:53PM
**Status:** Draft - Awaiting Acceptance Criteria
