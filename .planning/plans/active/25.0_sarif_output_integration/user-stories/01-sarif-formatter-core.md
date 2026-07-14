# User Story 1: SARIF Formatter Core

**Plan:** [25.0: SARIF Output Integration](../plan.md)

## User Story

**As a** maintainer of ATCR's report layer
**I want** `atcr report --format=sarif` to exist and produce a syntactically valid, top-level SARIF 2.1.0 JSON document over the reconciled findings
**So that** SARIF becomes a fourth first-class render target alongside markdown, JSON, and checklist — the foundation later stories build severity mapping, line/region anchoring, and CI documentation on top of

## Story Context

- **Background:** `internal/report/render.go` currently dispatches three formats (`FormatMarkdown`, `FormatJSON`, `FormatChecklist`) through a `Render(w io.Writer, findings []reconcile.JSONFinding, format string) error` switch, each backed by its own `render*` function in the same file. `cmd/atcr/report.go` already exposes a `--format` flag (default `"md"`) that is validated via `report.ValidFormat()` and passed straight into `report.Render()`; `internal/mcp/handlers.go`'s `handleReport` routes through the same `report.Render()` call. Neither GitHub Advanced Security's Code Scanning tab nor GitLab CI's SAST widget can ingest ATCR's existing three formats — both require SARIF 2.1.0 JSON, which this plan adds as the missing fourth target.
- **Assumptions:** `reconcile.JSONFinding` (Severity, File, Line, Problem, Fix, Category, etc., defined in `internal/reconcile/emit.go`) is the sole and sufficient input for the SARIF document — no new data collection or upstream schema change is needed. The `--format` flag lives only on `atcr report`; `atcr review` and `atcr reconcile` render no output and must not gain a duplicate flag. No new runtime dependency is required (hand-rolled struct tree + stdlib `encoding/json`, mirroring the existing `renderJSON` pattern).
- **Constraints:** This story covers structural validity only — it must NOT implement severity-to-`level` mapping logic (Story 2), region/line-anchoring fallback logic (Story 3), or CI documentation (Story 4). It must, however, stand up the `rules[]` array at a structural level (one entry per distinct `Category`) so Story 2/3 have a place to attach their mapping logic without re-architecting `sarif.go`. Output must remain deterministic (stable key/array ordering) so golden-file tests are reproducible, matching the existing `internal/report/render_test.go` (`TestRender_GoldenFiles`) pattern.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** Running `atcr report --format=sarif` against any `findings.json` (including an empty findings set) emits a single JSON document to stdout with `$schema`, `version: "2.1.0"`, a non-empty `runs[]` array, `runs[0].tool.driver.name == "atcr"`, `runs[0].tool.driver.rules[]`, and `runs[0].results[]` (empty array, not null, when there are no findings).
- **Measurable:** `internal/report/sarif_test.go` table-driven tests plus a `TestRender_GoldenFiles` entry against `internal/report/testdata/report.sarif.json` pass in CI; `json.Valid()` (or equivalent unmarshal round-trip) succeeds on every test-case output.
- **Achievable:** Follows the existing `renderJSON` pattern already proven in `internal/report/render.go` (nil-slice guard, `json.MarshalIndent` 2-space indent, trailing newline, error propagation) — no new library, no new subsystem.
- **Relevant:** Directly satisfies Plan Objective 1/2 ("Add SARIF 2.1.0 JSON as a new render target... Enable `atcr report --format=sarif` to produce schema-valid SARIF output") and is the load-bearing dependency every other story in this plan builds on.
- **Time-bound:** Deliverable within this sprint's first implementation phase, ahead of Stories 2-4 which extend this same `sarif.go` file.

## Acceptance Criteria Overview

