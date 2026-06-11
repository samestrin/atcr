# Acceptance Criteria: End-to-End Review Workflow

**Related User Story:** [01: CLI Review Workflow](../user-stories/01-cli-review-workflow.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Commands | Go + Cobra | `review`, `reconcile`, `report` subcommands |
| Test Framework | testify | assertions and suite |
| Key Dependencies | cobra, os/exec, sync | orchestration, git calls, concurrency |

## Related Files
- `cmd/atcr/main.go` - create: root Cobra command registration
- `cmd/atcr/review.go` - create: `atcr review` subcommand entry point
- `cmd/atcr/reconcile.go` - create: `atcr reconcile` subcommand entry point
- `cmd/atcr/report.go` - create: `atcr report` subcommand entry point

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [CLI Architecture](../documentation/cli-architecture.md) — Cobra patterns, `RunE`, `ExecuteContext` for global timeout, centralized exit-code logic in `main()`.
- [Range Resolution](../documentation/range-resolution.md) — Decision tree that `atcr review` invokes (explicit → merge-commit → auto) before any provider call.
- [LLM Client & Fan-out](../documentation/llm-client-fanout.md) — Parallel/serial lanes, partial-success semantics, fallback chain.
- [Reconciler & Findings Stream](../documentation/reconciler.md) — Pipeline invoked by `atcr reconcile` (discover → normalize → cluster → dedupe → merge → confidence → emit).
- [Findings Format v1](../documentation/findings-format.md) — `# atcr-findings/v1` header, 8-col per-source, 9-col reconciled; the contract between stages.

### Spec alignment notes

- Per `original-requirements.md` (line 100-110), reconciled `findings.txt` is **9 columns** (per-source 8 with `REVIEWERS` plural + `CONFIDENCE`). `documentation/findings-format.md` has been corrected to 9 columns and matches `original-requirements.md`.
- Default review-id scheme: `<YYYY-MM-DD>_<branch-slug>` written to `.atcr/latest`; `--id` flag overrides.
- Partial-success semantics: `atcr review` returns exit code 0 when ≥1 agent succeeded (with `partial: true` in `manifest.json`); all-fail produces nonzero exit.

## Happy Path Scenarios

**Scenario 1: Full zero-argument workflow on feature branch**
- **Given** user is on a feature branch with commits ahead of default branch
- **When** user runs `atcr review && atcr reconcile && atcr report`
- **Then** review directory is created, agents are invoked, findings are reconciled, and a human-readable report is rendered

**Scenario 2: Workflow with explicit flags**
- **Given** user specifies `--base` and `--head` flags
- **When** user runs `atcr review --base abc123 --head def456`
- **Then** the specified range is used instead of auto-detection, and the command exits 0 with reconciled/report.md written

**Scenario 3: Single-agent minimal review**
- **Given** only one agent is configured in `.atcr/config.yaml` roster
- **When** user runs `atcr review`
- **Then** the single agent processes the diff and produces findings; reconcile succeeds with ≥1 finding captured

## Edge Cases

**Edge Case 1: All agents fail**
- **Given** all configured agents return errors (network, auth, timeout)
- **When** `atcr review` completes
- **Then** CLI exits with non-zero status and error message indicating total failure

**Edge Case 2: Partial agent failure**
- **Given** 2 of 3 configured agents succeed, 1 fails
- **When** `atcr review` completes
- **Then** review succeeds with `partial: true` in manifest.json; only successful agent findings are reconciled

**Edge Case 3: No findings from any agent**
- **Given** all agents return empty findings (no issues detected)
- **When** `atcr reconcile` runs
- **Then** reconciled output contains empty findings list; report renders with "No findings" message

## Error Conditions

**Error Scenario 1: No config.yaml found**
- Error message: "no roster found: .atcr/config.yaml not found"
- Exit code: 1

**Error Scenario 2: No providers configured**
- Error message: "no providers configured in ~/.config/atcr/registry.yaml"
- Exit code: 1

## Performance Requirements
- **Response Time:** Full workflow (review + reconcile + report) completes within 120 seconds for a diff of 50 files
- **Throughput:** Fan-out invokes all configured agents concurrently (parallel lane)

## Security Considerations
- **Authentication/Authorization:** API keys loaded from registry.yaml; never logged or included in output artifacts
- **Input Validation:** Validate base/head SHAs are valid git refs before invoking agents

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** Mock git repo with 2-3 commits, mock LLM responses, sample roster config
**Mock/Stub Requirements:** Mock HTTP server for LLM API calls; mock git commands via test fixtures
**Test Framework:** `github.com/stretchr/testify` (assert/require/suite) per [Testing Patterns](../documentation/testing-patterns.md). Use `httptest.NewServer` for provider mocks with `atomic.Int32` high-water mark for parallelism verification. Build tag `//go:build integration` for the end-to-end scenario tests.
**Coverage Target:** ≥70% per `plan.md` success criteria.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `atcr review && atcr reconcile && atcr report` completes end-to-end with zero arguments
- [x] Partial failure: manifest.json records partial:true, exit code is 0 when ≥1 agent succeeded, and failed agents contribute no rows to sources/pool/findings.txt
- [x] All three subcommands registered and accessible via `atcr --help`
- [x] Exit codes are correct (0 success, 1 failure)

**Manual Review:**
- [ ] Code reviewed and approved
