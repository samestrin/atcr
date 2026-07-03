# Acceptance Criteria: `--auto-fix` Off by Default, Zero Behavior Change When Absent

**Related User Story:** [06: Opt In via `--auto-fix` with a Refuse-Without-Backend Gate](../user-stories/06-opt-in-auto-fix-flag-with-refuse-without-backend-gate.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI flag registration (`spf13/cobra`) | Mirrors `cmd.Flags().Bool("exec", false, ...)` at `cmd/atcr/review.go:47` |
| Test Framework | Go `testing` package + `testify/require`, cobra command execution with captured exit code/output | Follows `cmd/atcr/verify_test.go`'s `findVerifyCmd`/help-text-contains style (e.g. `TestVerifyCmd_Exists`, `cmd/atcr/verify_test.go:116-122`) |
| Key Dependencies | `github.com/spf13/cobra` (already a dependency) | No new dependency |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/review.go` - modify: add `cmd.Flags().Bool("auto-fix", false, "...")` alongside the existing `--exec`/`--verify`/`--debate` flags (near line 47), off by default
- `cmd/atcr/review_test.go` - modify: add a test asserting the `review` command's help text contains `--auto-fix` and that its default is `false` when unset
- `cmd/atcr/review.go` (existing `runReview` body, `internal/verify`, `internal/fanout`, `internal/reconcile` call sites) - reference: none of the existing review/verify/reconcile/github code paths may branch on `--auto-fix`'s absence differently than they did before this story
- `internal/autofix/` - reference (does not yet exist): this AC confirms no code under this future package is ever imported or reachable when `--auto-fix` is unset

## Happy Path Scenarios
**Scenario 1: Flag defaults to false and is absent from every non-review command**
- **Given** a fresh `atcr` binary built with this story's change
- **When** any existing command (`atcr review`, `atcr verify`, `atcr reconcile`, `atcr github`) is run without `--auto-fix`
- **Then** `cmd.Flags().GetBool("auto-fix")` (where the flag is registered) resolves to `false`, and no `--auto-fix` flag appears on commands other than the one it is registered on

**Scenario 2: Existing `atcr review` invocations are byte-identical**
- **Given** a repo state and `atcr review` invocation that produced a known review directory tree before this story shipped
- **When** the same invocation (still without `--auto-fix`) is run against the post-story binary
- **Then** the produced review directory tree (findings, manifest, reconciled output) is byte-identical to the pre-story baseline — no new files, no new fields, no altered exit code

**Scenario 3: Flag is discoverable via `--help`**
- **Given** the updated `atcr review --help` output
- **When** a user inspects the flag list
- **Then** `--auto-fix` is listed with a description explaining it is opt-in and requires a fully configured backend, consistent with how `--exec` documents its own refuse-without-backend behavior (`cmd/atcr/review.go:47`)

## Edge Cases
**Edge Case 1: `--auto-fix=false` explicitly passed**
- **Given** a user explicitly passes `--auto-fix=false`
- **When** the command runs
- **Then** behavior is identical to omitting the flag entirely — no partial gate evaluation occurs

**Edge Case 2: `--auto-fix` combined with other one-shot flags (`--verify`, `--exec`, `--debate`) while itself false**
- **Given** `atcr review --verify --exec --debate` is run without `--auto-fix`
- **Then** the existing `--verify`/`--exec`/`--debate` chain behaves exactly as it did before this story — the new flag's registration must not alter cobra's flag-parsing or default-value resolution for any pre-existing flag

**Edge Case 3: Flag registered on the wrong command by mistake**
- **Given** the design-sprint decision on where `--auto-fix` lives (per the story's Implementation Notes, most likely `atcr review`, alternatively a new `atcr autofix` subcommand)
- **When** `--auto-fix` is passed to any command it is NOT registered on
- **Then** cobra's standard "unknown flag" usage error fires (exit 2) — this is existing cobra behavior, not new logic this AC must implement, but it must not be suppressed or worked around

## Error Conditions
**Error Scenario 1: N/A for this AC**
- This AC covers only the off-by-default/no-op case; missing-backend refusal is covered by AC 06-02. No new error path is introduced by flag registration alone.

## Performance Requirements
- **Response Time:** Flag registration and default resolution add O(1) overhead (a single boolean flag lookup) — no measurable startup or execution latency change versus the pre-story baseline
- **Throughput:** N/A — flag parsing is a one-time, per-invocation cost identical to every other existing boolean flag on the command

## Security Considerations
- **Authentication/Authorization:** N/A — no credential or network surface is touched when the flag is absent or false
- **Input Validation:** Boolean flag parsing is delegated entirely to `spf13/cobra`'s existing, already-hardened flag parser; no custom parsing logic is introduced

## Test Implementation Guidance
**Test Type:** UNIT (flag registration/default) + INTEGRATION (byte-identical-behavior regression, following `cmd/atcr/verify_test.go`'s pattern of building a `cobra.Command`, executing it with `SetArgs`, and asserting on captured exit code/output)
**Test Data Requirements:** A minimal git repo fixture (existing `cmd/atcr` test helpers already construct one, e.g. `cmd/atcr/verify_test.go:40-60`) run once pre-flag and once post-flag registration to diff output trees
**Mock/Stub Requirements:** None — this is pure CLI wiring; no network, filesystem mutation beyond the existing review output, or sandbox/GitHub mocking is needed for this AC specifically

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `--auto-fix` defaults to `false` and is absent from every command's behavior when unset, verified by a help-text-contains test mirroring `TestVerifyCmd_Exists`
- [ ] A regression test confirms an existing `atcr review` invocation without `--auto-fix` produces output identical to the pre-story baseline
- [ ] No code path under `internal/autofix` (or equivalent) is reachable unless `--auto-fix` is explicitly `true`

**Manual Review:**
- [ ] Code reviewed and approved
