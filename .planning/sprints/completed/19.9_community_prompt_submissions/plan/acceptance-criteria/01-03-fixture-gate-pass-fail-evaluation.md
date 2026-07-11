# Acceptance Criteria: Fixture Gate Pass/Fail Evaluation Gates Progression

**Related User Story:** [01: Local Fixture-Gate Reuse and Submission Blocking](../user-stories/01-local-fixture-gate-reuse-and-submission-blocking.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cobra CLI subcommand (`RunE`) | `newPersonasSubmitCmd()` in cmd/atcr/personas.go |
| Test Framework | Go `testing` package | Stub `commpersonas.FixtureRunner` via `withFixtureRunner`; `executeSplit` for stdout/stderr separation; testify is not used in this codebase |
| Key Dependencies | `internal/personas.TestPersona`, `internal/personas.FixtureOutcome{Passed, Total}` | Reused verbatim; no reimplementation of pass/fail logic |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/personas.go` (modify) — `newPersonasSubmitCmd()`'s `RunE` evaluates `outcome.Passed != outcome.Total` as the blocking condition (after the `!outcome.HasFixture` check in AC 01-02) and, on a full pass, proceeds to a stubbed/no-op continuation point reserved for Theme 2's fork/PR call (per Implementation Notes in the user story)
- `internal/personas/test.go` (reference only) — `FixtureOutcome{Passed, Total}` (lines 14-18), `TestPersona` (line 195)
- `cmd/atcr/personas_test.go` (modify) — add `TestPersonasSubmit_FixtureFails` and `TestPersonasSubmit_FixturePasses` using `stubFixtureRunner` and `executeSplit`, mirroring `TestPersonasTest_FailExitsNonZero` (personas_test.go:648-657) and `TestPersonasTest_Pass` (personas_test.go:638-646)

## Design References
- [Local Fixture-Gate Reuse (TestPersona)](../documentation/fixture-gate-reuse.md) — the `TestPersona`/`FixtureOutcome` contract and the zero-network, zero-LLM gate `submit` must run before any GitHub call

## Happy Path Scenarios
**Scenario 1: Fully passing fixture proceeds past the local gate**
- **Given** `outcome.HasFixture == true` and `outcome.Passed == outcome.Total` (e.g. `Passed: 3, Total: 3`)
- **When** `atcr personas submit <name>` is invoked
- **Then** `RunE` does not return a non-nil error from the fixture-gate check and control reaches the stubbed/no-op continuation point reserved for Theme 2's fork/PR logic; no error is written to stderr from this check

**Scenario 2: Zero-case fixture (Total == 0) is treated consistently with the gate's pass condition**
- **Given** `outcome.HasFixture == true` and `outcome.Total == 0` (an edge state `personas test` reports as a stderr WARN but not a failure, at personas.go:316-318)
- **When** `atcr personas submit <name>` is invoked
- **Then** the story/plan owner's chosen behavior is applied consistently: since `Passed == Total` (`0 == 0`) under the story's stated condition (`Passed != Total` is the sole failing predicate), this case does NOT trip the failure branch and proceeds like Scenario 1 — implementers must document this explicit choice inline (matching the story's Constraint that only `outcome.Passed != outcome.Total` blocks) rather than silently diverging from `personas test`'s WARN treatment

## Edge Cases
**Edge Case 1: Partial failure reports pass/fail counts**
- **Given** `outcome.HasFixture == true` and `outcome.Passed < outcome.Total` (e.g. `Passed: 2, Total: 3`)
- **When** `atcr personas submit <name>` is invoked
- **Then** `RunE` writes a stderr message reporting the exact pass/fail counts (e.g. `cannot submit "<name>": fixture failed (2/3 cases passed)`) and returns a non-nil error; no fork/PR/`gh` call occurs

**Edge Case 2: Complete failure (Passed == 0)**
- **Given** `outcome.HasFixture == true`, `outcome.Passed == 0`, `outcome.Total == 1` (e.g. a template that dropped all `{{ }}` substitutions, per `renderFixture`'s AgentName check at test.go:169-172)
- **When** `atcr personas submit <name>` is invoked
- **Then** `RunE` writes the pass/fail-count stderr message (`0/1 cases passed`) and returns non-nil; no fork/PR/`gh` call occurs

**Edge Case 3: Fixture-gate check is the first statement after name/path resolution (no bypass path)**
- **Given** the story's Potential Risks table flags a future contributor adding fork/PR logic before the gate check
- **When** the code is reviewed/tested
- **Then** a unit test asserts that for every non-passing outcome (`HasFixture: false` OR `Passed != Total`), zero `gh`-related side effects occur — enforced structurally by placing the gate check immediately after `TestPersona` returns and before any stubbed continuation logic

## Error Conditions
**Error Scenario 1: Fixture fails (partial or complete)**
- Error message: `cannot submit "<name>": fixture failed (<passed>/<total> cases passed)` (exact pass/fail counts required, per Success Criteria "Measurable")
- Exit code: non-zero; written to `cmd.ErrOrStderr()` only, never stdout

## Performance Requirements
- **Response Time:** The full local gate (name validation + fixture render) must complete without network or LLM calls — sub-second in practice, matching `TemplateFixtureRunner`'s existing zero-network, zero-LLM contract.
- **Throughput:** N/A (single-invocation CLI command).

## Security Considerations
- **Authentication/Authorization:** No `gh`/GitHub credentials are read or consulted at any point in this story's scope — the gate must fully evaluate and potentially fail before any Theme 2 fork/PR code path (not present in this story) could run.
- **Input Validation:** N/A beyond AC 01-01/01-02, which must both pass before this AC's pass/fail evaluation is reached.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `personas.FixtureOutcome{HasFixture: true, Passed: 3, Total: 3}` (full pass), `personas.FixtureOutcome{HasFixture: true, Passed: 2, Total: 3}` (partial failure), `personas.FixtureOutcome{HasFixture: true, Passed: 0, Total: 1}` (complete failure).
**Mock/Stub Requirements:** Inject `stubFixtureRunner` via `withFixtureRunner` (cmd/atcr/personas_test.go:631) for all three outcome shapes; for the full-pass case, assert the command reaches (but does not execute) the stubbed continuation point — e.g. via a package-level spy/flag Theme 2 will later wire to the real fork/PR call — and does not return a non-nil error from the fixture-gate check itself. For failure cases, assert stderr contains the exact pass/fail counts and the command returns a non-nil error with zero fork/PR/`gh` side effects (`assert.False` on a spy invocation flag, matching Edge Case 3 in the story's Potential Risks table).

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `RunE` checks `outcome.Passed != outcome.Total` immediately after the `!outcome.HasFixture` check (AC 01-02) and before any continuation logic, matching the exact ordering in the story's Implementation Notes (in `SubmitGate`)
- [x] Failure-path stderr message includes the literal pass/fail counts (`cannot submit %q: fixture failed (%d/%d cases passed)`)
- [x] Full-pass test confirms the command proceeds past the gate without invoking any fork/PR/`gh` side effect (`TestPersonasSubmit_FixturePasses`; continuation spy asserted true). Zero-case `Total==0` proceeds per Scenario 2 (`TestPersonasSubmit_ZeroCaseFixtureProceeds`), documented inline in `submit.go`
- [x] Test asserts zero `gh`-related side effects for every non-passing outcome (partial and complete failure) (`TestPersonasSubmit_FixtureFails`)

**Manual Review:**
- [ ] Code reviewed and approved
