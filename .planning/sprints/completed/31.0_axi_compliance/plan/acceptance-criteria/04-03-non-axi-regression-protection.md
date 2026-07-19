# Acceptance Criteria: Non-AXI `review`/`resume` Behavior Remains Unchanged

**Related User Story:** [04: AXI Stderr Isolation and Escape-Sequence Guarantee](../user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI command (cobra) regression test suite | `cmd/atcr` binary |
| Test Framework | `go test` / `testify` (`assert`, `require`) | Reuses/extends existing `cmd/atcr/review_test.go`, `cmd/atcr/resume_test.go` coverage |
| Key Dependencies | None new — this AC only verifies the pre-existing non-`--axi` contract is undisturbed by AC 04-01's gating changes | |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/review.go` - modify (verify, not new behavior): confirm all six gated writes (lines 433, 436, 551, 573, 591, 602) still produce their exact existing text/format when `--axi` is not passed.
- `cmd/atcr/resume.go` - modify (verify, not new behavior): confirm all five gated writes (lines 153, 170, 188, 195, 259) still produce their exact existing text/format when `--axi` is not passed.
- `cmd/atcr/review_summary.go` - reference: `writeReviewSummary` (line 80) must retain its exact current output format (`"Total elapsed: %.1fs\n"`, `"Agents: %d/%d succeeded, %d failed, %d timed out\n"`, `"API calls: %d\n"`, `"Findings: %d%s\n"`) for both non-`--axi` callers.
- `cmd/atcr/review_test.go` - modify: existing non-`--axi` test assertions must continue to pass unmodified (byte-for-byte stdout comparison where such assertions already exist).
- `cmd/atcr/resume_test.go` - modify: existing non-`--axi` test assertions must continue to pass unmodified.

## Happy Path Scenarios
**Scenario 1: Plain `atcr review` output is byte-identical to pre-story behavior**
- **Given** the AXI-mode gating from AC 04-01 has been applied to `cmd/atcr/review.go`
- **When** `atcr review` is run without `--axi` against a fixture producing a known result
- **Then** captured stdout matches the exact pre-change format: `"review %s: %d/%d agents succeeded (%s)\n"` followed by the `writeReviewSummary` block and, where applicable, `"reconciled %d finding(s)\n"` / `"verified %d finding(s)..."` / `"debated %d item(s)..."` lines, unchanged in wording and ordering

**Scenario 2: Plain `atcr resume` output is byte-identical to pre-story behavior**
- **Given** the AXI-mode gating from AC 04-01 has been applied to `cmd/atcr/resume.go`
- **When** `atcr resume` is run without `--axi` on a review with pending agents
- **Then** captured stdout matches the exact pre-change format, including `"resuming review %s: %d completed, %d pending (%s)\n"` and the subsequent summary/reconcile lines

**Scenario 3: `AllComplete()` resume without `--axi`**
- **Given** `atcr resume` (no `--axi`) is run against a review where all agents already completed
- **When** the `AllComplete()` branch fires
- **Then** `"All configured agents already completed. Re-running reconciliation...\n"` is still written to stdout exactly as before

## Edge Cases
**Edge Case 1: Existing golden/snapshot-style assertions**
- **Given** any pre-existing test in `cmd/atcr/review_test.go` or `cmd/atcr/resume_test.go` asserts on exact stdout content for a non-`--axi` invocation
- **When** the test suite is run after AC 04-01's gating changes
- **Then** that test passes without modification — if it required a change to keep passing, the gating logic (not the test) is at fault and must be fixed

**Edge Case 2: Mixed-flag invocation with `--axi` unset but other flags present**
- **Given** `atcr review --verify --debate` (no `--axi`) is run
- **When** the review completes
- **Then** all human-oriented lines (progress, summary, reconcile, verify, debate) are written to stdout exactly as they were before this story, confirming the gating condition defaults to "human mode" when AXI is not requested

## Error Conditions
**Error Scenario 1: Non-`--axi` all-agents-failed review**
- **Given** every agent fails during a plain `atcr review` (no `--axi`)
- **When** the command returns its exit-1 error
- **Then** the pre-existing `"review %s: %d/%d agents succeeded (%s)\n"` and `writeReviewSummary` lines are still written to stdout before the error return, exactly as before this story
- Error message: unchanged
- HTTP status / error code: CLI exit code `1` (unchanged)

**Error Scenario 2: Non-`--axi` reconcile failure**
- **Given** `RunReconcile` fails after fan-out during a plain `atcr review --fail-on high` (no `--axi`)
- **When** the command returns its exit-2 usage error
- **Then** stdout behavior up to the failure point is unchanged from pre-story behavior
- Error message: unchanged (`usageError()` wrapping)
- HTTP status / error code: CLI exit code `2` (unchanged)

## Performance Requirements
- **Response Time:** No measurable regression — the AXI-mode check added in AC 04-01 is a single branch; the non-`--axi` (default/false) path executes the exact same write calls that existed before this story.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** No change — this AC verifies absence of regression, not new security surface.
- **Input Validation:** N/A — this AC covers output-format stability, not input handling.

## Test Implementation Guidance
**Test Type:** UNIT/INTEGRATION regression suite — re-run and, where needed, extend the existing `cmd/atcr/review_test.go` / `cmd/atcr/resume_test.go` non-`--axi` assertions after AC 04-01's changes land; add explicit stdout-content assertions for any write site (review.go:433/436/551/573/591/602, resume.go:153/170/188/195/259) that previously lacked direct test coverage, so a future change cannot silently alter non-`--axi` output without a test failure.
**Test Data Requirements:** Reuses the same fixture set as AC 04-01 (clean review, `AllComplete()` resume, all-agents-failed review, `--verify`/`--debate` chained review), run without `--axi` and compared against pre-story expected output strings.
**Mock/Stub Requirements:** Reuses existing fanout/reconcile test doubles already present in `cmd/atcr/review_test.go` and `cmd/atcr/resume_test.go` — no new mocks.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] All pre-existing non-`--axi` tests in `cmd/atcr/review_test.go` and `cmd/atcr/resume_test.go` pass unmodified after AC 04-01's gating lands
- [x] Explicit stdout-content assertions exist for every write site listed in AC 04-01, confirming unchanged text/format under non-`--axi` invocations
- [x] The `AllComplete()` resume branch (resume.go:152-164) is confirmed unaffected without `--axi`
- [x] Mixed-flag scenarios (`--verify`/`--debate` without `--axi`) confirmed to retain full human-oriented output

**Manual Review:**
- [x] Code reviewed and approved
