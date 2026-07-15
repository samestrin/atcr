# Sprint Design: SARIF Output Integration

**Created:** July 14, 2026 04:52:12PM
**Plan:** [Plan 25.0: SARIF Output Integration](plan.md)
**Plan Type:** ✨ Feature
**Status:** Design Complete

---

## Original User Request

> Support exporting ATCR review findings in standard SARIF (Static Analysis Results Interchange Format) so they feed GitHub Advanced Security's Code Scanning ("Security" tab) and GitLab CI's native SAST report widget — the two centralized, cross-repo security surfaces ATCR's existing PR-check/inline-comment integration (`atcr github`) does not reach.

**Referenced Resources:**

- [SARIF 2.1.0 Schema Reference](documentation/sarif-schema-reference.md)
  - **Summary:** Documents the OASIS SARIF 2.1.0 JSON schema and flags the direct conflict between the base spec (region optional for file-level results) and GitHub Code Scanning's actual rendering requirement (all four `region` fields mandatory to display).
  - **Key Points:** Base spec allows omitting `region`; GitHub's renderer silently drops results that omit it; resolved by synthesizing `1,1,1,1` fallback coordinates (Story 3).
- [GitHub Code Scanning SARIF Integration Constraints](documentation/github-code-scanning-integration.md)
  - **Summary:** Surfaces GitHub's `tool.driver.rules[]`/`ruleId` linkage requirement, not present in the original Proposed Solution.
  - **Key Points:** Every `results[].ruleId` must match a `rules[].id`; rule needs `shortDescription.text` and `fullDescription.text`; candidate design is one rule per distinct `Category`.
- [Schema-Validating SARIF Output with jsonschema-go](documentation/schema-validation-with-jsonschema-go.md)
  - **Summary:** Explains how to validate `renderSarif`'s output against a local SARIF 2.1.0 schema fixture using the already-vendored `google/jsonschema-go`.
  - **Key Points:** Requires adding `testdata/sarif-schema-2.1.0.json`; `Resolve`/`Validate` path is test-only, no production dependency added.
- [encoding/json Conventions for renderSarif](documentation/json-encoding-conventions.md)
  - **Summary:** Grounds the struct-tree + `json.MarshalIndent` convention `renderSarif` must follow, mirroring the existing `renderJSON`.
  - **Key Points:** 2-space indent, trailing newline, nil-slice guard so empty arrays never serialize as `null`.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** SARIF Output Integration
