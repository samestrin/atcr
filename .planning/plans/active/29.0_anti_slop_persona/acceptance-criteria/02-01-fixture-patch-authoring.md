# Acceptance Criteria: Fixture Patch Authoring

**Related User Story:** [2: Fixture Authoring & Test-Gate Integration](../user-stories/02-fixture-authoring-test-gate-integration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Static test-fixture data (unified diff) | Not Go code; consumed by `personas.TemplateFixtureRunner` and `personas_test.go` |
| Test Framework | `go test` + `testify/require` | Consumed indirectly by `TestCommunityPersonas_FixtureAndPromptCategory` and `TestTemplateFixtureRunner_CommunityPersonasPass` |
| Key Dependencies | None (plain-text `diff --git` unified-diff format) | Must parse the same way `personas/community/testdata/anthony_fixture.patch` does |

## Related Files
- `personas/community/testdata/simon_fixture.patch` - create: synthetic unified diff planting a known instance of AI-generated code bloat (slop) for `simon` to detect
- `personas/community/testdata/anthony_fixture.patch` - reference only: existing fixture whose structural pattern (single-purpose diff, one planted violation, explanatory comment naming the issue) this file must mirror
- `personas/community_test.go` - consumer: `TestCommunityPersonas_FixtureAndPromptCategory` (line ~202) loads this file via `communityPath("testdata", p.Slug+"_fixture.patch")` and renders it through `RenderPrompt`
- `internal/personas/community_fixture_test.go` - consumer: `TestTemplateFixtureRunner_CommunityPersonasPass` asserts `RunFixture("simon")` returns `HasFixture: true` and exactly one passing case

## Happy Path Scenarios
**Scenario 1: Fixture file exists at the correct community path**
- **Given** Story 1 has authored `personas/community/simon.yaml` and `personas/community/simon.md`
- **When** `personas/community/testdata/simon_fixture.patch` is created as a valid `diff --git` unified diff
- **Then** `os.ReadFile(communityPath("testdata", "simon_fixture.patch"))` in `personas/community_test.go` succeeds with no error

**Scenario 2: Fixture plants an unambiguous AI-slop violation**
- **Given** the fixture diff adds a small, single-purpose code change
- **When** the diff is authored to plant one obvious anti-slop instance — e.g. a single-implementation interface with no second implementer, or a tautological/apologetic AI-authored comment such as `// This function handles the logic for processing` — modeled directly on `anthony_fixture.patch`'s shape (one violation, one explanatory comment naming the issue)
- **Then** the fixture is narrow enough that `simon`'s rendered prompt (via `RenderPrompt`) contains no leftover `{{`/`}}` template actions and renders `AgentName` into the output, matching the `TestCommunityPersonas_FixtureAndPromptCategory` assertions applied to every other community persona

**Scenario 3: Fixture Runner reports exactly one passing case**
- **Given** `simon` is registered (Story 2 AC 02-02/02-03) and its fixture file exists
- **When** `TemplateFixtureRunner{}.RunFixture("simon")` executes
- **Then** the returned `FixtureOutcome` has `HasFixture: true`, `Total == Passed == 1`, matching the loop assertion in `TestTemplateFixtureRunner_CommunityPersonasPass` (`internal/personas/community_fixture_test.go`)

## Edge Cases
**Edge Case 1: Fixture too subtle to trigger slop detection**
- **Given** a fixture diff that resembles legitimate, idiomatic business logic rather than a planted slop instance
- **When** `simon`'s prompt is rendered against it
- **Then** this must be avoided by construction — the fixture must contain an unambiguous, single planted violation (interface-with-one-impl or tautological comment) rather than subtle or debatable code, following the `anthony_fixture.patch` precedent of one obvious issue per fixture

**Edge Case 2: Fixture diff resembles another persona's fixture category**
- **Given** 13 existing fixtures already cover coupling, logic, contract, validation, race, leak, complexity, type, dependency, observability, secret, duplication, and invariant issues
- **When** `simon_fixture.patch` is authored
- **Then** the planted violation must be an anti-slop/bloat instance (unnecessary abstraction or AI-tautological comment) distinct in kind from all 13 existing fixture categories, so it does not duplicate an existing persona's detection target

## Error Conditions
**Error Scenario 1: Fixture file missing**
- Error message: `"CommunityFixture(\"simon\")"` wrapped `require.NoErrorf` failure in `TestCommunityAccessors` (`personas/community_test.go:186`), and a hard file-not-found error from `os.ReadFile` in `TestCommunityPersonas_FixtureAndPromptCategory`
- HTTP status / error code: N/A (Go test failure — `os.ReadFile` returns `*fs.PathError` with `syscall.ENOENT`)

**Error Scenario 2: Fixture is not a valid unified diff**
- Error message: downstream `RenderPrompt` or fixture-runner parse failure surfaces as a non-nil `err` from `RunFixture("simon")` in `TestTemplateFixtureRunner_CommunityPersonasPass`, failing `require.NoError(t, err)`
- HTTP status / error code: N/A (Go test failure)

## Performance Requirements
- **Response Time:** Fixture file read + prompt render must complete within the existing per-persona subtest budget (`go test ./personas/...` full suite typically completes in well under 30s per package; no persona-specific extension is expected)
- **Throughput:** N/A — single static file read per test run, no concurrency requirements

## Security Considerations
- **Authentication/Authorization:** N/A — static in-repo test fixture, no runtime access control
- **Input Validation:** The fixture must be a syntactically valid unified diff (`diff --git a/... b/...` header, `@@` hunk markers) so it parses identically to the other 13 fixtures; no untrusted external input is involved since the file is authored and committed in-repo

## Test Implementation Guidance
**Test Type:** UNIT (existing tests in `personas/community_test.go` and `internal/personas/community_fixture_test.go` exercise this fixture automatically once the file exists and `simon` is registered — no new test code is required for this AC)
**Test Data Requirements:** The fixture file itself is the test data; author it as a `diff --git a/<path> b/<path>` unified diff with a `@@` hunk adding ~5-15 lines containing exactly one planted slop violation
**Mock/Stub Requirements:** None — no LLM call occurs; fixture parsing and template rendering are pure, deterministic, local operations

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `personas/community/testdata/simon_fixture.patch` exists and is a syntactically valid unified diff
- [ ] The diff plants exactly one unambiguous anti-slop violation (unnecessary single-implementation interface, or tautological/apologetic comment) distinct from all 13 existing fixture categories
- [ ] `TestTemplateFixtureRunner_CommunityPersonasPass` reports `HasFixture: true` and `Total == Passed == 1` for `simon`
- [ ] `TestCommunityPersonas_FixtureAndPromptCategory` renders `simon`'s prompt against this fixture with no leftover `{{`/`}}` actions

**Manual Review:**
- [ ] Code reviewed and approved
