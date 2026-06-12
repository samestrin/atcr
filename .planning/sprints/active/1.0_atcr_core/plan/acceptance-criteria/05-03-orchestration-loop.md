# Acceptance Criteria: Orchestration Loop

**Related User Story:** [05: Host Review via Skill](../user-stories/05-host-review-via-skill.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Orchestration | Skill instructions (Markdown) | Agent follows sequential steps |
| CLI Invocation | `os/exec` via agent shell | Calls to `atcr` binary |
| Background Polling | `atcr status <id>` polled by the Skill | `atcr review` runs as a background process; no `--wait` flag in v1 |
| Timeout | Configurable max retries | Prevents infinite hangs |
| Test Framework | `testify` (assert, suite) | Integration tests with mock atcr binary |

## Related Files
- `skill/SKILL.md` - modify: Add orchestration loop steps with exact command sequences
- `cmd/atcr/status.go` - create: `atcr status` command to check review progress
- `cmd/atcr/status_test.go` - create: Tests for status command output and exit codes
- `internal/review/manifest.go` - create: Review manifest reader (used by skill to find review directory)

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [CLI Architecture](../documentation/cli-architecture.md) — Skill calls the same cobra commands; orchestration is a sequence of CLI invocations, not custom Go code.
- [LLM Client & Fan-out](../documentation/llm-client-fanout.md) — The fan-out's per-agent `status.json` and the `summary.json` shape the `atcr status` command reads.
- [Reconciler & Findings Stream](../documentation/reconciler.md) — The Skill's reconcile step uses the same `atcr reconcile` CLI; the Skill reads `reconciled/report.md` to present results.
- [Range Resolution](../documentation/range-resolution.md) — `atcr range` pre-flight result JSON shape; the Skill uses it to validate the input before triggering fan-out.

### Spec alignment notes

- **Orchestration is exactly**: `atcr range` → `atcr review` (background, polled) → host review → `atcr reconcile` → `atcr report` → present `report.md`. Per `plan.md` (The atcr Skill section) and `user-stories/05-host-review-via-skill.md` original criterion #6.
- **Polling interval** is 10 seconds with max 60 polls (10-minute default timeout); both are configurable via skill arguments.
- **Background review**: there is no `--wait` flag in v1. The Skill launches `atcr review` as a background process and polls `atcr status <id>` in a bounded loop (max 60 polls at a 10-second interval, then a timeout error); the Skill never blocks on the review command.
- **Host review is the +1 reviewer**: the Skill writes `sources/host/findings.txt` so reconcile always has 2+ sources (host + pool agents), which produces HIGH confidence when they agree. Per `user-stories/05-host-review-via-skill.md` background.
- **No sprint knowledge** in the Skill: input is a git range, branch, or PR; output is the review directory path. Per `user-stories/05-host-review-via-skill.md` original criterion #11.
- **Timeout enforcement** at the Skill level prevents runaway orchestration: the Skill aborts if any step exceeds the configured per-step timeout and reports the failure to the user with a clear next-step suggestion.

## Happy Path Scenarios

**Scenario 1: Full orchestration loop completes successfully**
- **Given** the skill is invoked with a valid git range and the atcr binary is available
- **When** the agent follows the orchestration steps
- **Then** the agent runs `atcr range` to resolve and validate the git range
- **And** runs `atcr review` to fan out to pool agents (background, polled)
- **And** performs the host review (writes sources/host/findings.txt)
- **And** runs `atcr reconcile` to merge all findings
- **And** runs `atcr report` to generate the final report
- **And** presents the report.md content to the user

**Scenario 2: Review directory is created and populated**
- **Given** the orchestration loop starts with `atcr range`
- **When** the range is validated successfully
- **Then** a review directory is created at `.atcr/reviews/<id>/`
- **And** the review ID is written to `.atcr/latest`
- **And** `manifest.json` records the resolved base/head SHAs and roster

**Scenario 3: Host review runs after pool agents complete**
- **Given** `atcr review` has been started in background
- **When** the agent polls `atcr status` and the review completes
- **Then** the agent reads the payload from `.atcr/reviews/<id>/payload/`
- **And** generates host findings
- **And** writes them to `sources/host/findings.txt`
- **And** proceeds to `atcr reconcile`

**Scenario 4: Reconcile includes host as a reviewer**
- **Given** pool agent findings and host findings both exist in `sources/`
- **When** `atcr reconcile` runs
- **Then** the reconciler discovers findings from all sources (pool + host)
- **And** the reconciled report.md lists `host` as a reviewer alongside pool agents
- **And** confidence scoring accounts for host agreement with pool agents

**Scenario 5: Skill outputs review directory path**
- **Given** the orchestration loop completes
- **When** the final report is presented
- **Then** the skill outputs the review directory path (`.atcr/reviews/<id>/`)
- **And** the user can navigate to the directory for full artifacts

## Edge Cases

**Edge Case 1: Pool agents complete before host review starts**
- **Given** `atcr review` completes in under 5 seconds (small diff with fast models)
- **When** the agent checks `atcr status`
- **Then** the status shows completed on the first poll
- **And** the agent proceeds immediately to host review without additional polling intervals

**Edge Case 2: Review ID already exists (re-run)**
- **Given** a review with the same ID already exists in `.atcr/reviews/`
- **When** the skill invokes `atcr review` again
- **Then** the review uses the existing directory and overwrites artifacts
- **And** the orchestrating proceeds normally

**Edge Case 3: Only host review (no pool agents configured)**
- **Given** the user has no pool agents configured in `.atcr/config.yaml`
- **When** the skill runs the orchestration loop
- **Then** the host review is still performed
- **And** `atcr reconcile` processes only host findings
- **And** confidence is MEDIUM (single source)

**Edge Case 4: Orchestration with --id flag override**
- **Given** the user specifies a custom review ID
- **When** the skill passes the ID to `atcr review --id <custom>`
- **Then** the review directory uses the custom ID
- **And** all subsequent commands (reconcile, report) default to it via `.atcr/latest`

**Edge Case 5: Zero findings from all sources (clean review)**
- **Given** all sources produced `findings.txt` files but none contain any findings
- **When** `atcr reconcile` and `atcr report` run
- **Then** the orchestration reports "no issues found" and exits 0
- **And** this is a success path, not an error

**Edge Case 6: Partial pool agent failure**
- **Given** some pool agents fail during review (partial success)
- **When** orchestration continues
- **Then** reconcile proceeds with the successful sources
- **And** the presented report notes `partial: true`

**Edge Case 7: `.atcr/latest` missing or stale**
- **Given** `.atcr/latest` is missing or stale
- **When** the Skill orchestrates
- **Then** it passes the explicit review id (captured from the `atcr review` output) to reconcile/report rather than relying on the pointer

## Error Conditions

**Error Scenario 1: `atcr range` fails (empty range)**
- Error message: "Range is empty: no changes between <base> and <head>. Nothing to review."
- Skill behavior: Halt orchestration and report the error to the user

**Error Scenario 2: `atcr review` exceeds timeout**
- Error message: "Review timed out after <N> seconds. Check 'atcr status' for details."
- Skill behavior: Stop polling, report timeout, suggest checking individual agent logs

**Error Scenario 3: `atcr reconcile` fails (no reconcile sources)**
- **Given** `sources/` contains no source directories with `findings.txt` at all (the review never produced artifacts)
- **When** `atcr reconcile` runs
- **Then** it exits with an error
- Error message: "no reconcile sources found under sources/"
- Skill behavior: Halt orchestration and report the error to the user
- Note: zero findings from sources that did produce `findings.txt` is NOT this scenario — that is the success path (Edge Case 5)

**Error Scenario 4: `atcr` command not found during orchestration**
- Error message: "atcr binary not found in PATH. Install atcr before using the skill."
- Skill behavior: Halt immediately with installation instructions

## Performance Requirements
- **Range Resolution:** `atcr range` completes in < 5 seconds for repositories with < 10,000 commits
- **Review Polling:** Status polling interval is 10 seconds with max 60 polls (10-minute timeout default)
- **Host Review:** The host-review step issues no atcr calls other than status polling
- **Reconcile:** `atcr reconcile` completes in < 10 seconds for < 100 findings across < 10 sources
- **End-to-End:** atcr command overhead < 20 seconds total

## Security Considerations
- **Command injection prevention:** All `atcr` command arguments are validated; no user input passed through shell interpolation
- **Review directory containment:** All file writes confined to `.atcr/reviews/<id>/` and `.atcr/latest`
- **No network calls in skill:** Skill only invokes local `atcr` binary; all LLM calls are made by `atcr review` internally
- **Timeout enforcement:** Orchestration has configurable timeout to prevent runaway processes

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:**
- Mock git repository with feature branch and base branch
- Mock `atcr` binary (shell script that simulates range/review/reconcile/report commands)
- Sample manifest.json and payload files
- Pre-canned host findings fixture
**Mock/Stub Requirements:**
- Mock `atcr review` with controlled completion timing
- Mock `atcr status` returning progressing/completed states
- Mock filesystem for review directory structure

**Test Cases:**
1. `TestOrchestration_FullLoop` — verify complete range → review → host → reconcile → report
2. `TestOrchestration_PollingCompletes` — verify status polling terminates on completion
3. `TestOrchestration_Timeout` — verify orchestration halts on review timeout
4. `TestOrchestration_EmptyRange` — verify halt on empty range from `atcr range`
5. `TestOrchestration_HostOnlyNoPool` — verify orchestration works with host as sole reviewer
6. `TestOrchestration_OutputsReviewDir` — verify review directory path in output
7. `TestStatus_ProgressingState` — verify status command output while review in progress
8. `TestStatus_CompletedState` — verify status command output after review completes

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (unit + integration)
- [x] No linting errors (`golangci-lint run`)
- [x] Build succeeds (`go build ./cmd/atcr`)
- [x] `atcr status` command returns correct state

**Story-Specific:**
- [x] Skill orchestration follows: range → review → host review → reconcile → report
- [x] Background review polling works with configurable timeout
- [x] Host review runs after pool agents complete
- [x] Reconciled report includes host as a reviewer
- [x] Review directory path is output on completion
- [x] Orchestration halts gracefully on range/review/reconcile errors

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Orchestration loop verified end-to-end with a real review run
