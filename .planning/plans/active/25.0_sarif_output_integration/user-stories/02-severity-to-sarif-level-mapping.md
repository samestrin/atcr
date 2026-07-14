# User Story 2: Severity-to-SARIF-Level Mapping

**Plan:** [25.0: SARIF Output Integration](../plan.md)

## User Story

**As an** integrator consuming ATCR's SARIF output in GitHub Advanced Security or GitLab CI
**I want** ATCR's four canonical severities (CRITICAL, HIGH, MEDIUM, LOW) to map deterministically to SARIF's `level` enum (`error`, `warning`, `note`)
**So that** GitHub Code Scanning and GitLab's SAST widget rank and display findings by the same severity ATCR already assigned, with no re-triage needed on the consuming side

## Story Context

- **Background:** ATCR maintains one canonical severity rubric — `reconcile.SeverityRank` (`reconcile/severity.go`) with `reconcile.NormalizeSeverity()` as its case/whitespace-insensitive lookup key — and every existing severity consumer (report's md/json/checklist renderers, `internal/fanout/postprocess.go`'s constraint enforcement, `internal/ghaction`) already reuses it rather than redefining CRITICAL/HIGH/MEDIUM/LOW comparisons locally. `internal/fanout/postprocess.go:14` is cited in the plan as the existing precedent for correct reuse (it calls `reclib.NormalizeSeverity`/`reclib.SeverityRank` directly) and TD-0052 already documents the risk class this story must not add to: a fifth, drifted redefinition of the rubric.
- **Assumptions:** Story 1 (SARIF formatter core) has established or will establish `internal/report/sarif.go` and the base `renderSarif(w io.Writer, findings []reconcile.JSONFinding) error` document structure this story's mapping function plugs into. `reconcile.JSONFinding.Severity` is the raw string field this story's function receives (after normalization).
- **Constraints:** Must call `reconcile.NormalizeSeverity(f.Severity)` before ranking/comparing — never re-parse or re-case the raw string independently. Must not introduce a second severity constant map or a second CRITICAL/HIGH/MEDIUM/LOW string-comparison chain anywhere in the SARIF path. Output must be restricted to the three GitHub-recognized display levels (`error`, `warning`, `note`) — no `none`. An unrecognized/empty severity token (rank 0, per `SeverityRank`'s documented behavior) must resolve to a defined, non-crashing fallback level rather than an empty string or panic.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | User Story 1 (SARIF formatter core / `internal/report/sarif.go` base document structure and `renderSarif` must exist for this mapping function to be wired in) |

## Success Criteria (SMART Format)

- **Specific:** A standalone, independently-testable function `sarifLevel(severity string) string` in `internal/report/sarif.go` maps any input severity string to exactly one of `"error"`, `"warning"`, `"note"`, using `reconcile.NormalizeSeverity()` and `reconcile.SeverityRank` (or their `reclib` re-export alias per existing package convention) as its sole source of truth — never a local redefinition of the CRITICAL/HIGH/MEDIUM/LOW comparison.
- **Measurable:** Table-driven tests in `internal/report/sarif_test.go` cover all four canonical severities (CRITICAL→error, HIGH→error, MEDIUM→warning, LOW→note) plus at least one unrecognized/edge-case input (empty string, lowercase, unknown token, whitespace-padded) with an assertion on the resulting fallback level; every `renderSarif` result's `level` field in the golden/sample fixture matches this mapping exactly.
- **Achievable:** The mapping is a single small function reusing existing exported rubric symbols already imported by `internal/report` (`reconcile.NormalizeSeverity`, `reconcile.SeverityRank` — see `render.go`'s existing `reclib`/`reconcile` import pattern) — no new dependency, no schema change, no new data flow.
- **Relevant:** Directly satisfies Acceptance Criterion 2 of the plan ("The SARIF output correctly maps ATCR severities... to SARIF levels") and the plan's explicit risk mitigation against TD-0052-style rubric duplication.
- **Time-bound:** Completes within the same sprint as Story 1, before Story 3 (line/region anchoring) begins integration testing against a full sample findings set.

## Acceptance Criteria Overview

1. `sarifLevel(severity string)` returns `"error"` for CRITICAL and HIGH, `"warning"` for MEDIUM, and `"note"` for LOW, using `reconcile.NormalizeSeverity`/`reconcile.SeverityRank` as the sole comparison source (verified by code inspection: no second severity constant map exists in `internal/report/sarif.go`).
2. `renderSarif` calls `sarifLevel` for every finding's `result.level` field — no direct string comparison against `Severity` anywhere else in `sarif.go`.
3. Table-driven tests in `internal/report/sarif_test.go`, following the `internal/report/render_test.go` `t.Run` convention, cover all four canonical severities plus an unrecognized/edge-case severity string, and pass in CI.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/25.0_sarif_output_integration/`_

## Technical Considerations

- **Implementation Notes:** Add `sarifLevel(severity string) string` to `internal/report/sarif.go` (same file as Story 1's `renderSarif`, kept as a distinct, independently-testable unit per the plan's technical grounding). Implementation shape: normalize via `reconcile.NormalizeSeverity(severity)`, then branch on `reconcile.SeverityRank[normalized]` (or the normalized string directly) — `rank >= SeverityRank["HIGH"]` → `"error"`, `rank == SeverityRank["MEDIUM"]` → `"warning"`, `rank == SeverityRank["LOW"]` → `"note"`, unrecognized/rank-0 → a documented fallback (recommend `"warning"`, matching SARIF's neutral middle level, since GitHub has no `none` display level and silently dropping the result would hide it entirely).
- **Integration Points:** Consumed exclusively by `renderSarif` (Story 1) when building each `run.results[].level` field. Does not touch `internal/report/render.go`'s dispatcher, `cmd/atcr/report.go`, or any other renderer (md/json/checklist are unaffected and untouched).
- **Data Requirements:** Input is `reconcile.JSONFinding.Severity` (already-existing field, no schema change). No new struct fields, no new JSON schema elements beyond the `level` string SARIF 2.1.0 already requires.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A future edit re-implements severity comparison locally inside `sarif.go` instead of calling `sarifLevel`, silently creating a second rubric copy (the TD-0052 failure mode) | Medium | Code review / lint check confirms `sarif.go` contains exactly one severity-comparison site (`sarifLevel`); table-driven test asserts `renderSarif`'s output `level` for each severity matches `sarifLevel`'s own output, catching drift if the two diverge |
| Unrecognized/malformed severity strings (rank 0) produce an invalid or missing SARIF `level`, causing GitHub to reject the whole SARIF upload rather than just misrank one result | Medium | Explicit fallback branch in `sarifLevel` guarantees one of the three valid enum values is always returned; edge-case test asserts no panic and a valid enum value for empty/unknown input |
| SARIF `level` enum expectations drift from GitHub's actual accepted values in a future spec revision | Low | `sarifLevel`'s three-value output set is documented inline with a comment citing the GitHub Code Scanning constraint (no `none`); schema-validation test (Story 1) catches any structurally invalid level string |

---

**Created:** July 14, 2026 04:11:53PM
**Status:** Draft - Awaiting Acceptance Criteria
