# Acceptance Criteria: Report Rendering with Verification Sections

**Related User Story:** [[06]: Report Updates & Documentation](../user-stories/06-report-updates-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Report Renderer | Go Package (`internal/report`) | `Render()` in `render.go` extended with verification-aware sections |
| Test Framework | `go test` + `testify` (assert/require) | Golden file comparison with `-update` flag |
| Key Dependencies | `internal/reconcile` (JSONFinding, Verification struct) | Verification block is `*Verification` (nil-safe) |

## Related Files
- `internal/report/render.go` - modify: add Skeptic section rendering per verified finding, collapsed Refuted section, VERIFIED tier label
- `internal/report/render_test.go` - modify: add `TestRenderWithVerification` golden file test
- `internal/report/testdata/findings-with-verification.json` - create: input fixture with all verdict types (confirmed, refuted, unverifiable)
- `internal/report/testdata/report-v2.md` - create: golden file for v2 report output
- `internal/reconcile/emit.go` - read: `Verification` struct definition (Verdict, Skeptic, Notes fields)

## Happy Path Scenarios
**Scenario 1: Verified finding renders Skeptic section in panel**
- **Given** a finding with `Verification.Verdict = "confirmed"`, `Verification.Skeptic = "otto"`, `Verification.Notes = "reproduced via unit test"`
- **When** the report is rendered in markdown format
- **Then** the finding entry includes a Skeptic section showing: skeptic name ("otto"), model (from registry lookup or Verification struct), verdict ("confirmed"), and reasoning ("reproduced via unit test")

**Scenario 2: Refuted findings appear in collapsed section at bottom**
- **Given** 2 findings where `Verification.Verdict = "refuted"` with skeptic reasoning
- **When** the report is rendered in markdown format
- **Then** a collapsed `<details><summary>Refuted Findings (2)</summary>...</details>` section appears after the main findings list, containing each refuted finding's file, line, problem, original confidence, skeptic name, and reasoning

**Scenario 3: VERIFIED confidence tier rendered distinctly**
- **Given** a finding with v1 confidence `HIGH` and `Verification.Verdict = "confirmed"` → v2 confidence `VERIFIED`
- **When** the report is rendered
- **Then** the finding shows `[VERIFIED]` (or equivalent distinct marker) instead of `[HIGH]`, and the summary grid includes a VERIFIED conf column or row annotation

**Scenario 4: Unverifiable finding retains original tier with annotation**
- **Given** a finding with v1 confidence `MEDIUM` and `Verification.Verdict = "unverifiable"`
- **When** the report is rendered
- **Then** the finding retains its `MEDIUM` confidence display with an annotation indicating the skeptic could not verify it

**Scenario 5: Golden file test passes**
- **Given** the fixture `findings-with-verification.json` containing 4+ findings covering all verdict types
- **When** `TestRenderWithVerification` runs comparing rendered output against `report-v2.md` golden file
- **Then** the test passes with byte-identical output

## Edge Cases
**Edge Case 1: No refuted findings**
- **Given** all verified findings have verdict "confirmed" or "unverifiable"
- **When** the report is rendered
- **Then** the collapsed Refuted section is omitted entirely (no empty `<details>` block)

**Edge Case 2: Mix of verified and unverified findings**
- **Given** 3 findings: 1 with `Verification.Verdict = "confirmed"`, 1 without any `Verification` block, 1 with `Verification.Verdict = "refuted"`
- **When** the report is rendered
- **Then** the confirmed finding shows the Skeptic section, the unverified finding renders without any skeptic information, and the refuted finding appears only in the collapsed Refuted section

**Edge Case 3: Empty verification notes**
- **Given** a finding with `Verification.Verdict = "confirmed"`, `Verification.Skeptic = "otto"`, `Verification.Notes = ""`
- **When** the report is rendered
- **Then** the Skeptic section renders without a reasoning line (or shows "(no reasoning provided)")

**Edge Case 4: JSON format round-trips verification block**
- **Given** findings with verification blocks rendered in JSON format
- **When** the JSON output is parsed back as `[]reconcile.JSONFinding`
- **Then** the `Verification` field is preserved with all original values

## Error Conditions
**Error Scenario 1: Unknown verdict value in verification block**
- Error message: N/A (renderer does not validate; writer responsibility per emit.go contract)
- Behavior: Unknown verdict values render as-is in the Skeptic section (the renderer is display-only; validation is the writer's responsibility per `emit.go` comments)

## Performance Requirements
- **Response Time:** Rendering a report with 100 findings (50 verified) completes in < 50ms
- **Throughput:** No additional allocations beyond the existing `bytes.Buffer` pattern

## Security Considerations
- **Input Validation:** Skeptic name, notes, and reasoning are free text — must be HTML-escaped and newline-flattened via the existing `esc()` / `escTrunc()` functions before rendering in markdown
- **Injection Prevention:** The `<details>` / `<summary>` HTML tags are static template text, not user-controlled; all dynamic content inside them goes through `esc()`

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** `findings-with-verification.json` with 4+ findings covering: (1) v1=HIGH, verdict=confirmed → VERIFIED, (2) v1=HIGH, verdict=refuted → LOW (refuted), (3) v1=MEDIUM, verdict=unverifiable → MEDIUM, (4) v1=LOW, no verification block → LOW
**Mock/Stub Requirements:** None — uses existing `Render()` function with struct input

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/report/...`)
- [ ] No linting errors (`go vet ./internal/report/...`)
- [ ] Build succeeds (`go build ./...`)
- [ ] Coverage >= 90% on new rendering code paths

**Story-Specific:**
- [ ] Skeptic section rendered for each verified finding showing skeptic name, model, verdict, and reasoning
- [ ] Collapsed Refuted section at bottom with `<details>`/`<summary>` toggle listing all refuted findings
- [ ] VERIFIED tier rendered distinctly from v1 tiers (HIGH/MEDIUM/LOW)
- [ ] `TestRenderWithVerification` golden file test passes
- [ ] All free text in new sections is HTML-escaped and newline-flattened

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Report renders correctly in GitHub markdown preview (details/summary supported)
