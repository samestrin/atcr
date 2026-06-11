# Acceptance Criteria: Report Rendering

**Related User Story:** [01: CLI Review Workflow](../user-stories/01-cli-review-workflow.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Renderer | Go text/template | markdown, json, checklist formats |
| Output | Go os package | file writing, stdout |
| Test Framework | testify | golden file comparisons |

## Related Files
- `internal/report/renderer.go` - create: multi-format report rendering (md, json, checklist)
- `internal/report/renderer_test.go` - create: tests with golden files for each format
- `cmd/atcr/report.go` - modify: integrate renderer into report command with --format flag

## Happy Path Scenarios

**Scenario 1: Markdown report rendered**
- **Given** reconciled data in `reconciled/findings.json` with 5 findings
- **When** user runs `atcr report --format md`
- **Then** human-readable markdown report generated with sections: Summary, Findings (grouped by severity), Confidence Scores

**Scenario 2: JSON report rendered**
- **Given** reconciled data available
- **When** user runs `atcr report --format json`
- **Then** valid JSON output with all finding fields including confidence scores

**Scenario 3: Checklist report rendered**
- **Given** reconciled data available
- **When** user runs `atcr report --format checklist`
- **Then** output is markdown checklist with `- [ ]` items for each finding, suitable for copy-paste into PR comments

**Scenario 4: Report written to file**
- **Given** `--output report.md` flag specified
- **When** report renders
- **Then** output written to `report.md` file instead of stdout

**Scenario 5: Default format is markdown**
- **Given** no `--format` flag specified
- **When** user runs `atcr report`
- **Then** markdown format used by default

## Edge Cases

**Edge Case 1: Zero findings report**
- **Given** reconciled findings.json contains empty findings array
- **When** report renders
- **Then** report contains "No findings" message with summary showing 0 counts per severity

**Edge Case 2: Very long finding descriptions**
- **Given** finding PROBLEM text exceeds 500 characters
- **When** report renders to markdown
- **Then** text truncated with `...` suffix and full text available in JSON output

**Edge Case 3: Special characters in file paths**
- **Given** finding references file with unicode characters `src/café/main.go`
- **When** report renders
- **Then** file path displayed correctly in all output formats

## Error Conditions

**Error Scenario 1: No reconciled data found**
- Error message: "no reconciled data found: run 'atcr reconcile' first"
- Exit code: 1

**Error Scenario 2: Invalid format specified**
- Error message: "unknown format 'xml': supported formats are md, json, checklist"
- Exit code: 1

**Error Scenario 3: Output file not writable**
- Error message: "failed to write report to 'report.md': permission denied"
- Exit code: 1

## Performance Requirements
- **Response Time:** Report rendering completes in <100ms for 50 findings
- **Throughput:** N/A (single output generation)

## Security Considerations
- **Input Validation:** Finding text sanitized for markdown injection (no raw HTML passthrough in md format)
- **Output Safety:** JSON output uses encoding/json for proper escaping

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Golden files for each format (md, json, checklist); sample reconciled findings.json with varying severity/confidence
**Mock/Stub Requirements:** No external mocks needed; use golden file comparison for output validation

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Three formats render correctly: md, json, checklist
- [ ] Default format is markdown when --format not specified
- [ ] --output flag writes to file; default writes to stdout
- [ ] Zero-findings case renders gracefully
- [ ] Golden file tests pass for each format

**Manual Review:**
- [ ] Code reviewed and approved
