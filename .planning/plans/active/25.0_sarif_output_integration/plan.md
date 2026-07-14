# Plan 25.0: SARIF Output Integration

## Metadata
**Plan Type:** feature
**Last Modified:** 2026-07-14

## Plan Overview
**Plan Type:** feature
**Plan Goal:** Add a fourth `atcr report` render target — SARIF 2.1.0 JSON — so ATCR's reconciled findings can feed GitHub Advanced Security's Code Scanning "Security" tab and GitLab CI's native SAST report widget, the two centralized security surfaces ATCR's existing direct-API `atcr github` PR integration does not reach.
**Target Users:** DevOps/security engineers integrating ATCR into enterprise CI pipelines that already consume SARIF from other SAST tools (CodeQL, Semgrep, etc.).
**Framework/Technology:** Go (`github.com/samestrin/atcr`), Cobra CLI, stdlib `encoding/json`.

## Objectives

1. Add SARIF 2.1.0 JSON as a new render target in `internal/report`, following the existing `FormatMarkdown`/`FormatJSON`/`FormatChecklist` pattern.
2. Enable `atcr report --format=sarif` to produce schema-valid SARIF output over reconciled ATCR findings.
3. Map ATCR severities (CRITICAL, HIGH, MEDIUM, LOW) to SARIF `level` values using the canonical `reconcile.NormalizeSeverity`/`SeverityRank` rubric.
4. Anchor SARIF results to the correct file paths and line numbers from the git diff, including synthesizing region coordinates (line 1, col 1) for the file-level (`Line<=0`) edge case to ensure GitHub display.
5. Synthesize `tool.driver.rules` from `reconcile.JSONFinding.Category` to satisfy GitHub's requirement that every result's `ruleId` matches a declared rule.
6. Provide CI integration documentation demonstrating how to upload ATCR's SARIF output to GitHub Advanced Security's Code Scanning tab and to GitLab CI's native SAST report widget.

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Pending - generate with `/create-user-stories @.planning/plans/active/25.0_sarif_output_integration/`
- **Estimated Count:** 4 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/25.0_sarif_output_integration/`

## Feature Analysis Summary

ATCR currently renders reconciled findings as markdown, JSON, or a checklist via `internal/report/render.go`'s `Render()` dispatcher, invoked through `atcr report --format=<md|json|checklist>`. Neither GitHub Advanced Security's org-wide Code Scanning "Security" tab nor GitLab CI's native SAST report widget can ingest any of those — both require SARIF (Static Analysis Results Interchange Format) 2.1.0 JSON. ATCR's existing `atcr github` command already posts a PR check and inline comments directly via the GitHub API (documented in `docs/github-action.md`); that flow is explicitly unaffected and out of scope. This plan adds SARIF purely as a **fourth sibling render format** alongside the existing three — no new subsystem, no new API integration, no auth tokens. The extension point is confirmed by codebase discovery: `internal/report/render.go`'s existing `FormatMarkdown`/`FormatJSON`/`FormatChecklist` constants, `ValidFormat()`, `Formats()`, and `Render()` switch are the exact shape a `FormatSarif` case slots into, and `cmd/atcr/report.go` already generalizes over `format` via `report.ValidFormat`/`report.Render` — so no new CLI command wiring is needed beyond the constant itself.

## Technical Planning Notes

- **Extension point:** `internal/report/render.go` — add `FormatSarif = "sarif"`, extend `ValidFormat()` and `Formats()`, add a `case FormatSarif: return renderSarif(w, findings)` arm to `Render()`.
- **New file:** `internal/report/sarif.go` implementing `renderSarif(w io.Writer, findings []reconcile.JSONFinding) error`, following the `renderJSON` pattern (indented `json.MarshalIndent` over a hand-built struct tree) already used for the other formats in the same file.
- **Rule generation:** GitHub requires `tool.driver.rules[]` to be populated with `id`, `shortDescription.text`, and `fullDescription.text`. The formatter must generate one rule entry per distinct `reconcile.JSONFinding.Category` to satisfy this linkage.
- **Severity mapping:** must key off the existing canonical rubric — `reconcile.NormalizeSeverity()` / `reconcile.SeverityRank` (`reconcile/severity.go`, `reconcile/merge.go`) — rather than redefining a fourth copy of the CRITICAL/HIGH/MEDIUM/LOW rubric. TD-0052 already documents the risk of rubric duplication across `internal/fanout/postprocess.go` and other sites; the SARIF path must not add to it. Map CRITICAL/HIGH → SARIF `error`, MEDIUM → `warning`, LOW → `note`.
- **Line anchoring:** `reconcile.JSONFinding.Line` can be `0` for file-level findings (see `internal/ghaction/render.go`'s `location()` helper). While the base SARIF spec allows omitting the `region` block, GitHub Code Scanning requires `startLine`, `startColumn`, `endLine`, and `endColumn` for a result to display. The formatter must synthesize these fields (e.g., `startLine: 1, startColumn: 1, endLine: 1, endColumn: 1`) when `Line<=0` to ensure GitHub visibility.
- **No new runtime dependency required.** The codebase already depends on `github.com/google/jsonschema-go` (see `go.mod`), usable in tests to validate `renderSarif`'s output against the official SARIF 2.1.0 schema. `owenrumney/go-sarif/v2` is documented as an optional alternative in `package-recommendations.md` if the SARIF surface grows beyond a single `results[]` array in a later epic.
- **CLI flag:** `atcr report --format=sarif` — the `--format` flag already exists on `atcr report` (default `"md"`); only its help text needs updating to mention `sarif`. `atcr review`/`atcr reconcile` do not render output and must not gain a duplicate flag (confirmed during `/refine-epic`).
- **Documentation:** `docs/ci-integration.md` already has a "Maintained PR Action" subsection demonstrating the doc structure (fenced YAML snippet + link to a deeper doc) a new SARIF subsection should mirror — piping `atcr review && atcr reconcile && atcr report --format=sarif` to `codeql-action/upload-sarif`, plus the GitLab CI SAST-widget equivalent.

## Documentation References

**Location:** [`documentation/`](documentation/) — see [documentation/README.md](documentation/README.md) for the full index.

- [CRITICAL] [SARIF 2.1.0 Schema Reference](documentation/sarif-schema-reference.md) — flags a direct conflict between the base SARIF spec (region optional) and GitHub Code Scanning's requirement that `region.startLine/startColumn/endLine/endColumn` be present for a result to display. This has been resolved in Technical Planning Notes by electing to synthesize fallback coordinates for `Line<=0` findings to guarantee GitHub display.
- [CRITICAL] [GitHub Code Scanning SARIF Integration Constraints](documentation/github-code-scanning-integration.md) — surfaces a requirement not in the original Proposed Solution: every result's `ruleId` must match an entry in `tool.driver.rules` (with `id`, `shortDescription.text`, `fullDescription.text`), so `renderSarif` needs a `rules[]` builder (candidate: one rule per distinct `reconcile.JSONFinding.Category`), not just a flat `results[]` array.
- [IMPORTANT] [Schema-Validating SARIF Output with jsonschema-go](documentation/schema-validation-with-jsonschema-go.md) — the `Resolve`/`Validate` path needs a local SARIF 2.1.0 schema fixture (e.g. `testdata/sarif-schema-2.1.0.json`) that does not exist in the repo yet; add it as an explicit `sarif_test.go` task.
- [REFERENCE] [encoding/json Conventions for renderSarif](documentation/json-encoding-conventions.md)

## Implementation Strategy

1. Add `FormatSarif` to `internal/report/render.go`'s format enum/dispatcher.
2. Implement `internal/report/sarif.go`: SARIF 2.1.0 struct tree (`run.tool.driver.{name,rules}`, `run.results[].{ruleId,level,message,locations}`), severity-to-level mapping via `reconcile.NormalizeSeverity`, synthesize rules from categories, line-anchoring with fallback to line/col 1 for `Line<=0`.
3. Unit test `internal/report/sarif_test.go` (table-driven: severity mapping, rule generation, line anchoring including the `Line<=0` fallback, valid-JSON/schema shape) plus a golden-file fixture mirroring `internal/reconcile/testdata/golden/report.md`.
4. Update `cmd/atcr/report.go`'s `--format` flag help text.
5. Add the CI-integration documentation example to `docs/ci-integration.md` (GitHub Code Scanning upload + GitLab CI SAST-widget equivalent), consistent with the existing two-step `atcr review && atcr reconcile` pattern.

## Recommended Packages

No high-ROI package is required — see [package-recommendations.md](package-recommendations.md). `owenrumney/go-sarif/v2` is documented as an optional alternative to the default hand-rolled-struct approach if the SARIF surface grows in a future epic.

## User Story Themes

1. **SARIF formatter core** — as a maintainer, `atcr report --format=sarif` produces syntactically valid SARIF 2.1.0 JSON over the reconciled findings.
2. **Severity mapping** — as an integrator, ATCR severities (CRITICAL, HIGH, MEDIUM, LOW) map correctly and consistently (via the shared rubric) to SARIF's `level` enum.
3. **Line/file anchoring** — as an integrator, SARIF results anchor to the correct file and line from the git diff, including synthesizing valid region coordinates (line 1, col 1) for file-level (`Line<=0`) findings so they display in GitHub.
4. **CI integration documentation** — as an integrator, documented, copy-pasteable examples exist for uploading ATCR's SARIF output to GitHub Advanced Security's Code Scanning tab and to GitLab CI's native SAST report widget.

## Planning Success Criteria

- `atcr report --format=sarif` produces valid SARIF JSON (schema-checkable via the already-vendored `google/jsonschema-go`).
- ATCR severities map correctly and consistently to SARIF levels, sourced from the single canonical `reconcile.SeverityRank`/`NormalizeSeverity` rubric.
- File paths and line numbers correctly anchor to the git diff, with fallback coordinates (line 1, col 1) for the `Line<=0` file-level edge case to guarantee GitHub display.
- Every result's `ruleId` matches an entry in `tool.driver.rules` derived from the finding's Category, satisfying GitHub's strict display constraints.
- A documentation example exists for both the GitHub Code Scanning upload and the GitLab CI SAST-widget equivalent.
- No duplication of, or interference with, the existing `atcr github` direct-API PR-comment flow.

## Risk Mitigation

- **Risk:** Hand-rolled SARIF schema drifts from the SARIF 2.1.0 spec (wrong `level` enum, missing required fields), causing GitHub/GitLab to silently reject or misrender the upload. **Mitigation:** schema-validate `renderSarif`'s output in tests using the already-vendored `google/jsonschema-go`, and manually verify a real upload to a scratch repo's Code Scanning tab before marking AC1 done.
- **Risk:** Severity-rubric duplication (already flagged as TD-0052 elsewhere in the codebase). **Mitigation:** the SARIF formatter imports `reconcile.NormalizeSeverity`/`SeverityRank` directly rather than reimplementing the CRITICAL/HIGH/MEDIUM/LOW mapping.

## Next Steps
1. `/find-documentation @.planning/plans/active/25.0_sarif_output_integration/`
2. `/create-documentation @.planning/plans/active/25.0_sarif_output_integration/`
3. `/create-user-stories @.planning/plans/active/25.0_sarif_output_integration/`
4. `/create-acceptance-criteria @.planning/plans/active/25.0_sarif_output_integration/`
5. `/design-sprint @.planning/plans/active/25.0_sarif_output_integration/`
6. `/create-sprint @.planning/plans/active/25.0_sarif_output_integration/`
