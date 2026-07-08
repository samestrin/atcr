# Acceptance Criteria: Fixture Authoring and Fixture-Test Pass

**Related User Story:** [04: Model-Indexed Persona Library Authoring](../user-stories/04-model-indexed-persona-library-authoring.md)
**Design References:** [persona-yaml-schema.md](../documentation/persona-yaml-schema.md), [testing-mock-registry.md](../documentation/testing-mock-registry.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | `.patch` fixture files + Go test | `personas/community/testdata/<slug>_fixture.patch` (community-layout location, LOCKED below) |
| Test Framework | Go `testing` package, table-driven, mirroring `personas_test.go`'s fixture-load pattern | No network access permitted in this test path |
| Key Dependencies | stdlib `os`/`strings`; `internal/payload.PayloadContext` | No new third-party dependency |

### Fixture location (LOCKED — single choice)
Community fixtures live at **`personas/community/testdata/<slug>_fixture.patch`** (co-located with the community persona layout), NOT under the built-in `personas/testdata/`. This requires:
- A new embed directive in the `personas` package (`personas/personas.go` or a new `personas/community.go`): `//go:embed community/testdata/*.patch` bound to a new `embed.FS` var, and `//go:embed community/*.md` for the co-located prompt templates, alongside the existing built-in `//go:embed testdata/*.patch` / `//go:embed *.md`.
- Extending `TemplateFixtureRunner.RunFixture` (`internal/personas/test.go:34`), which today short-circuits to `HasFixture: false` for any non-built-in name, so that a community persona resolves its fixture from `community/testdata/<name>_fixture.patch` and its template from `community/<name>.md` (via new exported accessors on the `personas` package, e.g. `builtins.CommunityFixture(name)` / `builtins.CommunityGet(name)`), then renders and asserts no leftover `{{ }}`.
- Updating `docs/personas-authoring.md` so its documented fixture location/naming matches `personas/community/testdata/<slug>_fixture.patch` exactly.

### Related Files (from codebase-discovery.json)
- `personas/community/testdata/<slug>_fixture.patch` — create: one synthetic unified-diff fixture per authored community persona (10 total), embedded via the new `//go:embed community/testdata/*.patch` directive.
- `personas/community/<slug>.md` — reference: the co-located prompt template (Q2) that the fixture renders as `{{.Payload}}` — the fixture renders THIS `.md`, not a binding to a built-in template.
- `personas/community_test.go` — create: extends the fixture-test loop to iterate every community persona, asserting fixture presence, category-word presence in the template, slug-consistency, and full render with no leftover `{{ }}` actions.
- `internal/personas/test.go` (`TemplateFixtureRunner`) — modify: extend runner resolution (per the LOCKED location above) so community personas resolve `community/testdata/<name>_fixture.patch` + `community/<name>.md` and produce a passing fixture instead of `HasFixture: false`.
- `docs/personas-authoring.md` — modify: fixture location/naming/content rules updated to the locked `personas/community/testdata/` path.


## Happy Path Scenarios
**Scenario 1: Every community persona has a committed fixture at the correct location and name**
- **Given** the full set of authored community personas
- **When** the fixture-test loop runs
- **Then** each persona resolves to a `<slug>_fixture.patch` file present under `personas/community/testdata/` (the LOCKED location), and a missing fixture fails that persona's subtest

**Scenario 2: Each persona's target category word appears in its own prompt template**
- **Given** a persona's fixture, authored to plant a known instance of category X (e.g. `injection`, `n+1`, `race`)
- **When** the fixture test checks the persona's Markdown template text (case-insensitive) for the word X
- **Then** the word is found in the template's `## Focus` or `## Output Format` example, independent of whatever text is in the fixture's diff payload

**Scenario 3: Fixture renders as the diff payload with zero leftover template actions**
- **Given** the co-located `personas/community/<slug>.md` template (Q2 — the persona's own prompt, not a binding to a built-in template) and its fixture loaded as `{{.Payload}}`
- **When** the template is rendered with a fully-populated `payload.PayloadContext`
- **Then** the rendered output contains no unrendered `{{ }}` actions, and the test passes with zero network calls made

**Scenario 4: Slug is consistent across YAML, `.md`, resolvable name, and index `path`**
- **Given** each community persona
- **When** the test cross-checks its identifiers
- **Then** all of these are the same slug string: the YAML `name`/slug == the `.md` basename (`<slug>.md`) == the name `ResolvePersona` looks up (`<persona>.md`, `internal/registry/persona.go:64`) == the stem of the `index.json` `path`; and the slug passes `validateName` (`internal/registry/persona.go:111`: non-empty, no path separators, no `..`, no leading dot, not `_base`), so a mismatched or unresolvable entry fails the subtest

## Edge Cases
**Edge Case 1: Fixture contains only synthetic values, never a real credential**
- **Given** a fixture that plants a credential-shaped finding (e.g. for a security-lens persona)
- **When** the fixture content is reviewed
- **Then** any secret-like value uses an explicit placeholder pattern (e.g. `FAKE_API_KEY_00000000`), never a value that could be mistaken for a live credential

**Edge Case 2: Category word present in the fixture diff but absent from the template**
- **Given** a persona whose fixture diff happens to contain the category word in a comment, but the template itself never names it
- **When** the fixture test's category-word check inspects only the template text (not the diff)
- **Then** the test fails, rejecting a persona that would otherwise silently pass via word-leakage from the injected diff

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
- **Response Time:** Fixture load + template render for 10 personas completes in well under 1 second; no measurable regression versus baseline (≤1% wall-time difference in `go test ./...`) to `go test ./...` runtime.
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
- [ ] Every community persona has a `<slug>_fixture.patch` committed at `personas/community/testdata/` (the locked location), embedded and resolved by the extended `TemplateFixtureRunner`
- [ ] Each fixture renders the co-located `personas/community/<slug>.md` (Q2), not a binding to a built-in template
- [ ] Slug is consistent across YAML `name`, `.md` basename, resolvable persona name, and index `path` stem, and passes `validateName`
- [ ] Every persona's category word appears in its own template text, independent of the fixture diff
- [ ] Every persona's fixture renders with zero leftover `{{ }}` actions
- [ ] The fixture-test loop makes zero network calls

**Manual Review:**
- [ ] Code reviewed and approved
