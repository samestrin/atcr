# Acceptance Criteria: Alias Passthrough Resolves the 7 Alias-Covered Personas Without a Catalog Scan

**Related User Story:** [03: Hybrid Resolver (Alias / Created-Timestamp / Explicit-Pin)](../user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go resolver function (`ResolveModel` or equivalent) in `internal/personas/catalog.go` | Strategy-selection switch: explicit pin first, then alias table, then created-timestamp scan (per story's Implementation Notes) |
| Test Framework | `testing` + `httptest.NewServer`, table-driven subtests | Mirrors `internal/personas/client_test.go` conventions already used in this package |
| Key Dependencies | `internal/personas.HTTPClient` (`client.go:35`), the checked-in catalog snapshot fixture | No new external dependency; alias resolution here is a static lookup table, not a catalog scan |

### Related Files (from codebase-discovery.json)
- `internal/personas/catalog.go` — create: hybrid resolver entry point plus a static alias table mapping the 7 alias-covered personas (anthony, sonny, gene, milo, gia, flint, celeste) to their provider `~`-prefixed `-latest` alias slug.
- `internal/personas/catalog_test.go` — create: unit tests asserting each of the 7 personas resolves to its alias slug unchanged and that no catalog fetch/scan occurs for this path.
- `internal/personas/testdata/catalog_snapshot.json` — create: fixture containing the `-latest` alias entries per `documentation/catalog-snapshot-fixture.md` (Anthropic, OpenAI, Google, Moonshot vendors).
- `internal/personas/client.go:35` (`HTTPClient`) — reference only: reuse the injectable `HTTPClient` seam so the resolver's catalog client (used only by the other two strategies) is testable, even though this AC's alias path never calls it.
- `personas/community/index.json:1` — reference only: source of truth confirming the 7 alias-covered persona names and their current pinned slugs.
- `documentation/openrouter-catalog-api.md` — reference: lists the documented `~`-prefixed `-latest` alias forms this AC's alias table must match verbatim.

## Happy Path Scenarios
**Scenario 1: Anthropic persona (anthony) resolves via alias passthrough**
- **Given** anthony's binding is family `anthropic` / channel `@stable` (or `@latest` — channel is irrelevant to the alias path)
- **When** the resolver is invoked with anthony's binding and the parsed catalog model list
- **Then** it returns `~anthropic/claude-opus-latest` unchanged, with no `created`-timestamp comparison performed

**Scenario 2: All 7 alias-covered personas resolve to their documented alias slugs in one pass**
- **Given** the 7 personas (anthony, sonny → Anthropic; gene, milo → OpenAI; gia, flint → Google; celeste → Moonshot) each bound to their vendor family
- **When** each is resolved against the catalog snapshot fixture
- **Then** each returns its documented `-latest` alias slug from `documentation/openrouter-catalog-api.md`'s Code Examples list (e.g. `~openai/gpt-latest`, `~openai/gpt-mini-latest`, `~google/gemini-pro-latest`, `~google/gemini-flash-latest`, `~moonshotai/kimi-latest`) verbatim

**Scenario 3: Alias path performs no catalog scan**
- **Given** a catalog model list that is empty, `nil`, or otherwise unusable
- **When** an alias-covered persona is resolved
- **Then** resolution still succeeds and returns the alias slug — the alias table lookup does not depend on catalog contents, proving "the provider owns resolution" per the story's Constraints

## Edge Cases
**Edge Case 1: Two personas share the same vendor family but different alias slugs**
- **Given** anthony (`~anthropic/claude-opus-latest`) and sonny (`~anthropic/claude-sonnet-latest`) both bind to the `anthropic` family
- **When** each is resolved
- **Then** each returns its own distinct alias slug (opus vs. sonnet), proving the alias table is keyed by persona/model-tier, not merely by vendor family

**Edge Case 2: Alias table entry is looked up case-sensitively / exactly**
- **Given** a persona's family string matches an alias-table key only after exact-string comparison
- **When** resolution runs
- **Then** no fuzzy or case-insensitive matching is applied — the table lookup is a strict map lookup, avoiding an accidental match against an unrelated vendor prefix

## Error Conditions
**Error Scenario 1: Persona family has no alias-table entry and is not otherwise a pin or a `created`-timestamp vendor**
- Error message: `"no alias, pin, or vendor-prefix strategy found for persona family %q"`
- HTTP status / error code: N/A (library error, not HTTP) — returned as a Go `error` from the resolver function, never a panic

## Performance Requirements
- **Response Time:** Alias resolution is an O(1) map lookup; must complete in microseconds with no I/O
- **Throughput:** All 7 alias-covered personas resolve in a single pass over the catalog snapshot fixture in under 50ms combined (dominated by fixture parse, not alias lookup)

## Security Considerations
- **Authentication/Authorization:** N/A — no network call is made on this path; the alias table is a static, compiled-in mapping, not attacker-influenced input
- **Input Validation:** Persona family/binding strings originate from `PersonaIndexEntry` (`internal/personas/search.go:14`) or the on-disk persona unit — validated upstream per Story 2; this resolver treats an unrecognized family as a resolution error, never a silent fallback to an arbitrary slug

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** The 7 alias-covered persona bindings (family + channel) mirroring `personas/community/index.json`'s current entries (anthony, sonny, gene, milo, gia, flint, celeste); the catalog snapshot fixture is loaded but this path does not require it to succeed
**Mock/Stub Requirements:** None beyond the standard `httptest.NewServer` fixture server already used elsewhere in the package — the alias path itself needs no HTTP mock since it never calls the catalog client

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] All 7 alias-covered personas resolve to their documented `-latest` alias slug unchanged
- [ ] No catalog fetch or scan occurs on the alias path (verified via a test double that would fail the test if invoked)
- [ ] An unrecognized persona family returns a descriptive error rather than a zero-value slug

**Manual Review:**
- [ ] Code reviewed and approved
