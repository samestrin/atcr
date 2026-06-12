# Acceptance Criteria: Report, Range, and Status Tool Handlers

**Related User Story:** [04: MCP Integration](../user-stories/04-mcp-integration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| MCP Handlers | Go functions | Thin wrappers calling internal packages |
| Report Package | `internal/report` | Renders markdown, JSON, or checklist output |
| Git Range Package | `internal/gitrange` | Resolves base/head/merge_commit ranges |
| Test Framework | `testify` (assert, require) | Table-driven tests |

## Related Files
- `internal/mcp/handlers.go` - create: Handler functions for atcr_report, atcr_range, atcr_status
- `internal/mcp/handlers_test.go` - create: Unit tests for report, range, and status handlers
- `internal/mcp/server.go` - modify: Wire handlers to tool registration
- `internal/report/report.go` - create/reuse: Report renderer called by both CLI and MCP handler
- `internal/gitrange/resolve.go` - create/reuse: Git range resolver called by atcr_range handler

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [MCP Server Implementation](../documentation/mcp-server.md) — Authoritative spec for the 5-tool table; thin handlers call the same `internal/` packages as the CLI.
- [Range Resolution](../documentation/range-resolution.md) — `atcr_range` handler invokes the resolver; returns the `Resolution` JSON shape documented in the spec.
- [Reconciler & Findings Stream](../documentation/reconciler.md) — `atcr_report` handler reads from `reconciled/findings.json`; `report.md` shape documented in the spec.
- [CLI Architecture](../documentation/cli-architecture.md) — `atcr_report` default format is `md`; format enum `md|json|checklist` enforced at the schema level (not in the handler).

### Spec alignment notes

- **atcr_report default format is `md`** when `format` arg is omitted. The format enum `md|json|checklist` is enforced by the JSON Schema (not in the handler) — clients sending an invalid value receive a schema-validation error before the handler runs.
- **atcr_range returns the `Resolution` struct** as JSON: `{base, head, commit_count, file_count}` (with `detection_mode`, `default_branch`, `shallow`, `resolved_at` available for clients that need them). The empty-diff case is **not** an error — it returns `commit_count: 0, file_count: 0` so pre-flight checks can detect "nothing to review" without exception handling.
- **atcr_status reads from `manifest.json`** to derive status; if the manifest is missing required fields, the handler returns an error rather than guessing. Status values: `in_progress`, `completed`, `failed`, `stale` (the first three mapped from per-agent `status.json` aggregation; `stale` is inferred — see the Epic 1.5 amendment below).
- **Path containment** on `id_or_path` (same invariant as AC 04-03): paths must resolve under `.atcr/reviews/`.

### Contract Amendment — `stale` status (Epic 1.5, 2026-06-12)

The `status` value set is extended with a fourth value, `stale`. This is additive: the `ReviewStatus` JSON shape `{review_id, status, agent_count, agents_done, agents_pending, partial}` is unchanged, and the MCP `StatusResult` type alias and `atcr status` CLI continue to pass the value through unchanged.

- **`stale`** is an *inferred* terminal state, not an observed one. `ReadReviewStatus` reports it when `summary.json` (the completion sentinel) is absent **and** the manifest's `started_at + timeout_secs + 60s` grace margin has elapsed. A scaffolded review that died after fan-out (post-persistence failure, killed MCP server) thus reports `stale` instead of `in_progress` forever.
- **Backward compatibility:** a manifest written before `timeout_secs` existed (zero value) has no inferable deadline, so stale inference is disabled and it keeps reporting `in_progress`.
- **Post-fan-out persistence failure maps to `failed`,** not `stale`: a `WritePool` error writes a best-effort minimal `summary.json` (`succeeded=0`) that the existing reader path reports as `failed`; `stale` is only the fallback when even that marker cannot be written.
- **No new sentinel artifact:** `summary.json` remains the sole completion signal; `stale` adds no competing state-bearing file.
- **Poll-loop guidance:** consumers (the skill orchestration loop, CI gates) MUST treat `stale` as **terminal** alongside `completed`/`failed` — stop polling, do not wait for it to clear. See AC 05-03 orchestration loop.

## Happy Path Scenarios

**Scenario 1: atcr_report renders markdown by default**
- **Given** a completed and reconciled review exists at `.atcr/latest`
- **When** the MCP client calls `atcr_report` with empty args `{}`
- **Then** the handler reads the reconciled findings from the latest review
- **And** renders them as markdown (default format)
- **And** returns the rendered markdown string in the result

**Scenario 2: atcr_report with JSON format**
- **Given** the MCP client calls `atcr_report` with `{"format": "json"}`
- **When** the handler processes the request
- **Then** the handler returns the reconciled findings as a JSON object
- **And** the JSON structure matches the internal finding schema (id, title, severity, confidence, file, line, description)

**Scenario 3: atcr_report with checklist format**
- **Given** the MCP client calls `atcr_report` with `{"format": "checklist"}`
- **When** the handler processes the request
- **Then** the handler returns findings formatted as a markdown checklist
- **And** each item includes `- [ ]` prefix with finding summary

**Scenario 4: atcr_report for a specific review**
- **Given** the MCP client calls `atcr_report` with `{"id_or_path": "20260610-120000", "format": "md"}`
- **When** the handler processes the request
- **Then** the handler reads from `.atcr/reviews/20260610-120000/`
- **And** returns the rendered report for that specific review

**Scenario 5: atcr_range resolves current branch diff**
- **Given** the MCP client calls `atcr_range` with empty args `{}`
- **When** the handler processes the request
- **Then** the handler resolves base from default branch and head from HEAD
- **And** returns `{base: "<ref>", head: "<ref>", commit_count: <int>, file_count: <int>}`

**Scenario 6: atcr_range with explicit merge_commit**
- **Given** the MCP client calls `atcr_range` with `{"merge_commit": "abc123"}`
- **When** the handler processes the request
- **Then** the handler resolves range as `abc123^..abc123`
- **And** returns the range details for that single commit

**Scenario 7: atcr_status returns review progress**
- **Given** a review is in progress at `.atcr/reviews/20260610-120000/`
- **When** the MCP client calls `atcr_status` with `{"id_or_path": "20260610-120000"}`
- **Then** the handler returns `{review_id, status: "in_progress"|"completed"|"failed"|"stale", agent_count, agents_done, agents_pending}`

**Scenario 8: atcr_status defaults to latest review**
- **Given** the MCP client calls `atcr_status` with empty args `{}`
- **When** the handler processes the request
- **Then** the handler reads `.atcr/latest` to find the current review
- **And** returns the status for that review

## Edge Cases

**Edge Case 1: Report called on review with no reconciliation**
- **Given** a review exists but reconciliation has not been run
- **When** `atcr_report` is called for that review
- **Then** the handler returns error: "review <id> has no reconciliation results; run atcr_reconcile first"

**Edge Case 2: Report with invalid format**
- **Given** the MCP client calls `atcr_report` with `{"format": "xml"}`
- **When** the handler validates args
- **Then** the handler returns error: "invalid format: xml; must be one of: md, json, checklist"
- **Note:** Schema enum validation should catch this before the handler runs

**Edge Case 3: atcr_range in non-git directory**
- **Given** `atcr_range` is called outside a git repository
- **When** the handler attempts git resolution
- **Then** the handler returns error: "not a git repository"

**Edge Case 4: atcr_status with no reviews**
- **Given** no reviews have been created yet (`.atcr/latest` does not exist)
- **When** `atcr_status` is called with empty args
- **Then** the handler returns error: "no reviews found; run atcr_review first"

**Edge Case 5: atcr_range with empty diff**
- **Given** base and head point to the same commit
- **When** `atcr_range` is called
- **Then** the handler returns `{base, head, commit_count: 0, file_count: 0}`
- **And** does not error

## Error Conditions

**Error Scenario 1: Review not found**
- Error message: "review not found: <id_or_path>"
- MCP error code: -32602 (Invalid params)

**Error Scenario 2: Reconciliation not yet run**
- Error message: "review <id> has no reconciliation results; run atcr_reconcile first"
- MCP error code: -32603 (Internal error)

**Error Scenario 3: Git resolution fails**
- Error message: "failed to resolve range: <git error>"
- MCP error code: -32603 (Internal error)

## Performance Requirements
- **Report Rendering:** Markdown report renders in < 100ms for up to 50 findings
- **Range Resolution:** Git range resolution completes in < 200ms
- **Status Check:** Status lookup completes in < 10ms (filesystem read only)

## Security Considerations
- **Path Traversal:** `id_or_path` validated to prevent directory traversal; only `.atcr/reviews/` paths allowed
- **Format Validation:** Format field constrained to enum (`md`, `json`, `checklist`); enforced by JSON Schema
- **No user input in shell commands:** All git operations use Go git library or `exec.Command` with argument arrays (no shell interpolation)

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION
**Test Data Requirements:**
- Sample reconciled findings (JSON fixture)
- Mock git repository state for range tests
- Review directory structure for status tests
**Mock/Stub Requirements:**
- Mock filesystem for review directory (use `t.TempDir()`)
- Mock git operations for range resolution
- Pre-populated `.atcr/latest` symlink/file for default-path tests

**Test Cases:**
1. `TestReportHandler_DefaultMarkdown` — verify default format is markdown
2. `TestReportHandler_JSONFormat` — verify JSON output structure
3. `TestReportHandler_ChecklistFormat` — verify checklist output
4. `TestReportHandler_ExplicitID` — verify specific review targeting
5. `TestReportHandler_NoReconciliation` — verify error when no reconcile results
6. `TestRangeHandler_DefaultResolution` — verify base/head auto-resolution
7. `TestRangeHandler_MergeCommit` — verify merge_commit range
8. `TestRangeHandler_EmptyDiff` — verify zero-count result (no error)
9. `TestRangeHandler_NoGit` — verify error outside git repo
10. `TestStatusHandler_InProgress` — verify in-progress status reporting
11. `TestStatusHandler_Completed` — verify completed status
12. `TestStatusHandler_NoReviews` — verify error when no reviews exist
13. `TestStatusHandler_DefaultLatest` — verify reads from .atcr/latest

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (unit + integration)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./cmd/atcr`)
- [ ] All three report formats produce valid output

**Story-Specific:**
- [ ] `atcr_report` supports format enum: `md`, `json`, `checklist`
- [ ] `atcr_report` default format is `md`
- [ ] `atcr_range` returns `{base, head, commit_count, file_count}`
- [ ] `atcr_status` returns `{review_id, status, agent_count, agents_done, agents_pending}`
- [ ] All handlers default to `.atcr/latest` when `id_or_path` is omitted
- [ ] Handlers call `internal/report` and `internal/gitrange` directly (no duplicated logic)

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Report output reviewed for readability and correctness
- [ ] Error messages are clear and guide the user to the next action