**Complexity:** 6/12 (MODERATE)
**Timeline:** 5 days
**Phases:** 4
**Pattern:** Item 1 (Foundation) → Item 2 (Severity & Anchoring) → Integration → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
Go struct tree JSON render pattern
SARIF static analysis output format
severity rubric reuse across renderers
GitHub Code Scanning SARIF display constraints
CLI render format enum extension
```

---

## Complexity Breakdown

- **Architecture:** 1/3 - Extends the existing `Format*`/`render*` sibling pattern (`render.go`) with one new format; no new architectural approach — `renderSarif` mirrors `renderJSON`'s struct-tree + `json.MarshalIndent` shape exactly.
- **Integration:** 1/3 - Touches 3 components (`internal/report`, `cmd/atcr`, `docs/`) but each touch is thin: one new file plus a 4-line dispatcher extension, a help-text string update, and documentation. No new runtime dependency, no new outbound API call — GitHub/GitLab consume the emitted file externally.
- **Story/Task & Test:** 3/3 - 4 user stories, 9 acceptance criteria, table-driven unit tests, a golden-file fixture, JSON-Schema conformance validation via `jsonschema-go`, and CLI+MCP parity regression tests.
- **Risk/Unknowns:** 1/3 - Severity mapping reuses the canonical `reconcile.SeverityRank`/`NormalizeSeverity` rubric with zero ambiguity. The one genuine design unknown (GitHub's region-required-for-display constraint) was already resolved during `/refine-epic` with an exact synthesized-coordinate design (`documentation/sarif-schema-reference.md`).

**Time Formula:** MODERATE (4-6 complexity) → 4-7 day range; skews toward the low end because the two hardest design questions (SARIF struct shape, GitHub's region-fallback requirement) were already resolved during `/refine-epic`, leaving mostly grounded implementation plus thorough test authoring.
**Calculation:** 5 days = Phase 1 Foundation (1.5d) + Phase 2 Severity & Anchoring (1.5d) + Phase 3 Integration & Docs (1d) + Phase 4 Validation (1d)

---

## Recommended Flags

**Adversarial:** true
**Gated:** false
**Recommendation strength:** false
**Suggested command:** `/create-sprint @.planning/plans/active/25.0_sarif_output_integration/ --adversarial`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12.

---

## Phase Structure

### Phase 1: Foundation — SARIF Document Structure (RGR)
**Duration:** 1.5 days
**Items:** AC 01-01 (Format Constant Registration), AC 01-02 (Base Document Structure), AC 01-03 (Rules Array / Category Linkage)
**Focus:** `internal/report/render.go` gains `FormatSarif = "sarif"`, extended `ValidFormat()`/`Formats()`, and a `case FormatSarif: return renderSarif(w, findings)` dispatch arm. New `internal/report/sarif.go` defines the SARIF 2.1.0 struct tree (`sarifLog`, `sarifRun`, `sarifTool`, `sarifDriver`, `sarifRule`, `sarifResult`) and the `renderSarif(w io.Writer, findings []reconcile.JSONFinding) error` entry point, applying the same nil-slice guard `renderJSON` uses to `results[]` and `rules[]` so both serialize as `[]` never `null`. Rule collection iterates findings once, deduping `Category` in first-seen order. Golden fixture `internal/report/testdata/report.sarif.json` established here (exercising ≥2 distinct categories) and extended by later phases.

### Phase 2: Severity & Anchoring (RGR)
**Duration:** 1.5 days
**Items:** AC 02-01 (Severity-to-SARIF-Level Mapping), AC 03-01 (Line-Level Anchoring), AC 03-02 (File-Level Fallback Anchoring)
**Focus:** `sarifLevel(severity string) string` in `sarif.go`, calling `reclib.NormalizeSeverity`/`reclib.SeverityRank` exclusively (CRITICAL/HIGH→`error`, MEDIUM→`warning`, LOW→`note`, unrecognized→`warning` fallback — never a local redefinition). `sarifLocation(f reconcile.JSONFinding) ...` builds `physicalLocation.artifactLocation.uri` from `f.File` unmodified; for `Line>0` sets `region.startLine=region.endLine=f.Line` with synthesized non-zero columns; for `Line<=0` (covering both `Line==0` and negative) synthesizes `region: {1,1,1,1}`. Both helpers wired into each `renderSarif` result. Golden fixture extended with at least one file-level (`Line<=0`) finding.

### Phase 3: Integration — CLI/MCP Parity & CI Documentation
**Duration:** 1 day
**Items:** AC 01-04 (CLI Flag Help Text and MCP Parity), Story 4 / AC 04-01 (GitHub Code Scanning Upload Example), AC 04-02 (GitLab CI SAST Widget Example)
**Focus:** `cmd/atcr/report.go`'s `--format` help text updated to mention `sarif` (no new flag wiring — `report.ValidFormat`/`report.Render` already generalize); CLI-vs-`report.Render` byte-identical parity test; `internal/mcp/handlers.go`'s `handleReport` regression test confirming SARIF parity with no code change. `docs/ci-integration.md` gains a new "SARIF Upload for Code Scanning" subsection beneath "Maintained PR Action": a fenced GitHub Actions YAML snippet (`atcr review && atcr reconcile && atcr report --format=sarif > results.sarif` → `codeql-action/upload-sarif@v3`) and a fenced `.gitlab-ci.yml` snippet (`artifacts: reports: sast: results.sarif`), each with an explicit sentence distinguishing this path from `atcr github`'s already-shipped PR check/inline-comment flow.

### Phase 4: Validation
**Duration:** 1 day
**Items:** Schema conformance, cross-cutting regression, manual smoke test
**Focus:** Validate `renderSarif` output against a local `testdata/sarif-schema-2.1.0.json` fixture via `google/jsonschema-go` (`Schema.UnmarshalJSON` → `Resolve` → `Validate`), test-only — no production dependency added. Full `go test ./...`, `golangci-lint run`, `go vet ./...`. `yamllint`/`markdown-link-check` on the new documentation. Manual upload smoke-test of real SARIF output to a scratch repo's Code Scanning tab before marking the plan's AC1 done (per plan.md's Risk Mitigation).

---

## Work Decomposition

### Story 1: SARIF Formatter Core (Priority: High, Effort: M)
Testable elements:
- AC 01-01 — `ValidFormat("sarif")==true`, `Formats()` includes `sarif`, `Render()` dispatches `FormatSarif`→`renderSarif`, unknown-format error lists `sarif`. **Type:** Unit.
- AC 01-02 — Top-level document shape (`$schema`, `version:"2.1.0"`, non-empty `runs[]`, `tool.driver.name=="atcr"`), `results[]` present-but-empty (never `null`) for nil/empty findings, JSON round-trip validity, deterministic byte-identical output across repeated calls. **Type:** Unit + Golden-File.
- AC 01-03 — `rules[]` has one entry per distinct `Category` in first-seen order; `id==shortDescription.text==Category`; `fullDescription.text` is a synthesized category-generic sentence (never sourced from `Problem`/`Fix`); `results[].ruleId` matches its rule's `id`; empty-`Category` and empty-findings edge cases. **Type:** Unit.

### Story 2: Severity-to-SARIF-Level Mapping (Priority: High, Effort: S, depends on Story 1)
Testable elements:
- AC 02-01 — `sarifLevel(severity string) string` maps CRITICAL/HIGH→`error`, MEDIUM→`warning`, LOW→`note`, via `reclib.NormalizeSeverity`/`reclib.SeverityRank` exclusively; case/whitespace-insensitive; empty/unrecognized input falls back to `warning` (never `""`, never `"none"`, never panics); `renderSarif` wires it into every result's `level` with no second comparison site in `sarif.go`. **Type:** Unit.

### Story 3: SARIF Line/File Anchoring (Priority: High, Effort: S, depends on Story 1)
Testable elements:
- AC 03-01 — `artifactLocation.uri==f.File` unmodified for all path shapes; `Line>0` → `region.startLine==region.endLine==f.Line` with non-zero columns; deterministic across repeated calls; `Line==1` is a real line, not mistaken for the fallback. **Type:** Unit.
- AC 03-02 — `Line<=0` (covering `Line==0` and negative, including large-magnitude negative) → synthesized `region:{1,1,1,1}`; `artifactLocation.uri` unaffected by the fallback; `region` never omitted/partially populated; boundary `Line==1` correctly routes to the normal path, not the fallback. **Type:** Unit.

### Story 4: SARIF CI Integration Documentation (Priority: Medium, Effort: S, depends on Story 1; benefits from Stories 2-3 complete)
Testable elements:
- AC 04-01 — `docs/ci-integration.md` gains a GitHub Actions snippet (checkout `fetch-depth:0`, atcr pipeline, `codeql-action/upload-sarif@v3`) with an explicit distinction sentence linking to `docs/github-action.md`. **Type:** E2E (Manual/Static: YAML lint + doc review).
- AC 04-02 — Same subsection gains a `.gitlab-ci.yml` snippet (`artifacts: reports: sast: results.sarif`) using GitLab-native terminology only, no GitHub-only bleed. **Type:** E2E (Manual/Static: YAML lint + doc review).

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** `internal/report/` — test files co-located with source (`*_test.go` in the same package directory), per the project's Go testing convention.

**Test File Placement Examples:**
- `internal/report/sarif_test.go` (new) — table-driven unit tests for `renderSarif`, `sarifLevel`, `sarifLocation`, following the `t.Run` subtest convention in `internal/report/render_test.go`.
- `internal/report/testdata/report.sarif.json` (new) — golden fixture driven by the existing `sample()` two-finding fixture, extended with a file-level (`Line<=0`) case.
- `internal/report/testdata/sarif-schema-2.1.0.json` (new) — local SARIF 2.1.0 JSON Schema fixture for `jsonschema-go` conformance validation.
- `internal/report/render_test.go` (modify) — extend `goldenCases` with `{"sarif", FormatSarif, "report.sarif.json"}`; extend `TestValidFormat`.
- `cmd/atcr/report_test.go` (modify) — CLI-vs-`report.Render` byte-identical parity test for `--format=sarif`.
- `internal/mcp/handlers_test.go` (modify) — `handleReport` SARIF parity regression test.
- `docs/ci-integration.md` (modify) — new subsection, verified via `yamllint`/`markdown-link-check`, not `go test`.

**Unit/Integration/E2E:**
- **Unit (7 ACs):** 01-01, 01-02, 01-03, 02-01, 03-01, 03-02 plus the MCP-handler half of 01-04 — `go test ./internal/report/... ./internal/mcp/...`.
- **Integration (1 AC):** 01-04's CLI command-execution half — `go test ./cmd/atcr/...`.
- **E2E (0 automated / 2 manual-static):** Story 4's AC 04-01/04-02 are documentation-only, verified via `yamllint` + `markdown-link-check` + manual review, not automated `go test` coverage.
- **Coverage:** `go test -coverprofile=coverage.out ./...`, project baseline 80%.

**Test Environment Status:**
- Framework: Go stdlib `testing` + `testify/assert` — READY (already established throughout `internal/report`; the generic `discover_tests` scan reported `UNKNOWN` since it does not recognize Go conventions, but direct inspection of `internal/report/render_test.go` confirms the pattern).
- Execution: `go test ./...` — READY, existing CI already runs this target.
- Coverage Tools: `go test -coverprofile=coverage.out ./...` — READY, 80% baseline enforced project-wide.

---

## Architecture

**Primitives:**
- `reconcile.JSONFinding` (`internal/reconcile/emit.go`) — existing, sole input to `renderSarif`; no schema change.
- New SARIF struct tree in `internal/report/sarif.go`: `sarifLog`, `sarifRun`, `sarifTool`, `sarifDriver`, `sarifRule`, `sarifResult`, `sarifLocation`/`sarifPhysicalLocation`/`sarifRegion`.

**Module Boundaries:**
- `renderSarif(w io.Writer, findings []reconcile.JSONFinding) error` is the sole exported-shape entry point, matching the signature of every existing `render*` function (`renderMarkdown`, `renderJSON`, `renderChecklist`).
- `sarifLevel` and `sarifLocation` are internal, independently-testable helpers within the same file/package — not exposed outside `internal/report`.

**External Dependencies:**
- No new runtime dependency — stdlib `encoding/json` only, mirroring `renderJSON`.
- `github.com/google/jsonschema-go` (already in `go.mod`) is used in tests only (schema conformance validation), never in the production render path.

**Replaceability:**
- `sarif.go` can be swapped wholesale (e.g. for `owenrumney/go-sarif/v2`, documented as an optional future alternative) without touching `render.go` beyond its single existing dispatch line, since `renderSarif`'s signature is identical to every sibling renderer.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| SARIF JSON serialization of attacker-influenced free text (`Category`, `File`, `Problem`→`message.text`) | `internal/report/sarif.go` `renderSarif` | Malicious/unicode content in reviewer-produced `Category`/`Problem` attempting JSON-structure injection or control-character breakage | `encoding/json`'s standard escaping only — no raw string concatenation into the output; unlike the markdown renderer, no HTML-escaping needed since the sink is JSON, not markdown/HTML |
| `--output` file-write path for SARIF (`cmd/atcr/report.go`) | CLI `--format=sarif --output <path>` | Symlink/path traversal to write outside the intended directory | Existing `resolveOutputPath`/`validation.FilePath` guard already applies uniformly regardless of `--format` value; no SARIF-specific bypass introduced |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| `renderSarif` over the findings slice | Tens to low hundreds of findings per review (typical); low thousands worst case | O(n) single-pass render, no regression vs. `renderJSON` | Single pass builds `results[]`; single pass with a seen-set + ordered slice collects distinct categories for `rules[]` — no O(n²) dedup scan |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Empty/nil findings | `findings == nil` or `[]` | `results[]` and `rules[]` serialize as `[]`, never `null` |
| File-level findings | `Line==0`, `Line<0`, large-magnitude negative `Line` | Synthesized `region:{1,1,1,1}`; `artifactLocation.uri` unaffected |
| Line boundary | `Line==1` (real) vs. the `1,1,1,1` fallback | Coincidentally identical output from two distinct code paths — tested as separate table rows to catch a future `<=`-vs-`<` regression |
| Category edge values | Empty `Category` string, repeated categories, case-variant categories (`"Security"` vs `"security"`) | Empty string is one distinct category; repeats dedupe to one rule; case-variants remain distinct rules (no normalization, documented as-is) |
| Severity edge values | Lowercase, whitespace-padded, empty, unrecognized token | Normalized via `reconcile.NormalizeSeverity`; unrecognized falls back to `"warning"`, never empty or panicking |

### Defensive Measures Required

- **Input Validation:** `--format` validated against the closed `ValidFormat` enum before any I/O (existing TD-003 pattern, unchanged); `Category`/`File`/`Problem` free text is safely escaped via `encoding/json` alone — no sanitization/normalization performed or required at this layer.
- **Error Handling:** `renderSarif` propagates writer/marshal errors unchanged, matching `renderJSON`'s `_, err = w.Write(...); return err` contract — no panics on nil, empty, or malformed input.
- **Logging/Audit:** N/A — no new logging surface; existing CLI error paths (`usageError`, exit codes) are unchanged by adding `sarif` as a format value.
- **Rate Limiting:** N/A — local CLI/library render, no network surface, no service boundary.
- **Graceful Degradation:** Unrecognized severity falls back to a visible `"warning"` level rather than dropping the finding or failing the whole render; a missing/invalid `Line` falls back to a synthesized, visible `1,1,1,1` region rather than omitting the finding from GitHub's Security tab.

---

## Risks

**Technical:**
- Hand-rolled SARIF struct tree drifts from the SARIF 2.1.0 spec (wrong field name/nesting) → schema-validate `renderSarif` output in tests via `google/jsonschema-go` against a local schema fixture, plus a manual upload smoke-test to a scratch repo's Code Scanning tab before marking the plan's AC1 done.
- A future edit reimplements severity comparison locally inside `sarif.go`, silently creating a second rubric copy (the TD-0052 failure mode) → code review confirms `sarifLevel` is the only severity-comparison site; a test asserts `renderSarif`'s per-result `level` matches `sarifLevel`'s own output.
- Fallback coordinates (`1,1,1,1`) collide across multiple distinct file-level findings in the same file → accepted trade-off (stacked display beats no display); documented inline, no further mitigation needed.

**TDD-Specific:**
- Golden-file test (`TestRender_GoldenFiles` SARIF case) locks in output shape early in Phase 1 before severity/anchoring logic lands in Phase 2 — regenerate the fixture (`go test ./internal/report -update`) at the end of each phase rather than hand-editing it, so the fixture always reflects real `renderSarif` output.
- `Line<=0` boundary must be tested with `Line==0` AND a distinct `Line<0` row (not collapsed into one case) — the single highest-value regression test in this sprint, since a `Line==0`-only check would silently mis-handle negative values.
- CLI/MCP parity tests (AC 01-04) must call `report.Render` directly and compare bytes, not just assert "no error" — a subtle formatting divergence between the CLI path and the library path would otherwise pass silently.

---

**Next:** `/create-sprint @.planning/plans/active/25.0_sarif_output_integration/`
