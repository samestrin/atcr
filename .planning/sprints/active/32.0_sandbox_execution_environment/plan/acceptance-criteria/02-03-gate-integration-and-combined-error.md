# Acceptance Criteria: Gate Integration — Sandbox Resolution as the Fourth Piece of validateAutoFixBackend

**Related User Story:** [02: Sandbox Resolution and Preflight Gate for Auto-Fix](../user-stories/02-sandbox-resolution-and-preflight-gate.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI gate function `validateAutoFixBackend` (`cmd/atcr/autofix.go:107`), struct field addition on `autoFixBackend` (`cmd/atcr/autofix.go:59`) | Integration wiring only — no new resolver logic beyond calling `verify.ResolveAutoFixSandbox` (AC 02-01/02-02) |
| Test Framework | `go test` + `testify` (`assert`/`require`), table-driven, `fakeDocker` shim | Extends existing `validateAutoFixBackend` test coverage in `cmd/atcr` |
| Key Dependencies | `internal/verify.ResolveAutoFixSandbox`, `internal/sandbox.Backend`, `github.com/spf13/cobra` (`*cobra.Command` flags already threaded through) | Reuses the existing `missing []string` accumulation pattern verbatim |

## Related Files
- `cmd/atcr/autofix.go` - modify: add step (4) inside `validateAutoFixBackend` (`:107-191`) calling `verify.ResolveAutoFixSandbox`, appending to `missing` on failure and assigning to a new `autoFixBackend` field on success; add the new field to the `autoFixBackend` struct (`:59-67`).
- `cmd/atcr/autofix_test.go` (or equivalent existing test file for `validateAutoFixBackend`) - modify/create: table-driven test asserting the combined `missing` error names the sandbox failure alongside other missing pieces (apply-target, validation-command, GitHub-credential) in the same run.
- `internal/verify/autofix_exec.go` - reference (read-only): `ResolveAutoFixSandbox` signature and error shapes produced by AC 02-01/02-02, consumed here as a black box.
- `internal/registry/project.go` - reference (read-only): `ProjectConfig.Sandbox` field (`:85`) — already in scope at the `validateAutoFixBackend` call site via `proj *registry.ProjectConfig`, no new plumbing required.
- `cmd/atcr/review.go` - reference (read-only): `:353` already passes `cfg.Project` into the gate call chain, confirming `proj.Sandbox` reaches `validateAutoFixBackend` unchanged.

### Related Files (from codebase-discovery.json)

- `cmd/atcr/autofix.go:107` — update: `validateAutoFixBackend` gains sandbox resolution as gate step (4), appended to the same `missing []string`; the `autoFixBackend` struct (`:59`) gains the resolved `sandbox.Backend` field (discovery `files_to_modify`, scope major).
- `cmd/atcr/autofix_test.go` — update: table-driven gate tests asserting the combined usage error names the sandbox failure alongside other missing pieces (discovery `files_to_modify`, scope minor).

## Happy Path Scenarios
**Scenario 1: Sandbox resolution succeeds alongside the other three pieces**
- **Given** a fully valid `proj.SandboxConfig` (passing `fakeDocker` shim), a valid apply target, a resolvable validation command, and valid GitHub credentials
- **When** `validateAutoFixBackend(cmd, proj, repoRoot)` is called
- **Then** it returns a populated `autoFixBackend` with the resolved `sandbox.Backend` stored on its new field, `nil` error, and `runAutoFix`/`orchestrateAutoFix` can read that field directly without calling `ResolveAutoFixSandbox` again.

**Scenario 2: Sandbox failure is the only problem — gate still returns the standard usage error shape**
- **Given** every other piece (apply target, validation command, GitHub credentials) is valid, but `proj.Sandbox` is `nil` (unconfigured under the default sandbox-on posture)
- **When** `validateAutoFixBackend` runs
- **Then** it returns `autoFixBackend{}` and a `usageError` wrapping `fmt.Errorf("--auto-fix cannot run: %s", ...)` whose joined message names the sandbox failure — exit code 2, identical shape to the existing three-piece gate (`cmd/atcr/autofix.go:188-190`).

## Edge Cases
**Edge Case 1: Sandbox failure combines with other missing pieces in one error, not a separate early return**
- **Given** `proj.Sandbox` is `nil` AND the GitHub token is also missing (two independent failures in the same run)
- **When** `validateAutoFixBackend` runs
- **Then** the single returned error's joined `missing` text contains BOTH the sandbox-unconfigured message and the `"a GitHub token is required..."` message (`cmd/atcr/autofix.go:175`) — proving the sandbox check joins the same `missing []string` slice rather than returning early via a separate error path, per the story's explicit constraint ("must preserve the existing all-or-nothing gate contract").

**Edge Case 2: `autoFixBackend`'s new field is populated even though nothing downstream consumes it yet**
- **Given** a fully valid run (all four pieces succeed, including sandbox)
- **When** `validateAutoFixBackend` returns
- **Then** the new field on the returned `autoFixBackend` is non-nil/populated, verified directly by the test — even though `runAutoFix`'s call to `verify.RunConfiguredValidation` (`cmd/atcr/autofix.go:252`) does not yet route through it (that wiring is Story 1's scope) — guarding against the struct field going stale/unused before Story 1 lands (Risk 3 in the story's risk table).

**Edge Case 3: Call-site literal for `enabled` is `true` in this story (no `--no-sandbox` flag exists yet)**
- **Given** this story ships before Story 3's `--no-sandbox` flag
- **When** `validateAutoFixBackend` calls `verify.ResolveAutoFixSandbox(ctx, true, proj.Sandbox)` (hard-coded `true`, not yet reading a flag)
- **Then** every `--auto-fix` invocation on a project without a valid, preflighted sandbox configuration hard-refuses at the gate — the intended fail-closed behavior change the story's risk table flags as needing Story 3's escape hatch to land "immediately alongside," not deferred indefinitely.

## Error Conditions
**Error Scenario 1: Combined usage error naming the sandbox failure**
- Error message: `"--auto-fix cannot run: <apply-target msg (if any)>; <validation-command msg (if any)>; sandbox: <ResolveAutoFixSandbox error text>; <github msg (if any)>"` (exact ordering follows the existing four-step sequence; the assertion checks substring containment, not exact ordering, matching the existing test style for `validateAutoFixBackend`).
- HTTP status / error code: exit code 2 (`usageError` wrapping, consistent with the existing three-piece gate's contract at `cmd/atcr/autofix.go:189`).

**Error Scenario 2: Preflight failure surfaces through the same combined path**
- **Given** `proj.Sandbox` is non-nil but points at a `fakeDocker` shim that fails preflight
- **When** `validateAutoFixBackend` runs
- **Then** the combined error contains the `"preflight"`-bearing message from AC 02-01, proving the CLI-level gate does not swallow or reformat the resolver's error in a way that loses the diagnostic detail.

## Performance Requirements
- **Response Time:** Adding the sandbox check must not change `validateAutoFixBackend`'s "local-only, no network call" contract (`cmd/atcr/autofix.go:93-95`) — `Preflight` invokes local `docker` subprocess calls only, no remote API calls, consistent with the gate's existing performance envelope.
- **Throughput:** N/A (one gate evaluation per `--auto-fix` invocation).

## Security Considerations
- **Authentication/Authorization:** N/A — no new credential surface; GitHub token/repo checks (`cmd/atcr/autofix.go:167-186`) are unchanged by this story.
- **Input Validation:** The gate must continue to perform zero mutating operations (no patch apply, no GitHub call) until ALL four checks pass — the sandbox check must be added as a pure local check (append-to-`missing`-or-assign-to-`be`) with no side effects, preserving the "no file touched before the combined gate passes" invariant the docstring already states (`cmd/atcr/autofix.go:103-106`).

## Test Implementation Guidance
**Test Type:** INTEGRATION (exercises `validateAutoFixBackend` end-to-end with a `*cobra.Command` and `fakeDocker`-backed `proj.Sandbox`, but no live Docker/GitHub — still hermetic)
**Test Data Requirements:** Table cases crossing {sandbox valid, sandbox nil, sandbox preflight-fails} × {other-three-valid, one-other-piece-also-missing}.
**Mock/Stub Requirements:** `fakeDocker` shim for the sandbox dimension; existing `cmd/atcr` test fixtures/flag-setting helpers for apply-target/validation-command/GitHub-credential dimensions (reuse whatever the current three-piece gate tests already use — no new mocking framework).

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `validateAutoFixBackend` calls `verify.ResolveAutoFixSandbox` as step (4), joining failures into the same `missing []string` slice
- [x] A combined-failure test proves the sandbox message appears alongside at least one other missing-piece message in a single returned error
- [x] The resolved `sandbox.Backend` is stored on a new `autoFixBackend` field and asserted populated on the success path
- [x] No new early-return error path is introduced — the sandbox check follows the append-or-assign pattern of the other three checks

**Manual Review:**
- [x] Code reviewed and approved
