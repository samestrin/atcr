# Acceptance Criteria: `docs/code-review-backend.md` `--output-dir` Contract Accuracy

**Related User Story:** [4: Documentation Accuracy Pass](../user-stories/04-documentation-accuracy-pass.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation | `docs/code-review-backend.md` — `--output-dir` contract page for external backend integrators |
| Test Framework | Manual cross-check against User Story 2's backward-compatibility test assertions and current CLI `--output-dir` flag behavior | No automated doc-linting harness in repo; ground truth is the live test + code, not memory of prior doc content |
| Key Dependencies | User Story 2's new `--output-dir` backward-compatibility test (source of ground truth for expected output tree/behavior) | This story does not write the test; it verifies the doc against it |

## Related Files
- `docs/code-review-backend.md` - verify, correct only if drifted: "Output tree" code block (`docs/code-review-backend.md:44-64`) and "Behavioral notes for callers" section (`docs/code-review-backend.md:101-116`)
- `cmd/atcr/main.go` - read-only ground truth: `--output-dir` flag registration and behavior on the `review`/`reconcile`/`report` commands
- User Story 2's backward-compatibility test file (path TBD at this story's execution time, created by User Story 2) - read-only ground truth: asserts the actual `--output-dir` output tree shape and behavior this doc must match

## Happy Path Scenarios
**Scenario 1: "Output tree" code block matches actual `--output-dir` output**
- **Given** `docs/code-review-backend.md:44-64` documents the exact directory tree written under `${OUT_DIR}` (`manifest.json`, `payload/`, `sources/pool/{raw,findings.txt,summary.json}`, `reconciled/{findings.txt,findings.json,report.md,summary.json,ambiguous.json,disagreements.json}`)
- **When** this tree is cross-checked against User Story 2's backward-compatibility test assertions and current `atcr review --output-dir`/`atcr reconcile` behavior
- **Then** every listed path is confirmed present and correctly named (no missing, renamed, or extra entries), and no edit is required if already accurate

**Scenario 2: "Behavioral notes for callers" section matches current CLI semantics**
- **Given** `docs/code-review-backend.md:101-116` documents three behavioral notes: partial runs are normal, finding counts differ from other backends, and review scope is diff-touched-lines-only (temporary, pending a future context-injection epic)
- **When** each note is checked against current `atcr review`/`atcr reconcile` behavior and User Story 2's test
- **Then** all three notes are confirmed still accurate (or corrected if the underlying behavior changed)

## Edge Cases
**Edge Case 1: `--output-dir` mutual-exclusivity or collision behavior changed**
- **Given** `docs/code-review-backend.md:24-26` states `--output-dir` "is mutually exclusive with `--id`" and elsewhere (README.md:97, referenced for consistency) documents `--force`/`--resume` collision handling
- **When** checked against current flag validation in `cmd/atcr/main.go`
- **Then** confirm the mutual-exclusivity statement still holds, correcting only if the flag contract changed

**Edge Case 2: `summary.json` field list has drifted**
- **Given** `docs/code-review-backend.md:87-99` enumerates `summary.json` fields (`total_findings`, `sources_scanned`, `per_source_counts`, `partial`, `clusters_collapsed`, `severity_disagreements`, `authority_promoted`)
- **When** cross-checked against the actual struct/JSON emitted by the reconcile command
- **Then** any added, removed, or renamed field is corrected; fields already correct are left untouched

## Error Conditions
**Error Scenario 1: Doc claims a guarantee the code does not actually provide**
- **Given** the "Behavioral notes for callers" section makes a specific promise (e.g., "partial runs are normal" and callers should treat `partial=true` as success-with-fewer-sources)
- **When** User Story 2's test or current code shows the guarantee does not hold (e.g., a partial run under some condition now fails instead of degrading)
- **Then** the doc is corrected to state the actual behavior — a doc must never assert a contract the code does not honor, since this page is external integrators' source of truth
- Error message: N/A (not a runtime error path; this is a documentation-accuracy guard)
- HTTP status / error code: N/A (documentation-only story, no service boundary)

## Performance Requirements
- **Response Time:** N/A — static documentation verification, not a runtime code path
- **Throughput:** N/A — single-pass manual cross-check of one file against test assertions and CLI behavior

## Security Considerations
- **Authentication/Authorization:** N/A — no auth surface; documentation-only change
- **Input Validation:** N/A — no user input; verification confirms doc prose accuracy against code/test behavior, not runtime validation logic

## Test Implementation Guidance
**Test Type:** MANUAL (documentation cross-check against an existing automated test's assertions; no new test is authored by this AC)
**Test Data Requirements:** Current `docs/code-review-backend.md`, User Story 2's backward-compatibility test source, and `cmd/atcr/main.go`'s `--output-dir` flag handling
**Mock/Stub Requirements:** None — direct comparison of doc prose against test assertions and code; optionally run User Story 2's test locally (`go test ./cmd/atcr/...`) to confirm the tree shape empirically before comparing to the doc

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (including User Story 2's backward-compatibility test — `go test ./...` green)
- [ ] No linting errors (repo has no markdown linter configured; N/A)
- [ ] Build succeeds (`go build -o bin/atcr ./cmd/atcr` — confirms doc edits did not touch code)

**Story-Specific:**
- [ ] "Output tree" code block (`docs/code-review-backend.md:44-64`) verified against User Story 2's test and current `--output-dir` behavior
- [ ] "Behavioral notes for callers" section (`docs/code-review-backend.md:101-116`) verified against current CLI semantics
- [ ] Any correction made is limited to the specific drifted line(s); no unrelated restructuring or rewording

**Manual Review:**
- [ ] Code reviewed and approved
