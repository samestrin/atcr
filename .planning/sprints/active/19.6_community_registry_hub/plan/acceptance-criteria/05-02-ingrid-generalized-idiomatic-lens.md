# Acceptance Criteria: `ingrid` — Generalizing `idiomatic`'s Go-Specific Lens

**Related User Story:** [05: Human-Names Migration for Built-in Stragglers](../user-stories/05-human-names-migration-for-built-in-stragglers.md)
**Design References:** [human-names-migration.md](../documentation/human-names-migration.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown prompt template (`personas/idiomatic.md` → `personas/ingrid.md`) | Content rewrite, not a mechanism change |
| Test Framework | Go `testing` package fixture test (`TestPersona`/`TemplateFixtureRunner`) | Verifies the rewritten template still renders and still catches the fixture's target category |
| Key Dependencies | Existing `payload.RenderPrompt` template engine; `{{.AgentName}}`/`{{.ScopeRule}}`/`{{.ToolsEnabled}}`/`{{.Payload}}` template variables (unchanged) | No new template variables introduced |

### Related Files (from codebase-discovery.json)
- `personas/idiomatic.md` → `personas/ingrid.md` — rename + rewrite: generalize Role/Focus sections from Go-specific idioms to a language-agnostic idiomatic-style lens while preserving concrete finding categories.
- `personas/testdata/idiomatic_fixture.patch` → `personas/testdata/ingrid_fixture.patch` — rename (carries the original Go diff).
- `personas/testdata/ingrid_lang2_fixture.patch` (or equivalent second-fixture path) — **create**: a NEW non-Go diff (e.g. Python or TypeScript) exercising a generalized idiomatic category (swallowed/discarded errors, resource leak, or stdlib reinvention) so that "generalized beyond Go" is positively verified by a passing non-Go fixture, not asserted by prose alone. The exact second-fixture wiring (a second `_fixture.patch` the runner iterates, or an added table-driven case) is a sizing detail for `/create-sprint`; the requirement is that at least one non-Go sample passes against `ingrid.md`.
- `personas/personas.go` (names slice ~line 20, embedded file guard) — modify: replace `"idiomatic"` with `"ingrid"` in the `names` slice.
- `personas/personas_test.go` — modify: update fixture test call from `idiomatic`/`idiomatic_fixture.patch` to `ingrid`/`ingrid_fixture.patch`.
- `docs/personas-authoring.md` / `docs/personas-install.md` — modify: update worked examples (see AC 05-04).


## Happy Path Scenarios
**Scenario 1: Generalized prompt renders without unresolved template markers**
- **Given** the rewritten `personas/ingrid.md` template with Go-specific phrasing replaced by language-agnostic phrasing
- **When** `payload.RenderPrompt` renders the template against a fixture context (as `TemplateFixtureRunner.RunFixture` does)
- **Then** the rendered output contains no unresolved `{{` markers, matching the existing pass condition for all other built-in personas

**Scenario 2: `atcr personas test ingrid` passes against the renamed fixture**
- **Given** `personas/ingrid.md` and `personas/testdata/ingrid_fixture.patch` both exist post-rename
- **When** `atcr personas test ingrid` is run
- **Then** the command reports the fixture passing (`HasFixture: true, Passed: 1, Total: 1`), confirming the generalized lens still catches the fixture's target category

**Scenario 4: A non-Go fixture positively verifies "generalized beyond Go"**
- **Given** a second, non-Go fixture (e.g. a Python or TypeScript diff added at `personas/testdata/ingrid_lang2_fixture.patch`) that plants a generalized idiomatic violation (e.g. a swallowed/discarded error, an unreleased resource, or a reinvented standard-library helper)
- **When** the fixture runner exercises `ingrid.md` against that non-Go sample
- **Then** the generalized lens catches the planted category and the fixture passes — so AC4's "generalized beyond Go" is confirmed by an executed non-Go check, not only by prose in the Role/Focus sections. This is the positive counterpart to Edge Case 2 (which confirms the original Go sample still passes)

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
- [ ] A second, non-Go fixture (Python/TypeScript) exercising a generalized idiomatic category passes against `ingrid.md`, positively verifying "generalized beyond Go" (AC4) — not asserted by prose alone
- [ ] The generalized prompt retains concrete, checkable finding categories (not diluted into generic style advice)
- [ ] `{{.AgentName}}` and other template variables are unchanged from the pre-rewrite template

**Manual Review:**
- [ ] Code reviewed and approved
