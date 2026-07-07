# Acceptance Criteria: Passing Fixtures for the 3 New Personas

**Related User Story:** [01: Author Model-Tuned Persona Content](../user-stories/01-author-model-tuned-persona-content.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Synthetic unified-diff `.patch` fixture files | External repo `atcr/personas`, `personas/testdata/` convention |
| Test Framework | Persona fixture test harness (per `docs/personas-authoring.md` section 3, "The fixture") | Manual/external execution — no in-repo CI surface |
| Key Dependencies | `docs/personas-authoring.md` fixture requirements table; existing fixtures (`personas/testdata/sentinel_fixture.patch`, `personas/testdata/tracer_fixture.patch`) as structural precedent |

## Related Files
- `atcr/personas/testdata/claude-reviewer_fixture.patch` - create: synthetic diff planting a known instance of the Claude persona's target category
- `atcr/personas/testdata/gpt-reviewer_fixture.patch` - create: synthetic diff planting a known instance of the GPT persona's target category
- `atcr/personas/testdata/gemini-reviewer_fixture.patch` - create: synthetic diff planting a known instance of the Gemini persona's target category
- `docs/personas-authoring.md` - reference only: fixture location/naming/content rules this AC verifies against (no change)

## Happy Path Scenarios
**Scenario 1: Each fixture is correctly located and named**
- **Given** the 3 new personas (`claude-reviewer`, `gpt-reviewer`, `gemini-reviewer`) have been authored
- **When** their fixtures are committed to `atcr/personas/testdata/` as `claude-reviewer_fixture.patch`, `gpt-reviewer_fixture.patch`, and `gemini-reviewer_fixture.patch`
- **Then** each file follows the `<slug>_fixture.patch` naming convention exactly, matching its persona's slug, with file mode `0644`

**Scenario 2: Fixture test passes for all 3 personas with no LLM and no network**
- **Given** the fixture test harness described in `docs/personas-authoring.md` section 3 ("What the test does")
- **When** it loads each committed `.patch` fixture, asserts the expected category word is present in that persona's prompt template, and renders the template with the fixture as the diff payload
- **Then** all 3 personas pass: no missing fixture, the category word is found in each template, and no unrendered `{{ }}` actions remain after rendering

## Edge Cases
**Edge Case 1: Fixture contains a real (non-synthetic) secret or credential**
- **Given** a fixture is drafted for review
- **When** it contains what could be a real API key, token, or credential rather than a placeholder like `FAKE_API_KEY_00000000`
- **Then** the fixture fails the contribution checklist and must be rewritten with synthetic-only content before merge (per `docs/personas-authoring.md`'s "never a real credential" requirement)

**Edge Case 2: Category word present only in the fixture diff, not the prompt template**
- **Given** a fixture plants a synthetic instance of the target category (e.g. an N+1 query, a prompt-injection pattern, an unsanitized input path)
- **When** the corresponding persona's `.md` template does not itself contain that category word in `## Focus` or `## Output Format`
- **Then** the fixture test fails — the word leaking in only from the injected diff does not satisfy the "name the category in the prompt" requirement

## Error Conditions
**Error Scenario 1: Missing or uncommitted fixture**
- Error message: fixture test load failure — "a missing or uncommitted fixture fails here" (per `docs/personas-authoring.md` section 3, step 1)
- HTTP status / error code: N/A (test-harness failure, not an HTTP path)

**Error Scenario 2: Unrendered template variable remains after rendering**
- Error message: fixture test rendering failure — "confirms no unrendered `{{ }}` actions remain" (per `docs/personas-authoring.md` section 3, step 3)
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — fixture test runs locally with no network call, per the "no network" requirement
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — fixture test requires no credentials
- **Input Validation:** Fixtures must contain synthetic-only content (no real secrets/credentials); the fixture test enforces no live network access in the test path

## Test Implementation Guidance
**Test Type:** MANUAL (external-observation-based; this codebase's CI does not execute the `atcr/personas` repo's own fixture test suite)
**Test Data Requirements:** 3 synthetic unified-diff fixtures, one per persona, each containing a known instance of that persona's target category (e.g. Claude persona: a correctness/security-flavored synthetic bug per its Focus categories; GPT persona: its own target category; Gemini persona: its own target category)
**Mock/Stub Requirements:** N/A — the fixture test is itself the mock: it substitutes for a real model call by rendering the template against static fixture content and asserting no leftover template actions

## Definition of Done
**Auto-Verified:**
- [ ] N/A in this codebase — no automated test target exists here for external-repo content (documented per story's Constraints)
- [ ] No linting errors (N/A — external repo)
- [ ] Build succeeds (N/A — external repo)

**Story-Specific:**
- [ ] All 3 fixtures exist at `atcr/personas/testdata/<slug>_fixture.patch`, mode `0644`, synthetic-content-only
- [ ] All 3 fixtures' target category words appear in their respective persona's prompt template (not just the diff)
- [ ] Fixture test harness passes for all 3 personas with no network access, per `docs/personas-authoring.md`'s checklist

**Manual Review:**
- [ ] Code reviewed and approved (external repo maintainer confirms fixture content is synthetic and the checklist is satisfied before merge)
