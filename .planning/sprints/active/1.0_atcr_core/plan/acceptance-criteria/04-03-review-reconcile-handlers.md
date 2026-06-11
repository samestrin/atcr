# Acceptance Criteria: Review and Reconcile Tool Handlers

**Related User Story:** [04: MCP Integration](../user-stories/04-mcp-integration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| MCP Handlers | Go functions | Thin wrappers calling internal packages |
| Fanout Package | `internal/fanout` | Shared with CLI; no logic duplication |
| Reconcile Package | `internal/reconcile` | Shared with CLI; no logic duplication |
| Git Range Package | `internal/gitrange` | Resolve base/head from git |
| Test Framework | `testify` (assert, require) | Table-driven tests with mocked internals |

## Related Files
- `internal/mcp/handlers.go` - create: Handler functions for atcr_review and atcr_reconcile
- `internal/mcp/handlers_test.go` - create: Unit tests for review and reconcile handlers
- `internal/mcp/server.go` - modify: Wire handlers to tool registration
- `internal/fanout/fanout.go` - create/reuse: Fanout engine called by both CLI and MCP handler
- `internal/reconcile/reconcile.go` - create/reuse: Reconciler called by both CLI and MCP handler

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [MCP Server Implementation](../documentation/mcp-server.md) — Authoritative spec for thin handlers over the shared engine; no business logic in handlers; same `internal/` packages as CLI.
- [LLM Client & Fan-out](../documentation/llm-client-fanout.md) — `atcr_review` handler invokes the fan-out engine with the same args it would receive from the CLI command.
- [Reconciler & Findings Stream](../documentation/reconciler.md) — `atcr_reconcile` handler invokes the same reconciler pipeline that `cmd/atcr/reconcile.go` invokes.

### Spec alignment notes

- **atcr_review returns immediately** once the review directory is created and fan-out has started (per `user-stories/04-mcp-integration.md` original criterion #12), with result `{review_id, review_path, status: "running", agent_count}`. Fan-out continues in the server process; clients poll `atcr_status` for completion. The `partial` flag is reported by `atcr_status` / `manifest.json` at completion, not by `atcr_review`. This avoids blocking the MCP client on multi-minute fan-out across N agents.
- **atcr_reconcile arg `id_or_path` defaults to `.atcr/latest`** (per `user-stories/04-mcp-integration.md` original criterion #5). Handler reads the latest pointer when arg is empty.
- **Path containment invariant**: `id_or_path` is validated to be under `.atcr/reviews/` and not contain `..` or absolute paths. Reject with `MCP error code -32602` (Invalid params) on violation.
- **No partial-result surprise**: `partial: true` is always set in `manifest.json` (and reported by `atcr_status`) at completion when ≥1 agent fails but ≥1 succeeds. The engine does not retry; the client decides whether to re-invoke with a new review id.
- **`atcr_reconcile` with `fail_on`** returns `pass: true|false`; when `pass: false`, the failing findings are included in the result so the MCP client can render them inline without a follow-up `atcr_report` call.

## Happy Path Scenarios

**Scenario 1: atcr_review triggers review with default resolution**
- **Given** the MCP client calls `atcr_review` with empty args `{}`
- **When** the handler receives the call
- **Then** the handler resolves head=HEAD and base from git (merge-base or default branch)
- **And** the handler invokes the fanout package to run all configured personas
- **And** the result contains: `review_id` (string), `review_path` (string), `status` (`"running"`), `agent_count` (int)
- **And** the review artifacts are written to `.atcr/reviews/<review_id>/`

**Scenario 2: atcr_review with explicit base and head**
- **Given** the MCP client calls `atcr_review` with `{"base": "main", "head": "feature-x"}`
- **When** the handler receives the call
- **Then** the handler uses the provided base and head without git auto-resolution
- **And** the fanout runs with the specified diff range

**Scenario 3: atcr_review with merge_commit**
- **Given** the MCP client calls `atcr_review` with `{"merge_commit": "abc123"}`
- **When** the handler receives the call
- **Then** the handler resolves the range as `abc123^..abc123`
- **And** the fanout runs with that single-commit diff

**Scenario 4: atcr_review returns immediately (non-blocking)**
- **Given** the MCP client calls `atcr_review`
- **When** the review directory is created and fan-out has started
- **Then** the handler returns immediately with `{review_id, review_path, status: "running", agent_count}`
- **And** fan-out continues in the server process
- **And** the client polls `atcr_status` for completion; the `partial` flag is reported by `atcr_status` / `manifest.json` at completion, not by `atcr_review`

**Scenario 5: atcr_reconcile processes latest review by default**
- **Given** a completed review exists at `.atcr/reviews/20260610-120000/`
- **And** `.atcr/latest` points to that review
- **When** the MCP client calls `atcr_reconcile` with empty args `{}`
- **Then** the handler reads the review from `.atcr/latest` path
- **And** invokes the reconciler to deduplicate and score findings
- **And** writes reconciled output to the review directory
- **And** returns the reconciliation summary

**Scenario 6: atcr_reconcile with explicit id**
- **Given** the MCP client calls `atcr_reconcile` with `{"id_or_path": "20260610-120000"}`
- **When** the handler receives the call
- **Then** the handler resolves the review at `.atcr/reviews/20260610-120000/`
- **And** processes reconciliation for that specific review

**Scenario 7: atcr_reconcile with fail_on threshold**
- **Given** the MCP client calls `atcr_reconcile` with `{"fail_on": "MEDIUM"}`
- **When** reconciliation finds findings at MEDIUM severity or above (fail_on thresholds the SEVERITY column: CRITICAL|HIGH|MEDIUM|LOW; confidence is a separate column and plays no role in fail_on)
- **Then** the result includes a `pass: false` field indicating threshold breach
- **And** the finding details are included in the result

## Edge Cases

**Edge Case 1: No git repository found**
- **Given** `atcr_review` is called in a directory without a git repository
- **When** the handler attempts git resolution
- **Then** the handler returns error: "not a git repository: run atcr init first"
- **And** no review artifacts are created

**Edge Case 2: Review directory already exists for same timestamp**
- **Given** a review was started within the same second
- **When** the handler attempts to create the review directory
- **Then** the handler appends a suffix to ensure uniqueness (e.g., `20260610-120000-1`)

**Edge Case 3: Reconcile called on review with no agent results**
- **Given** a review directory exists but contains no agent output files
- **When** `atcr_reconcile` is called for that review
- **Then** the handler returns error: "no agent results found in review <id>; run atcr_review first"

**Edge Case 4: Reconcile with fail_on but no findings exceed threshold**
- **Given** `atcr_reconcile` called with `{"fail_on": "HIGH"}`
- **When** all findings are below HIGH severity
- **Then** the result includes `pass: true`

**Edge Case 5: Reconcile with invalid fail_on value**
- **Given** `atcr_reconcile` is called with an invalid `fail_on` value (e.g., `"SEVERE"`)
- **When** schema/enum validation runs
- **Then** the call is rejected with an error before any reconciliation work executes

**Edge Case 6: All agents fail during a review**
- **Given** all agents fail during a review started via `atcr_review`
- **When** the client polls `atcr_status`
- **Then** status reports the run as failed (not partial) with per-agent error detail from the manifest/status files

## Error Conditions

**Error Scenario 1: Fanout fails (one or more agents error)**
- Error message: "review completed with errors: <agent_name> failed: <error>"
- Behavior: Review is partial; `manifest.json` records `partial: true` and the list of failed agents, reported via `atcr_status` at completion

**Error Scenario 2: Review path does not exist**
- Error message: "review not found: <id_or_path>"
- MCP error code: -32602 (Invalid params)

## Performance Requirements
- **Handler Overhead:** MCP handler adds < 5ms overhead beyond internal package execution
- **Review Dispatch:** Fanout dispatches to all agents within 100ms of handler call
- **Reconcile Time:** Reconciliation completes in < 500ms for up to 6 agent result files

## Security Considerations
- **Path Traversal:** `id_or_path` is validated to prevent directory traversal; only paths under `.atcr/reviews/` are allowed
- **No shell execution:** Handlers do not shell out; all operations use Go standard library and internal packages
- **Input Sanitization:** Review IDs are validated to match expected format (timestamp-based)

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION
**Test Data Requirements:**
- Mock git repository (via `internal/gitrange` test helpers)
- Sample agent result files for reconcile testing
- Sample config with persona definitions
**Mock/Stub Requirements:**
- Mock LLM calls in fanout (return canned responses)
- Mock filesystem for review directory creation (use `t.TempDir()`)

**Test Cases:**
1. `TestReviewHandler_DefaultArgs` — verify default git resolution
2. `TestReviewHandler_ExplicitBaseHead` — verify explicit base/head usage
3. `TestReviewHandler_MergeCommit` — verify merge_commit range resolution
4. `TestReviewHandler_PartialFailure` — verify partial result on agent error
5. `TestReconcileHandler_LatestReview` — verify default reads from .atcr/latest
6. `TestReconcileHandler_ExplicitID` — verify explicit review ID resolution
7. `TestReconcileHandler_FailOn` — verify fail_on threshold logic
8. `TestReconcileHandler_NoResults` — verify error on empty review
9. `TestHandler_NoLogicDuplication` — verify handlers only call internal packages (code structure test)

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (unit + integration)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./cmd/atcr`)
- [ ] Handler files contain zero business logic (only delegation to internal packages)

**Story-Specific:**
- [ ] `atcr_review` returns `{review_id, review_path, status: "running", agent_count}` immediately once the review directory is created and fan-out has started
- [ ] `atcr_reconcile` returns reconciliation summary with pass/fail status
- [ ] Handlers call `internal/fanout` and `internal/reconcile` directly (no duplicated logic)
- [ ] `atcr_review` supports args: id, base, head, merge_commit (all optional)
- [ ] `atcr_reconcile` supports args: id_or_path, fail_on (all optional)
- [ ] Default id_or_path resolves via `.atcr/latest` pointer

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Handler logic verified as thin wrapper (no business logic)
- [ ] Handlers are verified in code review to call the same internal engine functions as the CLI — no duplicated logic
- [ ] Error messages are clear and actionable
