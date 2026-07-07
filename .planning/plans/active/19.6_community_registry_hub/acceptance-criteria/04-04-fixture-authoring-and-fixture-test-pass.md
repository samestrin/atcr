# Acceptance Criteria: Fixture Authoring and Fixture-Test Pass

**Related User Story:** [04: Model-Indexed Persona Library Authoring](../user-stories/04-model-indexed-persona-library-authoring.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | `.patch` fixture files + Go test | `personas/testdata/<slug>_fixture.patch` (or the community-layout equivalent) |
| Test Framework | Go `testing` package, table-driven, mirroring `personas_test.go`'s fixture-load pattern | No network access permitted in this test path |
| Key Dependencies | stdlib `os`/`strings`; `internal/payload.PayloadContext` | No new third-party dependency |

## Related Files
- `personas/testdata/<slug>_fixture.patch` - create: one synthetic unified-diff fixture per authored community persona (10 total), each containing a known instance of that persona's target category
- `personas/community_test.go` - modify/create: extends the fixture-test loop (per `docs/personas-authoring.md` section 3) to iterate every community persona, asserting fixture presence, category-word presence in the template, and full render with no leftover `{{ }}` actions
- `docs/personas-authoring.md` - reference: fixture location/naming/content rules (`<slug>_fixture.patch`, mode `0644`, synthetic values only)

## Happy Path Scenarios
**Scenario 1: Every community persona has a committed fixture at the correct location and name**
- **Given** the full set of authored community personas
- **When** the fixture-test loop runs
- **Then** each persona resolves to a `<slug>_fixture.patch` file present under `personas/testdata/` (or the documented community-layout equivalent), and a missing fixture fails that persona's subtest

**Scenario 2: Each persona's target category word appears in its own prompt template**
- **Given** a persona's fixture, authored to plant a known instance of category X (e.g. `injection`, `n+1`, `race`)
- **When** the fixture test checks the persona's Markdown template text (case-insensitive) for the word X
- **Then** the word is found in the template's `## Focus` or `## Output Format` example, independent of whatever text is in the fixture's diff payload

**Scenario 3: Fixture renders as the diff payload with zero leftover template actions**
- **Given** a persona template and its fixture loaded as `{{.Payload}}`
- **When** the template is rendered with a fully-populated `payload.PayloadContext`
- **Then** the rendered output contains no unrendered `{{ }}` actions, and the test passes with zero network calls made

## Edge Cases
**Edge Case 1: Fixture contains only synthetic values, never a real credential**
- **Given** a fixture that plants a credential-shaped finding (e.g. for a security-lens persona)
- **When** the fixture content is reviewed
- **Then** any secret-like value uses an explicit placeholder pattern (e.g. `FAKE_API_KEY_00000000`), never a value that could be mistaken for a live credential

**Edge Case 2: Category word present in the fixture diff but absent from the template**
- **Given** a persona whose fixture diff happens to contain the category word in a comment, but the template itself never names it
- **When** the fixture test's category-word check inspects only the template text (not the diff)
- **Then** the test fails, correctly rejecting a persona that would otherwise silently pass via word-leakage from the injected diff

## Error Conditions
**Error Scenario 1: Fixture file missing or uncommitted**
- **Given** a persona listed in `index.json` with no corresponding `<slug>_fixture.patch` on disk
- **When** the fixture-test loop runs
- **Then** that persona's subtest fails with a clear "fixture not found" error identifying the expected path

**Error Scenario 2: Fixture-test attempts network access**
- **Given** the fixture-test loop's implementation
- **When** `go test ./...` runs in a sandboxed/offline CI environment
- **Then** all community persona fixture subtests pass with zero outbound network calls, consistent with the existing built-in persona fixture-test pattern

## Performance Requirements
- **Response Time:** Fixture load + template render for 10 personas completes in well under 1 second; no measurable regression to `go test ./...` runtime.
- **Throughput:** N/A (test-time only).

## Security Considerations
- **Authentication/Authorization:** N/A — fixtures are static local files, no auth surface.
- **Input Validation:** Fixture content is read from disk only (no untrusted network input at test time); every fixture is asserted to contain only synthetic placeholder values per `docs/personas-authoring.md`'s security note.

## Test Implementation Guidance
**Test Type:** UNIT (mirrors the existing built-in persona fixture-test pattern in `personas_test.go`)
**Test Data Requirements:** 10 `<slug>_fixture.patch` files, one per authored community persona, each a valid unified diff containing a synthetic instance of that persona's target category
**Mock/Stub Requirements:** None — fixtures are read from disk; no HTTP client or LLM call is exercised in this test path

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Every community persona has a `<slug>_fixture.patch` committed at the documented location
- [ ] Every persona's category word appears in its own template text, independent of the fixture diff
- [ ] Every persona's fixture renders with zero leftover `{{ }}` actions
- [ ] The fixture-test loop makes zero network calls

**Manual Review:**
- [ ] Code reviewed and approved
