# Acceptance Criteria: `atcr resume --axi` Parity via Shared Context-Mode Propagation

**Related User Story:** [01: `--axi` Token-Dense Output Mode for `atcr review` and `atcr report`](../user-stories/01-axi-token-dense-output-mode.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI command, `context.Context`-based mode propagation | Reuses the `PersistentPreRunE` context-injection mechanism (`main.go` line 230-239) that already carries the logger and telemetry client |
| Test Framework | `go test` with `testify/assert`; cobra command execution | |
| Key Dependencies | `context` standard library package; existing `log.NewContext`/`telemetry.NewContext` pattern as the precedent to follow for a new (e.g.) `axi.NewContext`/`axi.FromContext` pair | |

## Related Files
- `cmd/atcr/resume.go` - modify: the duplicate `writeReviewSummary(cmd.OutOrStdout(), summaryDelta, time.Since(req.StartedAt))` call at line 195, plus the human `"resuming review %s: %d completed, %d pending..."` line (170) and `"review %s: %d/%d agents succeeded..."` line (188), must all be gated behind the same axi-mode check introduced in AC 01-03 for `review.go` — resume must not drift into a second, unguarded copy of this output logic.
- `cmd/atcr/main.go` - modify: add the axi-mode value to `PersistentPreRunE`'s context injection (main.go line 230-239) so a single flag parse populates context once and both `review.go` and `resume.go` read it via one accessor, rather than each command re-parsing `--axi` independently.
- `cmd/atcr/review_summary.go` - reference: `writeReviewSummary` (line 80) is the single shared function both `review.go` (line 436) and `resume.go` (line 195) call — the axi-mode branch belongs here once, not duplicated at each call site.

## Happy Path Scenarios
**Scenario 1: `atcr resume --axi` produces the same payload shape as `atcr review --axi`**
- **Given** a review directory with pending agents from a prior interrupted `atcr review` run
- **When** the user runs `atcr resume --axi <review-dir>`
- **Then** stdout emits the identical axi payload shape (same field names, same TOON/JSON structure) that `atcr review --axi` emits for a normal completion — no schema drift between the two entry points

**Scenario 2: Context-mode propagation requires no re-parsing at each call site**
- **Given** `--axi` is set on the `atcr resume` command
- **When** `resume.go` reaches its `writeReviewSummary` call (line 195)
- **Then** the axi-mode value is read from `cmd.Context()` via the same accessor pattern `log.FromContext`/`telemetry.FromContext` already use, not from a second independent flag lookup, so a future third call site (if any) automatically inherits correct behavior

## Edge Cases
**Edge Case 1: `AllComplete()` short-circuit path**
- **Given** a resume where all agents already completed (resume.go line 152: `if info.AllComplete()`)
- **Then** the human line `"All configured agents already completed. Re-running reconciliation..."` (line 153) is also gated by axi mode — this early-return branch must not be missed just because it bypasses the main `writeReviewSummary` call

**Edge Case 2: `--axi` set on `resume` but not on the original `review` invocation (or vice versa)**
- **Given** a review started without `--axi` and resumed with `--axi` (or the reverse)
- **Then** each command's output mode is determined independently by its own invocation's flags — axi mode is not persisted into the review's on-disk manifest, so mixing modes across `review`/`resume` invocations is well-defined (each command's own stdout obeys its own flag) rather than producing undefined behavior

## Error Conditions
**Error Scenario 1: Empty-union usage error under `--axi`**
- **Given** `atcr resume --axi` hits the `fanout.ErrEmptyRoster` usage-error path (resume.go line 202-204)
- **Then** the usage error still surfaces via the existing exit-2 mechanism (stderr), unaffected by axi mode — axi mode governs stdout payload shape only, never the exit-code/stderr diagnostic contract (explicitly out of scope per the story's Constraints section)
- Error message: unchanged existing wrapped `fanout.ErrEmptyRoster` message

## Performance Requirements
- **Response Time:** Context read/write for the axi-mode value adds negligible overhead (<1ms), consistent with the existing logger/telemetry context injection.
- **Throughput:** No change.

## Security Considerations
- **Authentication/Authorization:** None.
- **Input Validation:** None beyond what AC 01-01/01-02/01-03 already establish for the shared `writeReviewSummary`/axi-encoding path — this AC is about propagation plumbing, not new data handling.

## Test Implementation Guidance
**Test Type:** INTEGRATION (cobra command execution for both `review` and `resume`, asserting identical payload shape)
**Test Data Requirements:** A fixture review directory with a partially-completed manifest (some agents done, some pending) to drive the `resume` path through `writeReviewSummary`.
**Mock/Stub Requirements:** Mock/stub `fanout.ExecuteResume` the same way existing resume tests do; assert the axi-mode context value set by `PersistentPreRunE` is correctly read at the `writeReviewSummary` call site without a second flag parse.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/...`)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `atcr resume --axi` and `atcr review --axi` emit byte-identical payload shapes for equivalent summary data
- [ ] axi-mode value is propagated via `PersistentPreRunE` context injection, not re-parsed independently in `review.go` and `resume.go`
- [ ] The `AllComplete()` short-circuit branch in `resume.go` is also axi-gated
- [ ] Exit-code/stderr behavior is unchanged by `--axi` (confirmed by an error-path test)

**Manual Review:**
- [ ] Code reviewed and approved
