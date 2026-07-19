# Acceptance Criteria: Shared Truncation Wrapper Applied Uniformly Across Both AXI Code Paths

**Related User Story:** [Story 3: AXI Pagination and Truncation Guarantees](../user-stories/03-axi-pagination-and-truncation-guarantees.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/report`) — single shared wrapping writer/post-processor, consumed by two CLI commands | Prevents the two AXI code paths (`atcr review --axi`, `atcr report --axi`) from diverging in cap behavior |
| Test Framework | `go test` at both the `internal/report` unit level and `cmd/atcr` integration level | Integration tests exercise the actual command entry points, not just the shared function |
| Key Dependencies | None new; reuses `internal/report/pagination.go` from AC 03-01 as the single implementation | |

### Related Files (from codebase-discovery.json)
- `internal/report/pagination.go` - modify: the single truncation implementation from AC 03-01/03-02/03-03, exported for use by both command entry points (not duplicated)
- `cmd/atcr/report.go` - modify: `atcr report --axi` command path calls the shared pagination step from `internal/report`
- `cmd/atcr/review.go` - modify: `atcr review --axi` live-output path calls the same shared pagination step, per Story 1's scope covering both code paths
- `cmd/atcr/resume.go` - reference/modify if applicable: any AXI-mode output surfaced via `atcr resume` (a `review` variant) must also route through the shared step rather than a separate implementation

## Happy Path Scenarios
**Scenario 1: `atcr review --axi` and `atcr report --axi` truncate identically for the same payload shape**
- **Given** two invocations — one via `atcr review --axi`, one via `atcr report --axi` — that both render the same underlying findings set exceeding 500 lines
- **When** both commands run with no `ATCR_AXI_MAX_LINES` override
- **Then** both emit output truncated at exactly 500 lines with identical `truncated: true` semantics and identical header `N` values — proving both paths route through the same shared truncation step rather than independent implementations

**Scenario 2: An `ATCR_AXI_MAX_LINES` override applies identically to both commands**
- **Given** `ATCR_AXI_MAX_LINES=50` is set
- **When** `atcr review --axi` and `atcr report --axi` are each run against payloads exceeding 50 lines
- **Then** both commands truncate at 50 lines — confirming the env-var resolution (AC 03-03) and the truncation mechanism (AC 03-01) are shared, not reimplemented per command

## Edge Cases
**Edge Case 1: `atcr review`'s live/streaming AXI output is capped the same as its final summary**
- **Given** `atcr review --axi` emits AXI-formatted output incrementally during a live run (per Story 1's "equivalent live-output path")
- **When** the cumulative emitted line count would exceed the cap
- **Then** the live-output path applies the same deterministic cap and `truncated` semantics as the batch `atcr report --axi` path, not a separate or looser cap

**Edge Case 2: A future third AXI-mode surface reuses the same wrapper without new truncation logic**
- **Given** the truncation step lives in `internal/report/pagination.go` as a single shared function/writer (per plan.md's Implementation Strategy)
- **When** a code reviewer inspects `cmd/atcr/review.go` and `cmd/atcr/report.go`
- **Then** neither file contains its own line-counting/truncation logic — both call into the same `internal/report` function, confirmed by the absence of duplicated cap constants or truncation loops outside `internal/report/pagination.go`

## Error Conditions
**Error Scenario 1: Divergence between the two paths is treated as a test failure, not a silent inconsistency**
- **Given** a regression test that runs the same synthetic payload through both `atcr review --axi` and `atcr report --axi`
- **When** the two outputs' `truncated` flag, header `N`, or emitted line count differ for identical input and identical `ATCR_AXI_MAX_LINES` setting
- **Then** the test fails loudly, preventing the two commands from silently diverging in cap behavior (per the story's Risk 1)

## Performance Requirements
- **Response Time:** No additional overhead beyond AC 03-01's truncation step — sharing one implementation adds no runtime cost over duplicating it, and avoids the maintenance cost of two divergent implementations.
- **Throughput:** N/A beyond AC 03-01.

## Security Considerations
- **Authentication/Authorization:** N/A.
- **Input Validation:** N/A beyond AC 03-01/03-02/03-03 — this AC is structural (single implementation, two call sites) rather than introducing new input-handling surface.

## Test Implementation Guidance
**Test Type:** INTEGRATION (exercises both `atcr review --axi` and `atcr report --axi` command entry points against identical fixture input) + a lightweight static/structural check (e.g. a test or code-review note confirming no parallel truncation logic exists in `cmd/atcr/review.go` or `cmd/atcr/report.go`)
**Test Data Requirements:** A shared synthetic fixture (findings set or diff) large enough to exceed the default cap, run through both commands.
**Mock/Stub Requirements:** For `atcr review --axi`, stub/mock the underlying review execution so the test can drive a large, deterministic findings set through the live-output path without invoking real reviewer agents.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `atcr review --axi` and `atcr report --axi` produce identical truncation behavior (line count, `truncated` flag, header `N`) for equivalent input
- [x] Both commands call a single shared implementation in `internal/report/pagination.go`, with no duplicated truncation logic in `cmd/atcr/review.go` or `cmd/atcr/report.go`
- [x] `atcr review --axi`'s live/incremental output path applies the same cap as its batch/summary output
- [x] An `ATCR_AXI_MAX_LINES` override applies identically across both commands

**Manual Review:**
- [x] Code reviewed and approved
