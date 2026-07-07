# Acceptance Criteria: `ingrid` — Generalizing `idiomatic`'s Go-Specific Lens

**Related User Story:** [05: Human-Names Migration for Built-in Stragglers](../user-stories/05-human-names-migration-for-built-in-stragglers.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown prompt template (`personas/idiomatic.md` → `personas/ingrid.md`) | Content rewrite, not a mechanism change |
| Test Framework | Go `testing` package fixture test (`TestPersona`/`TemplateFixtureRunner`) | Verifies the rewritten template still renders and still catches the fixture's target category |
| Key Dependencies | Existing `payload.RenderPrompt` template engine; `{{.AgentName}}`/`{{.ScopeRule}}`/`{{.ToolsEnabled}}`/`{{.Payload}}` template variables (unchanged) | No new template variables introduced |

## Related Files
- `personas/idiomatic.md` - rename + rewrite to `personas/ingrid.md`: generalize the Role/Focus sections from Go-specific idioms (line 1: "Go idioms and conventions reviewer"; line 3: "Go idiom skeptic... un-idiomatic Go"; line 7: "sentinel errors compared by string"; line 11: "Stdlib misuse: reimplementing strings/strconv/sort helpers") to a language-agnostic idiomatic-style lens ("idiomatic style for the language under review") while preserving the same category of findings (swallowed errors, leaked resources/goroutines, misused abstractions, reinvented standard-library behavior)
- `personas/testdata/idiomatic_fixture.patch` - rename to `personas/testdata/ingrid_fixture.patch`: verify the fixture's target category (an "error" category per `personas_test.go` line 131) still triggers under the generalized prompt
- `personas/personas_test.go` - modify: update the fixture test call at line 131 from `fixtureTest(t, "idiomatic", "testdata/idiomatic_fixture.patch", "error")` to reference `ingrid`/`ingrid_fixture.patch`

## Happy Path Scenarios
**Scenario 1: Generalized prompt renders without unresolved template markers**
- **Given** the rewritten `personas/ingrid.md` template with Go-specific phrasing replaced by language-agnostic phrasing
- **When** `payload.RenderPrompt` renders the template against a fixture context (as `TemplateFixtureRunner.RunFixture` does)
- **Then** the rendered output contains no unresolved `{{` markers, matching the existing pass condition for all other built-in personas

**Scenario 2: `atcr personas test ingrid` passes against the renamed fixture**
- **Given** `personas/ingrid.md` and `personas/testdata/ingrid_fixture.patch` both exist post-rename
- **When** `atcr personas test ingrid` is run
- **Then** the command reports the fixture passing (`HasFixture: true, Passed: 1, Total: 1`), confirming the generalized lens still catches the fixture's target category

**Scenario 3: Role/Focus sections read as language-agnostic**
- **Given** the rewritten prompt text
- **When** the Role and Focus sections are inspected
- **Then** they describe reviewing "idiomatic style for the language under review" (or equivalent general phrasing) rather than naming Go specifically, while still listing concrete, language-adaptable examples (e.g., "swallowed/discarded errors" instead of only "sentinel errors compared by string", "reinvented standard-library behavior" instead of only "reimplementing strings/strconv/sort helpers")

## Edge Cases
**Edge Case 1: Generalization must not dilute findings into generic advice**
- **Given** the story's Risk table warns that over-generalizing could produce advice too generic to catch specific idiom violations
- **When** the rewritten prompt's Focus and Severity Rubric sections are reviewed
- **Then** they retain concrete, checkable categories (error handling, resource/goroutine leaks, interface/abstraction misuse, stdlib reinvention) with severity examples, not just a vague "write idiomatic code" instruction

**Edge Case 2: Existing Go-specific fixture still exercises the generalized template**
- **Given** `personas/testdata/idiomatic_fixture.patch` (renamed to `ingrid_fixture.patch`) contains a Go code diff (the fixture content is not rewritten by this story per the plan's scope — only the prompt text changes)
- **When** the fixture test runs against the generalized `ingrid.md`
- **Then** the test still passes, since the generalized prompt's error-handling/resource-leak categories still apply to the Go example in the fixture; the persona is language-agnostic in phrasing, not incapable of reviewing Go

**Edge Case 3: `{{.AgentName}}` self-reference updated consistently**
- **Given** `idiomatic.md` line 1 and line 3 both reference `{{.AgentName}}` (a template variable, not the literal string "idiomatic")
- **When** the file is renamed/rewritten
- **Then** the `{{.AgentName}}` template variable usage is preserved unchanged — only the surrounding descriptive text ("Go idioms and conventions reviewer", "Go idiom skeptic") is generalized, since `{{.AgentName}}` is resolved at render time from the registered persona name, not hardcoded

## Error Conditions
**Error Scenario 1: Rewritten prompt breaks template rendering**
- **Given** a hypothetical malformed edit to `personas/ingrid.md` (e.g., an unbalanced `{{if}}`/`{{end}}` block introduced during the rewrite)
- **When** `payload.RenderPrompt` attempts to render it
- **Then** rendering returns an error (existing `RenderPrompt` error path, unchanged by this story), and `TestPersona` surfaces `fmt.Errorf("render persona %q: %w", name, err)` — caught by CI before merge via the existing fixture test
- HTTP status / error code: N/A (Go error return, not an HTTP path)

## Performance Requirements
- **Response Time:** No performance impact — prompt content length is comparable pre/post rewrite; template rendering remains a single in-memory string substitution pass.
- **Throughput:** N/A (not a request path).

## Security Considerations
- **Authentication/Authorization:** N/A — prompt content change only, no new trust boundary.
- **Input Validation:** N/A — static prompt template, no user input processed by this AC.

## Test Implementation Guidance
**Test Type:** UNIT (fixture rendering test) + manual/CLI verification (`atcr personas test ingrid`)
**Test Data Requirements:** Renamed `personas/testdata/ingrid_fixture.patch` (content carried over from `idiomatic_fixture.patch` unchanged, since the fixture code sample is not in scope for rewrite — only the prompt is)
**Mock/Stub Requirements:** None — reuses the existing `TemplateFixtureRunner` fixture-verification pattern with no LLM call

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `personas/ingrid.md`'s Role/Focus sections describe a language-agnostic idiomatic-style lens, with no remaining literal mentions of "Go" as the review target's language
- [ ] `atcr personas test ingrid` passes against `personas/testdata/ingrid_fixture.patch`
- [ ] The generalized prompt retains concrete, checkable finding categories (not diluted into generic style advice)
- [ ] `{{.AgentName}}` and other template variables are unchanged from the pre-rewrite template

**Manual Review:**
- [ ] Code reviewed and approved
