# Acceptance Criteria: `atcr review --axi` Gates Human-Oriented Live Output

**Related User Story:** [01: `--axi` Token-Dense Output Mode for `atcr review` and `atcr report`](../user-stories/01-axi-token-dense-output-mode.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI command (`cobra.Command`), stdout writer gating | Threads an axi-mode flag through the same command-context injection point already used for the logger/telemetry client |
| Test Framework | `go test` with `testify/assert`; `cobra` command execution against a captured `bytes.Buffer` stdout | Mirrors existing review-command tests that assert on `cmd.OutOrStdout()` content |
| Key Dependencies | `github.com/spf13/cobra`; no new dependency | |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/review.go` - modify: gate the live human-oriented `cmd.OutOrStdout()` writes at lines 433-434 (`"review %s: %d/%d agents succeeded..."`), 436 (`writeReviewSummary` call), 551 (`"reconciled %d finding(s)"`), 573 (`"verified %d finding(s)..."`), and 591 (`"debated %d item(s)..."`) behind the new `--axi` mode, replacing them with a single token-dense payload write when axi mode is active.
- `cmd/atcr/review_summary.go` - modify: `writeReviewSummary` (line 80) is the human end-of-review metrics block; either add an axi-aware sibling function or a mode branch so the same `summarySnapshot` data (line 25-33) can render as a TOON/JSON payload instead of the four `fmt.Fprintf` human lines (line 83-87).
- `internal/report/render.go` - reference: reuses the `FormatAXI` encoder built in AC 01-01/01-02 so the review-summary payload and the `atcr report --axi` payload share one encoding implementation rather than two divergent ones.
- `cmd/atcr/main.go` - modify: register the `--axi` flag (or `--format axi` mode value) on the `atcr review` command and thread it through `PersistentPreRunE`'s context-injection mechanism (`cmd.SetContext(telemetry.NewContext(...))` pattern, main.go line 230-239) so both `review.go` and `resume.go` can consult it via a `FromContext`-style accessor.

## Happy Path Scenarios
**Scenario 1: `atcr review --axi` suppresses human progress/summary lines**
- **Given** a repository with a resolvable git range
- **When** the user runs `atcr review --axi`
- **Then** stdout contains no human-oriented lines like `"review <id>: N/M agents succeeded (<dir>)"` or the four-line `writeReviewSummary` block, and instead contains a single token-dense TOON/JSON payload summarizing the run (id, dir, agent counts, findings counts), with zero ANSI escapes and zero Markdown syntax

**Scenario 2: `--axi` combined with `--verify`/`--debate`/reconcile chaining**
- **Given** `atcr review --axi --verify --debate`
- **When** the review, reconcile, verify, and debate stages all run (review.go lines 542-594)
- **Then** each stage's existing human line (`"reconciled %d finding(s)"` line 551, `"verified %d finding(s)..."` line 573, `"debated %d item(s)..."` line 591) is either suppressed or folded into the single axi payload rather than leaking a mixed human/machine stdout stream

## Edge Cases
**Edge Case 1: All agents fail (exit 1 path)**
- **Given** every reviewer agent fails during `atcr review --axi`
- **When** the run completes with all agents failed and the exit-1 path is taken
- **Then** the axi-mode payload (or its absence, per the exit-code story's stderr-diagnostics contract) still avoids emitting the human `"review %s: %d/%d agents succeeded"` line on stdout — diagnostic detail goes to stderr, consistent with the existing exit-code contract this story explicitly does not alter (see story Constraints: exit-code reconciliation is Story 2's scope, not this AC's)

**Edge Case 2: Graceful interrupt (SIGINT/SIGTERM)**
- **Given** a running `atcr review --axi` receives an interrupt
- **When** the interrupt is delivered and the run terminates early via the interrupt path
- **Then** `reportInterrupt` (review.go line 419) is still invoked, and its output must also honor axi mode — an interrupted run must not fall back to human-formatted text just because it took the interrupt path instead of the normal-completion path

**Edge Case 3: `--auto-fix` terminal path**
- **Given** `atcr review --axi --auto-fix`
- **When** the review completes and the auto-fix stage runs
- **Then** `orchestrateAutoFix` (review.go line 602) — which owns its own stdout handoff — is scoped for axi awareness in this story or explicitly deferred with a follow-up TD note if out of scope; it must not be silently left emitting unguarded human output when `--axi` is set

## Error Conditions
**Error Scenario 1: `--axi` combined with an incompatible flag**
- **Given** a flag combination that is not yet defined as axi-compatible (e.g. `--axi` with an interactive-only quickstart-style flag)
- **When** the command is invoked with that combination
- **Then** the command returns a usage error (exit 2) rather than emitting undefined/mixed output
- Error message: `"--axi is not supported with <flag>"` (exact wording to be finalized during sprint design)

## Performance Requirements
- **Response Time:** Gating existing writes behind a boolean/mode check adds negligible overhead (<1ms) versus the current unconditional `fmt.Fprintf` calls.
- **Throughput:** No change — this AC only changes what is written, not the review engine's execution path.

## Security Considerations
- **Authentication/Authorization:** None — no new auth surface.
- **Input Validation:** The axi-mode summary payload must reuse the same free-text escaping as AC 01-01/01-02 (finding problem/fix/evidence text originates from LLM reviewer output and is not trusted) so a reviewer-controlled string cannot inject ANSI escapes or break payload structure in the review-summary path, mirroring `sanitizeDisplay`'s control-character-stripping precedent (`cmd/atcr/models.go` line 288-299).

## Test Implementation Guidance
**Test Type:** INTEGRATION (cobra command execution against a captured stdout buffer)
**Test Data Requirements:** A fixture review directory (or mocked `fanout.ExecuteReview` result) sufficient to drive `runReview` through to the summary-write point without live LLM calls.
**Mock/Stub Requirements:** Mock/stub the LLM client and fan-out engine the same way existing `review.go` tests already do; assert on `cmd.OutOrStdout()` content for both `--axi` and non-`--axi` invocations to prove the flag actually toggles output shape.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/...`)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `atcr review --axi` stdout contains zero occurrences of the pre-existing human progress/summary line formats
- [ ] `atcr review --axi` stdout contains zero `\x1b[` ANSI sequences and zero Markdown table/heading syntax
- [ ] `--verify`/`--debate` chained stage output is also gated, not just the top-level summary
- [ ] Interrupt path (`reportInterrupt`) is axi-aware, not left on the human-only branch

**Manual Review:**
- [ ] Code reviewed and approved
