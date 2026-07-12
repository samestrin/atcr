# Acceptance Criteria: `--no-local-debt` Opt-Out Flag

**Related User Story:** [02: Reconcile-Time Persistence Hook](../user-stories/02-reconcile-time-persistence-hook.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (Cobra CLI flag) | `cmd/atcr/reconcile.go`, `newReconcileCmd` + `runReconcile` |
| Test Framework | `go test` + `testify/require` | Mirrors the existing `--no-scorecard` flag tests (`cmd/atcr/reconcile_test.go:257-299`) |
| Key Dependencies | `github.com/spf13/cobra` (already a dependency) | No new dependency |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/reconcile.go` — modify: register `cmd.Flags().Bool("no-local-debt", false, "skip writing reconciled findings to the local TD store")` in `newReconcileCmd()` alongside the existing `--no-scorecard` registration (line 43); read it in `runReconcile` via `cmd.Flags().GetBool("no-local-debt")` in the same style as the existing `noScorecard` read (line 110)
- `cmd/atcr/reconcile_test.go` — modify: add `TestRunReconcile_NoLocalDebtFlag` (writes zero records) and a `--help` listing assertion, mirroring `TestRunReconcile_NoScorecardFlag`-style tests at lines 257-299
- `internal/scorecard/scorecard.go` — reference (no change): `EmitOpts.NoScorecard` is the precedent gate shape (`type EmitOpts struct { NoScorecard bool; ... }`) the local-debt equivalent should structurally mirror when threaded into the store call
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/cli-integration-points.md` — reference: `--no-scorecard` flag precedent and expected hook behavior

## Happy Path Scenarios
**Scenario 1: Default behavior persists (flag unset)**
- **Given** a review directory with reconciled findings and no `--no-local-debt` flag passed
- **When** `atcr reconcile <review-dir>` runs
- **Then** the local TD store receives the run's findings (persistence-on is the default), identical to omitting `--no-scorecard`

**Scenario 2: `--no-local-debt` suppresses persistence for a single run**
- **Given** a review directory with reconciled findings
- **When** `atcr reconcile --no-local-debt <review-dir>` runs
- **Then** zero new records are written to `.atcr/debt/*.jsonl` (verified by comparing record counts before/after, or by absence of the directory entirely on a fresh repo), while the command's exit code and stdout summary are otherwise unaffected

**Scenario 3: Flag appears in `--help` output**
- **Given** the `atcr reconcile --help` command
- **When** invoked
- **Then** the output lists `--no-local-debt` with help text describing it skips writing to the local TD store, following the same phrasing register as `--no-scorecard`'s "skip writing scorecard records to the local store"

## Edge Cases
**Edge Case 1: Both `--no-scorecard` and `--no-local-debt` passed together**
- **Given** `atcr reconcile --no-scorecard --no-local-debt <review-dir>`
- **When** invoked
- **Then** both side effects are independently suppressed — zero scorecard records AND zero local-debt records — with no interaction or ordering dependency between the two flags

**Edge Case 2: `--no-local-debt` passed with no findings to persist**
- **Given** a reconcile run that produces zero findings, with `--no-local-debt` also set
- **When** invoked
- **Then** behavior is identical to the flag being absent in this case (already a no-op) — no error, no spurious "flag had no effect" message

## Error Conditions
**Error Scenario 1: Unrecognized flag value**
- Error message: standard Cobra "invalid argument ... for '--no-local-debt'" boolean-parse error (e.g., `--no-local-debt=notabool`)
- HTTP status / error code: usage error, exit 2 (Cobra's default flag-parse failure behavior — unchanged from how `--no-scorecard` already behaves)

## Performance Requirements
- **Response Time:** Flag read is a single `cmd.Flags().GetBool` call — negligible, no measurable overhead
- **Throughput:** N/A — flag gates an I/O path, not a data volume

## Security Considerations
- **Authentication/Authorization:** N/A — local CLI flag, no privilege implication
- **Input Validation:** Cobra's built-in boolean flag parsing handles validation; no custom parsing introduced

## Test Implementation Guidance
**Test Type:** INTEGRATION (CLI command execution via the existing `execCmd`/`execCmdCapture` test helpers in `cmd/atcr/reconcile_test.go`)
**Test Data Requirements:** Same fixture review directory used for the `--no-scorecard` tests; reused/extended rather than duplicated
**Mock/Stub Requirements:** None — real temp directories, real flag parsing, matching existing test conventions

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `--no-local-debt` flag registered in `newReconcileCmd()`, defaulting to `false` (persistence-on)
- [ ] Setting the flag suppresses local-debt persistence and writes zero new records
- [ ] The flag is independent of `--no-scorecard` — either can be set without affecting the other
- [ ] `--help` output lists `--no-local-debt` with descriptive help text

**Manual Review:**
- [ ] Code reviewed and approved