1. `internal/report/render.go` gains a `FormatSarif = "sarif"` constant, and `ValidFormat()`, `Formats()`, and `Render()`'s switch all recognize it (routing to a new `renderSarif` call) — `atcr report --format=sarif` no longer errors as an unknown format, and `atcr report --format=bogus` still lists `sarif` in its supported-formats error message.
2. A new `internal/report/sarif.go` implements `renderSarif(w io.Writer, findings []reconcile.JSONFinding) error`, producing a top-level SARIF 2.1.0 document (`$schema`, `version`, `runs[]`, `tool.driver.name="atcr"`) with a `results[]` array (empty-but-present, never `null`, when `findings` is nil/empty) and a `rules[]` array containing one entry per distinct `reconcile.JSONFinding.Category` seen across the input, each with an `id`, `shortDescription.text`, and `fullDescription.text` (content sourced from the category value itself at this structural stage — full linkage semantics land in later stories).
3. `cmd/atcr/report.go`'s `--format` flag help text is updated to mention `sarif`, and `atcr report --format=sarif` is exercised by an automated test (unit and/or golden-file) confirming the CLI path produces the same output as calling `report.Render` directly — with a note that `internal/mcp/handlers.go`'s `handleReport` gains SARIF support automatically via the shared `report.Render()` call (no MCP-specific code change required, but worth a regression test or comment confirming parity).

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/25.0_sarif_output_integration/`_

## Technical Considerations

- **Implementation Notes:** Add `FormatSarif` next to the existing format constants (`internal/report/render.go:24-27`); extend `ValidFormat` (line 34-41) and `Formats()` (line 44); add a `case FormatSarif: return renderSarif(w, findings)` arm to `Render()` (line 48-63). New file `internal/report/sarif.go` defines the SARIF struct tree (`sarifLog`, `sarifRun`, `sarifTool`, `sarifDriver`, `sarifRule`, `sarifResult`, etc.) and `renderSarif`, mirroring `renderJSON`'s nil-slice guard and `json.MarshalIndent(..., "", "  ")` + trailing-newline convention (`internal/report/render.go:65-77`). Rule generation should iterate findings once, collecting distinct `Category` values in first-seen order (deterministic output) to build `rules[]`.
- **Integration Points:** `cmd/atcr/report.go` (flag help text only — the `--format` plumbing and `report.ValidFormat`/`report.Render` calls already generalize, no new wiring needed); `internal/mcp/handlers.go:handleReport` (no code change expected, but confirm/test parity since it shares `report.Render()`); `internal/report/render_test.go`'s `TestRender_GoldenFiles` gains a SARIF case with a fixture at `internal/report/testdata/report.sarif.json`.
- **Data Requirements:** Sole input is the existing `[]reconcile.JSONFinding` slice (`internal/reconcile/emit.go`) — no schema changes, no new persisted artifact. Output is a stdout/writer stream only, matching every other `render*` function's signature.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Hand-rolled SARIF struct tree drifts from the SARIF 2.1.0 spec (wrong field names/nesting), producing JSON that is technically valid but not spec-conformant | Medium | Schema-validate `renderSarif` output in tests using the already-vendored `google/jsonschema-go` against a local SARIF 2.1.0 schema fixture (per plan's documentation reference); cross-check top-level shape against a known-good CodeQL SARIF sample. |
| Scope creep into severity-mapping or line-anchoring logic (owned by Stories 2/3) causes rework or merge conflicts when those stories land | Low | Keep `renderSarif` in this story deliberately minimal — `level` and `region` fields may be present with placeholder/pass-through values, but the mapping rubric and fallback-coordinate logic are explicitly out of scope here and referenced as dependent follow-on stories. |
| `results[]` serializes as JSON `null` instead of `[]` when `findings` is empty, breaking strict SARIF consumers (GitHub/GitLab) that expect an array | Medium | Apply the same nil-slice guard `renderJSON` already uses (`if findings == nil { findings = []reconcile.JSONFinding{} }`) to the results slice inside `renderSarif`, and cover it with an explicit empty-findings test case. |

---

**Created:** July 14, 2026 04:11:53PM
**Status:** Draft - Awaiting Acceptance Criteria
