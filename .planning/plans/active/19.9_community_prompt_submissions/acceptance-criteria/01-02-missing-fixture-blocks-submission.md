# Acceptance Criteria: Missing Fixture Blocks Submission

**Related User Story:** [01: Local Fixture-Gate Reuse and Submission Blocking](../user-stories/01-local-fixture-gate-reuse-and-submission-blocking.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cobra CLI subcommand (`RunE`) | `newPersonasSubmitCmd()` in cmd/atcr/personas.go |
| Test Framework | Go `testing` + `testify` | Stub `commpersonas.FixtureRunner` via `withFixtureRunner` helper (cmd/atcr/personas_test.go:631) |
| Key Dependencies | `internal/personas.TestPersona` (test.go:195), `internal/personas.FixtureOutcome` (test.go:14) | Reused verbatim; no new fixture logic |

## Related Files
- `cmd/atcr/personas.go` - modify: `newPersonasSubmitCmd()`'s `RunE` calls `commpersonas.TestPersona(name, personasFixtureRunner)` and checks `!outcome.HasFixture` as an explicit blocking branch (distinct wording from `personas test`'s informational "No fixture defined for persona %q" at personas.go:314)
- `internal/personas/test.go` - reference only (no change): `TestPersona` (line 195) and `FixtureOutcome.HasFixture` (line 15); `TemplateFixtureRunner.RunFixture` (line 38) is the concrete runner that yields `HasFixture: false` for a persona with no committed fixture (e.g. test.go:43, test.go:69, test.go:81, test.go:102)
- `cmd/atcr/personas_test.go` - modify: add `TestPersonasSubmit_NoFixture` using `stubFixtureRunner{personas.FixtureOutcome{HasFixture: false}}` (pattern at line 662) and `executeSplit`

## Happy Path Scenarios
**Scenario 1: Persona with a defined, passing fixture is not blocked by this AC's check**
- **Given** `outcome.HasFixture == true` (any Passed/Total values)
- **When** `atcr personas submit <name>` runs the fixture gate
- **Then** this AC's "no fixture" branch is skipped and control proceeds to the pass/fail evaluation (AC 01-03)

## Edge Cases
**Edge Case 1: Built-in persona with no embedded fixture**
- **Given** a built-in persona name for which `builtins.Fixture(name)` returns an error (test.go:41-44), so `TemplateFixtureRunner.RunFixture` returns `FixtureOutcome{HasFixture: false}, nil`
- **When** `atcr personas submit <builtin-name>` is invoked
- **Then** `RunE` writes a distinct, submission-specific message to stderr (not the `test` command's softer "No fixture defined" wording) and returns non-nil; no fork/PR/`gh` call occurs

**Edge Case 2: Community-library persona with no embedded fixture patch**
- **Given** a community-library name resolves via `builtins.CommunityGet` but `builtins.CommunityFixture(name)` errors (test.go:100-103), yielding `HasFixture: false`
- **When** `atcr personas submit <library-name>` is invoked
- **Then** the same distinct blocking message is written to stderr and `RunE` returns non-nil; no fork/PR/`gh` call occurs

**Edge Case 3: Name resolves to neither a built-in nor a library persona (post name-validation)**
- **Given** the name passes `validatePersonaName`/`personaPath` but is not a built-in and has no embedded community template (test.go:79-82), so the runner returns `HasFixture: false` with a nil error
- **When** `atcr personas submit <unknown-name>` is invoked
- **Then** `RunE` treats this the same as any other `HasFixture: false` outcome — a submission-blocking, non-zero-exit stderr message — since `TestPersona` does not distinguish "not found" from "no fixture" in its return value

## Error Conditions
**Error Scenario 1: No fixture defined**
- Error message: `cannot submit "<name>": no fixture defined — add a fixture before submitting` (per the story's risk-mitigation wording at user-stories/01-local-fixture-gate-reuse-and-submission-blocking.md:52, deliberately distinct from `personas test`'s "No fixture defined for persona %q")
- Exit code: non-zero; written to `cmd.ErrOrStderr()` only, never stdout

**Error Scenario 2: `TestPersona`/runner returns a non-nil error (e.g. a broken installed unit's YAML fails to parse, or `assertBoundModel` fails)**
- Error message: propagate the wrapped error from `TestPersona`/`TemplateFixtureRunner.RunFixture` (e.g. `persona "<name>": bound model missing from structured metadata`) to stderr
- Exit code: non-zero; no fork/PR/`gh` call occurs (this error path returns before any fixture-gate pass/fail evaluation is reached)

## Performance Requirements
- **Response Time:** The fixture check must complete via local template render only — no network, no LLM call — consistent with `TemplateFixtureRunner`'s existing zero-network contract; sub-second in practice.
- **Throughput:** N/A (single-invocation CLI command).

## Security Considerations
- **Authentication/Authorization:** None consulted — the fixture gate runs entirely locally and must not read `gh`/GitHub credentials before this check passes.
- **Input Validation:** N/A beyond AC 01-01's name validation, which must run first; this AC assumes a validated name has already reached the fixture gate.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `personas.FixtureOutcome{HasFixture: false}` via a stub runner; optionally a second case where the stub runner returns a non-nil error to exercise Error Scenario 2.
**Mock/Stub Requirements:** Inject a stub `commpersonas.FixtureRunner` via `withFixtureRunner` (cmd/atcr/personas_test.go:631) — do not exercise the production `TemplateFixtureRunner` for this AC's negative cases, matching the existing `TestPersonasTest_NoFixture` pattern (personas_test.go:659-667). Assert stderr contains the distinct "no fixture defined — add a fixture before submitting" wording, not the `test` command's wording, and assert zero fork/PR/`gh` side effects.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `RunE` checks `!outcome.HasFixture` immediately after calling `TestPersona` and returns a distinct, submission-specific stderr message (not `personas test`'s wording)
- [ ] Test asserts the "no fixture" case exits non-zero with the exact expected message and zero fork/PR/`gh` side effects
- [ ] Test covers a `TestPersona`/runner error path (non-nil `err`) separately from the `HasFixture: false`, nil-error path

**Manual Review:**
- [ ] Code reviewed and approved
