# Acceptance Criteria: Gate Human-Oriented Stdout Writes in review.go and resume.go Under AXI Mode

**Related User Story:** [04: AXI Stderr Isolation and Escape-Sequence Guarantee](../user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI command (cobra) | `cmd/atcr` binary, `RunE`/`cmd.OutOrStdout()` output pattern |
| Test Framework | `go test` / `testify` (`assert`, `require`) | Matches existing style in `cmd/atcr/*_test.go` |
| Key Dependencies | Story 1's AXI-mode flag/command-context propagation mechanism (threaded via `PersistentPreRunE`, same injection point as the root logger/telemetry client) | No new dependency; reuses existing context-injection pattern |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/review.go` - modify: gate the human-oriented `cmd.OutOrStdout()` writes at lines 433 (`"review %s: %d/%d agents succeeded (%s)\n"`), 436 (`writeReviewSummary` call), 551 (`"reconciled %d finding(s)\n"`), 573 (`"verified %d finding(s)..."`), 591 (`"debated %d item(s)..."`), and 602 (`orchestrateAutoFix` output writer) behind the AXI-mode value read from command context.
- `cmd/atcr/resume.go` - modify: gate the human-oriented `cmd.OutOrStdout()` writes at lines 153 (`"All configured agents already completed..."`), 170 (`"resuming review %s: %d completed, %d pending (%s)\n"`), 188 (`"review %s: %d/%d agents succeeded (%s)\n"`), 195 (`writeReviewSummary` call), and 259 (`resumeReconcile`'s `"reconciled %d finding(s)\n"`) behind the same AXI-mode value.
- `cmd/atcr/review_summary.go` - modify: `writeReviewSummary` (line 80) is shared by both callers (`review.go:436`, `resume.go:195`); apply gating consistently so both invocation paths are covered by a single change rather than two independently-maintained conditionals.
- `cmd/atcr/review_test.go` - modify: add/extend tests asserting captured `cmd.OutOrStdout()` is empty (or contains only the AXI payload) for the fresh-review path under `--axi`.
- `cmd/atcr/resume_test.go` - modify: add/extend tests asserting captured `cmd.OutOrStdout()` is empty (or contains only the AXI payload) for the resume path under `--axi`, including the `AllComplete()` re-reconcile branch (line 153).

## Happy Path Scenarios
**Scenario 1: Fresh review under `--axi` emits no human-oriented stdout text**
- **Given** `atcr review --axi` is run against a valid commit range
- **When** the review completes (agents succeed, reconcile runs)
- **Then** captured `cmd.OutOrStdout()` contains none of the strings `"agents succeeded"`, `"Total elapsed"`, `"Agents:"`, `"API calls:"`, `"Findings:"`, or `"reconciled"` — only the AXI payload (or nothing, if the payload is written by a different code path)

**Scenario 2: Resume under `--axi` emits no human-oriented stdout text**
- **Given** an interrupted review with pending agents, resumed via `atcr resume --axi`
- **When** the resume completes (all pending agents run, reconcile runs)
- **Then** captured `cmd.OutOrStdout()` contains none of the strings `"resuming review"`, `"agents succeeded"`, `"Total elapsed"`, or `"reconciled"`

**Scenario 3: `--verify`/`--debate`/`--auto-fix` chained under `--axi`**
- **Given** `atcr review --axi --verify --debate` is run
- **When** the verify and debate stages both complete
- **Then** neither the `"verified %d finding(s)..."` line (review.go:573) nor the `"debated %d item(s)..."` line (review.go:591) reaches stdout

## Edge Cases
**Edge Case 1: `AllComplete()` resume re-reconcile path**
- **Given** `atcr resume --axi` is run against a review where all configured agents already completed
- **When** the `info.AllComplete()` branch (resume.go:152-164) fires and re-runs reconciliation without touching any provider
- **Then** the `"All configured agents already completed. Re-running reconciliation..."` line (resume.go:153) does not reach stdout under `--axi`, matching the gating applied to the rest of the resume path

**Edge Case 2: Interrupted review under `--axi`**
- **Given** a review or resume is interrupted mid-fan-out (SIGINT/SIGTERM) while `--axi` is active
- **When** `reportInterrupt` runs on the interrupt path
- **Then** `reportInterrupt`'s stdout output (if any) is likewise gated — the interrupt-reporting path must not become an unguarded escape hatch that leaks human text under `--axi`

**Edge Case 3: `writeReviewSummary` gated consistently across both callers**
- **Given** the shared `writeReviewSummary` function is invoked from both `review.go:436` and `resume.go:195`
- **When** either caller runs under `--axi`
- **Then** both produce identical (empty/no-human-text) stdout behavior — gating is applied once at the shared function or consistently at both call sites, never at only one

## Error Conditions
**Error Scenario 1: All-agents-failed under `--axi`**
- **Given** every agent in the roster fails during `atcr review --axi`
- **When** the command returns its exit-1 error
- **Then** no `"agents succeeded"` progress line or `writeReviewSummary` output reaches stdout before the error is returned (stdout gating applies even on the error-return path, since `result != nil` still triggers the writes at review.go:432-436)
- Error message: N/A — this AC concerns stdout content, not the error message itself (unchanged, exit code 1)
- HTTP status / error code: CLI exit code `1` (unchanged by this story)

**Error Scenario 2: Reconcile failure under `--axi`**
- **Given** `RunReconcile` fails after a successful fan-out under `atcr review --axi --fail-on high`
- **When** the command returns its exit-2 usage error
- **Then** no `"reconciled %d finding(s)"` line was written to stdout before the failure (the gate applies before the reconcile call succeeds, not just after)
- Error message: propagated verbatim from the existing `usageError()` wrapping (unchanged)
- HTTP status / error code: CLI exit code `2` (unchanged by this story)

## Performance Requirements
- **Response Time:** Gating adds a single boolean check per write site — no measurable overhead; no new I/O, allocation, or computation is introduced beyond an `if axiMode { ... } else { ... }`-style branch.
- **Throughput:** N/A — CLI output formatting, not a throughput-sensitive path.

## Security Considerations
- **Authentication/Authorization:** No change — this story does not touch auth-error classification (exit 3) or the auth-error boundary established in Story 2.
- **Input Validation:** The AXI-mode value itself must already be validated by Story 1's flag parsing; this AC only consumes that validated value and must not silently default to human-mode on a malformed/unexpected value (fail closed toward gating stdout, not open).

## Test Implementation Guidance
**Test Type:** UNIT (primary, via `cmd/atcr/review_test.go` and `cmd/atcr/resume_test.go`, asserting on a captured `bytes.Buffer` passed as `cmd.OutOrStdout()`) + INTEGRATION (subcommand-level assertions using existing test helpers, e.g. `execute()` in `cmd/atcr/main_test.go`)
**Test Data Requirements:** A fixture review/resume scenario producing: (1) a clean successful review, (2) an `AllComplete()` resume, (3) an all-agents-failed review, (4) a `--verify`/`--debate` chained review — each run once under `--axi` and once without, so the two captured stdout buffers can be directly diffed for the "human text is gated / non-`--axi` unaffected" contract.
**Mock/Stub Requirements:** Reuse existing fanout/reconcile test doubles already present in `cmd/atcr/review_test.go` and `cmd/atcr/resume_test.go`; no new network or provider mocks required — this AC only changes what is written to an already-captured output writer.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Every listed `cmd.OutOrStdout()` write in `cmd/atcr/review.go` (lines 433, 436, 551, 573, 591, 602) is gated behind AXI mode, confirmed by code inspection
- [x] Every listed `cmd.OutOrStdout()` write in `cmd/atcr/resume.go` (lines 153, 170, 188, 195, 259) is gated behind AXI mode, confirmed by code inspection
- [x] Both callers of `writeReviewSummary` (`review.go:436`, `resume.go:195`) exhibit identical gated behavior under `--axi`
- [x] A test exercising the `AllComplete()` resume branch confirms line 153's text does not leak under `--axi`

**Manual Review:**
- [x] Code reviewed and approved
