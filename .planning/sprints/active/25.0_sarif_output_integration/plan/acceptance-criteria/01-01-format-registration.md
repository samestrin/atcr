# Acceptance Criteria: SARIF Format Constant Registration

**Related User Story:** [01: SARIF Formatter Core](../user-stories/01-sarif-formatter-core.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package function/constant (`internal/report/render.go`) | Extends the existing format-enum pattern (`FormatMarkdown`, `FormatJSON`, `FormatChecklist`) |
| Test Framework | `go test` (table-driven, `testify/assert`) | Mirrors `TestValidFormat` in `internal/report/render_test.go:51-56` |
| Key Dependencies | none (stdlib only) | No new dependency; `renderSarif` (AC 01-02) is called from the new switch arm |

### Related Files (from codebase-discovery.json)

- [`internal/report/render.go`](../../../../../internal/report/render.go) — modify:
  - Add `FormatSarif = "sarif"` constant alongside the existing `FormatMarkdown`/`FormatJSON`/`FormatChecklist` block ([`internal/report/render.go:23-27`](../../../../../internal/report/render.go)).
  - Extend `ValidFormat()` to accept `"sarif"` ([`internal/report/render.go:34-41`](../../../../../internal/report/render.go)).
  - Extend `Formats()` to include `sarif` in the supported-formats list ([`internal/report/render.go:44`](../../../../../internal/report/render.go)).
  - Add a `case FormatSarif: return renderSarif(w, findings)` arm to `Render()`'s switch ([`internal/report/render.go:48-63`](../../../../../internal/report/render.go)).
- [`internal/report/render_test.go`](../../../../../internal/report/render_test.go) — modify:
  - Extend `TestValidFormat` ([`internal/report/render_test.go:51-56`](../../../../../internal/report/render_test.go)) to assert `ValidFormat("sarif")` is `true` and `ValidFormat("SARIF")` is `false`.
  - Add/extend coverage asserting `Formats()` includes `"sarif"` and that `Render()` dispatches `FormatSarif` to `renderSarif` without error.
- [`internal/report/sarif.go`](../../../../../internal/report/sarif.go) — create: defines `renderSarif(w io.Writer, findings []reconcile.JSONFinding) error` with the correct signature so this AC's `Render()` switch arm can call it; the SARIF document shape is owned by AC 01-02 and AC 01-03.

## Happy Path Scenarios
**Scenario 1: sarif is accepted as a valid format**
- **Given** the `report` package's format enum
- **When** `ValidFormat("sarif")` is called
- **Then** it returns `true`

**Scenario 2: Render dispatches sarif to renderSarif**
- **Given** a non-empty `[]reconcile.JSONFinding` slice and `format = "sarif"`
- **When** `Render(w, findings, "sarif")` is called
- **Then** it returns `nil` error and `w` contains the `renderSarif` output (no `"unknown format"` error path is taken)

**Scenario 3: Formats() enumerates sarif for error messages**
- **Given** the `Formats()` helper
- **When** it is called
- **Then** the returned string contains `"sarif"` alongside `"md"`, `"json"`, and `"checklist"`

## Edge Cases
**Edge Case 1: format string is case-sensitive**
- **Given** `format = "SARIF"` (uppercase)
- **When** `ValidFormat("SARIF")` is called
- **Then** it returns `false` — the enum matches the existing case-sensitive convention already used for `"md"`/`"json"`/`"checklist"`; no new case-normalization behavior is introduced by this story

**Edge Case 2: unknown format still lists sarif in the error**
- **Given** `format = "bogus"`
- **When** `Render(w, findings, "bogus")` is called (or `atcr report --format=bogus` at the CLI)
- **Then** an error is returned whose message includes the full supported-formats list, now including `sarif`

## Error Conditions
**Error Scenario 1: unknown format defensive backstop**
- **Given** an invalid format string reaches `Render()` directly (bypassing CLI-level `ValidFormat` pre-check)
- **When** `Render(w, findings, "notaformat")` is called
- Error message: `unknown format "notaformat": supported formats are md, json, checklist, sarif`
- Go error type: standard `error` from `fmt.Errorf`, no panic, no partial write to `w`

## Performance Requirements
- **Response Time:** Format validation and dispatch add O(1) overhead — a single string comparison in the switch — no measurable regression versus the existing three-format dispatch.
- **Throughput:** N/A (single-process CLI invocation, not a service).

## Security Considerations
- **Authentication/Authorization:** N/A — local CLI/library call, no network or auth boundary.
- **Input Validation:** `format` is a plain string compared against a closed enum (`switch`); no injection surface. Unknown values are rejected with a bounded, static-shaped error message (the invalid value is interpolated but not executed or used to construct a path).

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Reuses the existing `sample()` fixture in `internal/report/render_test.go` (two findings, CRITICAL/security and LOW/style) — no new fixture needed for this AC.
**Mock/Stub Requirements:** None — `Render`, `ValidFormat`, and `Formats` are pure functions over in-memory data; no I/O mocking required.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/report/...`)
- [ ] No linting errors (`golangci-lint run` or project-configured linter)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `ValidFormat("sarif")` returns `true` and `ValidFormat("SARIF")` returns `false`
- [ ] `Formats()` includes `"sarif"` in its output string
- [ ] `Render(w, findings, FormatSarif)` returns `nil` and dispatches to `renderSarif`
- [ ] `Render(w, findings, "bogus")` error message lists `sarif` among supported formats

**Manual Review:**
- [ ] Code reviewed and approved
