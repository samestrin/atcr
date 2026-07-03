# Acceptance Criteria: Gate Passes Silently When Fully Configured, No Overhead Into Story 1

**Related User Story:** [06: Opt In via `--auto-fix` with a Refuse-Without-Backend Gate](../user-stories/06-opt-in-auto-fix-flag-with-refuse-without-backend-gate.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go gate function (`cmd/atcr/autofix.go`, same `validateAutoFixBackend` as AC 06-02) — success path | Mirrors `resolveExec`'s nil-error return when `proj.Sandbox` is present and preflight-shaped (`cmd/atcr/verify.go:45-58`) |
| Test Framework | Go `testing` + `testify/require`; integration test asserting control reaches Story 1's apply entry point | Follows `cmd/atcr/verify_test.go`'s harness conventions |
| Key Dependencies | None beyond the gate function itself and Story 1's apply entry point (`internal/autofix`, once it exists) | No new dependency |

## Related Files
- `cmd/atcr/autofix.go` - modify: the success path of `validateAutoFixBackend` — returns `nil` when apply target, validation command, and GitHub token/repo shape are all present and valid, with no side effect (no log line required beyond normal verbose/debug logging conventions already used elsewhere in `cmd/atcr`)
- `cmd/atcr/autofix_test.go` - modify: add the fully-configured "gate passes" case to the same table-driven suite as AC 06-02's failure cases, asserting `err == nil` and (via an integration test) that control proceeds into Story 1's apply call
- `cmd/atcr/review.go` - reference: the call site where `validateAutoFixBackend`'s `nil` return allows the `--auto-fix` flow to continue unimpeded into Story 1's apply step, exactly as `resolveExec`'s non-error return allows `--verify`/`--exec` to continue (`cmd/atcr/review.go:157-161`)
- `internal/autofix/` - reference (does not yet exist prior to Story 1): this AC's success path is the hand-off boundary — Story 1's apply entry point is called with the gate's already-resolved apply target once Story 1 lands, but no autofix package code executes as part of this AC itself

## Happy Path Scenarios
**Scenario 1: All three pieces present and valid — gate returns nil**
- **Given** `--auto-fix` is passed with a valid apply target, a configured validation command (Story 2), and both `GITHUB_TOKEN`/`--token` and a valid `owner/name` `--repo`/`GITHUB_REPOSITORY` present
- **When** `validateAutoFixBackend` runs
- **Then** it returns `nil`, produces no error output, and does not write to `.atcr/reviews/<id>/` or any other output path itself — control returns to the caller with no observable side effect from the gate

**Scenario 2: Gate success allows the flow to proceed into Story 1's apply step**
- **Given** the gate has returned `nil` for a fully-configured backend
- **When** the `--auto-fix` flow continues past the gate call site in `cmd/atcr/review.go`
- **Then** Story 1's apply entry point (`internal/autofix`, once implemented) is invoked with the exact apply target the gate resolved — no re-resolution, no duplicate validation of the same three pieces

**Scenario 3: Flags/env resolved via `--repo`/`--token` explicit flags take precedence over env vars, consistent with `envOr`**
- **Given** both `--token`/`--repo` flags AND `GITHUB_TOKEN`/`GITHUB_REPOSITORY` env vars are set to different values
- **When** the gate resolves GitHub credentials via `envOr` (`cmd/atcr/github.go:66-72`)
- **Then** the flag values win (matching `runGithub`'s existing precedence), and the gate validates the flag-provided values' shape, not the env-provided ones

## Edge Cases
**Edge Case 1: Gate re-run is idempotent**
- **Given** the gate is called once per `--auto-fix` invocation (not looped or re-invoked per fix item)
- **When** a single `--auto-fix` run processes multiple technical-debt items across Stories 1-5's chain
- **Then** `validateAutoFixBackend` executes exactly once at the top of the flow — its success does not need to be re-checked per item, and no per-item overhead is introduced by this gate

**Edge Case 2: Whitespace-only or trailing-slash values still resolve correctly**
- **Given** a `GITHUB_REPOSITORY` value with incidental leading/trailing whitespace (e.g. from a CI environment variable export)
- **When** the gate resolves it via `envOr`/`parseRepo` (which already `strings.TrimSpace`s, per `cmd/atcr/github.go:67-81`)
- **Then** the gate passes on the trimmed, correctly-shaped value — no false-negative refusal from incidental whitespace

**Edge Case 3: Gate success on a dry-run-equivalent invocation**
- **Given** no `--dry-run` flag exists for `--auto-fix` in this story's scope (out of scope per the story's Assumptions)
- **When** the gate passes
- **Then** control unconditionally proceeds into Story 1's real apply step — this AC does not define or gate any dry-run/no-op mode

## Error Conditions
**Error Scenario 1: N/A for this AC**
- This AC covers only the fully-configured success path; every failure mode is covered by AC 06-02. No new error path exists here — the absence of an error return IS the behavior under test.

## Performance Requirements
- **Response Time:** Successful gate evaluation completes in low single-digit milliseconds (same local-only checks as the failure path in AC 06-02, no network I/O) — "no observable overhead" per the story's AC3 wording means this is negligible relative to Story 1-5's subsequent apply/validate/GitHub-API work
- **Throughput:** One gate evaluation per `--auto-fix` invocation, called exactly once regardless of how many fix items the subsequent chain processes

## Security Considerations
- **Authentication/Authorization:** The gate's success path still never transmits the GitHub token over the network — it hands the already-resolved token to Story 4/5's GitHub client construction, which is the first point any network call using it occurs
- **Input Validation:** Same trimmed/shape-validated values from AC 06-02 flow forward unchanged into Stories 1-5; the gate does not re-parse or mutate them a second time downstream

## Test Implementation Guidance
**Test Type:** UNIT (gate function returns `nil` for a fully-valid input table) + INTEGRATION (full `atcr review --auto-fix ...` invocation with a fully-configured fixture, asserting the flow reaches Story 1's apply call — via a test stub/spy on the apply entry point until Story 1 lands, or an explicit "flow proceeded past gate" marker if Story 1 is not yet merged)
**Test Data Requirements:** A fully-configured `.atcr/config.yaml` fixture (validation command block from Story 2) plus `GITHUB_TOKEN`/`GITHUB_REPOSITORY` env vars or `--token`/`--repo` flags, plus a valid apply-target temp directory
**Mock/Stub Requirements:** If Story 1's `internal/autofix` apply entry point does not yet exist at the time this AC is implemented, stub the call site with a test-only marker/spy so this AC's "gate passes, flow proceeds" assertion does not hard-depend on Story 1's landing order; replace with a real call-through assertion once Story 1 merges

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A fully-configured backend (valid apply target, validation command, GitHub token/repo shape) makes `validateAutoFixBackend` return `nil` with no error output and no filesystem/network side effect
- [ ] A test confirms the `--auto-fix` flow proceeds past the gate call site into Story 1's apply entry point (or its stub) exactly once per invocation, not once per fix item
- [ ] Gate evaluation on the success path completes with no measurable overhead (no network I/O, no repeated re-validation) relative to the subsequent Story 1-5 work

**Manual Review:**
- [ ] Code reviewed and approved
