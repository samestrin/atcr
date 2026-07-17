# Acceptance Criteria: Community Roster Registration (`communityPersonas`)

**Related User Story:** [2: Fixture Authoring & Test-Gate Integration](../user-stories/02-fixture-authoring-test-gate-integration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go test-file data (slice literal) | `communityPersona` struct: `{Slug, VendorToken, Category string}` |
| Test Framework | `go test` + `testify/require` | `personas/community_test.go` |
| Key Dependencies | None beyond stdlib/testify | Roster is a hand-maintained `[]communityPersona` literal, no code generation |

## Related Files
- `personas/community_test.go` - modify: add a `{Slug: "simon", VendorToken: "<token>", Category: "<word>"}` row to the `communityPersonas` slice literal (line ~117-130)
- `personas/community/simon.md` - dependency (from Story 1): the prompt template that must literally contain the same `Category` word chosen in the roster row
- `personas/community/simon.yaml` - dependency (from Story 1): source of the bound model id from which `VendorToken` is derived as a case-insensitive substring

### Related Files (from codebase-discovery.json)
- `personas/community_test.go:117` - modify (`files_to_modify`, minor scope): append `{Slug: "simon", VendorToken: "<vendor-substring-of-model>", Category: "<distinct word>"}` to the `communityPersonas` roster slice so all roster-driven gate tests cover it
- `personas/community/simon.yaml` - dependency (`files_to_create`, Story 1): bound model id the `VendorToken` is derived from
- `personas/community/simon.md` - dependency (`files_to_create`, Story 1): prompt text that must contain the roster row's `Category` word verbatim (case-insensitive)
- `personas/community/gene.yaml` - reference only (`related_files`, low relevance): precedent that VendorToken reuse across personas is permitted (gene and milo both use `gpt`)

## Happy Path Scenarios
**Scenario 1: Roster row added with unclaimed Category and matching VendorToken**
- **Given** `simon.yaml`'s `model` field contains a vendor-identifying substring (e.g. `anthropic/claude-...` contains `claude`) and `simon.md` contains a chosen category word not among the 13 claimed values (coupling, logic, contract, validation, race, leak, complexity, type, dependency, observability, secret, duplication, invariant)
- **When** the row `{Slug: "simon", VendorToken: "<lowercase-substring-of-model>", Category: "<unclaimed-word>"}` is appended to `communityPersonas`
- **Then** `TestCommunityAccessors` (`personas/community_test.go:175`) passes `require.Len(t, names, len(communityPersonas))` with the roster now at 14 entries, matching `len(CommunityNames())`

**Scenario 2: Category word verified present in simon.md**
- **Given** the roster row's `Category` field is set
- **When** `TestCommunityPersonas_FixtureAndPromptCategory` (line ~202) reads `personas/community/simon.md` and lowercases it
- **Then** `require.Containsf(t, strings.ToLower(string(text)), p.Category, ...)` passes because the exact word appears verbatim in `simon.md`

**Scenario 3: VendorToken verified as substring of the bound model id**
- **Given** the roster row's `VendorToken` field is set to a lowercase token
- **When** `TestCommunityIndex_Registration` (line ~390) checks `require.Containsf(t, strings.ToLower(e.Model), p.VendorToken, ...)` against the `index.json` entry's model
- **Then** the assertion passes, and `simon` is also discoverable via `strings.Contains(strings.ToLower(cand.Model), p.VendorToken)` in the discoverable-by-vendor-token check (line ~401)

**Scenario 4: Category distinct across the full roster**
- **Given** all 14 roster rows (13 existing + `simon`) after registration
- **When** `TestCommunityPersonas_DistinctCategories` (line ~298) builds a `map[string]string` keyed by `Category`
- **Then** no duplicate key is found and `require.Lenf(t, seen, len(communityPersonas), ...)` passes, confirming `simon`'s category is unique

## Edge Cases
**Edge Case 1: Category word chosen matches a claimed value by coincidence**
- **Given** a candidate category word is picked without cross-checking the claimed list
- **When** it happens to equal one of coupling/logic/contract/validation/race/leak/complexity/type/dependency/observability/secret/duplication/invariant
- **Then** `TestCommunityPersonas_DistinctCategories` fails with `t.Fatalf("personas %q and %q share category %q — lenses must be distinct", ...)`; the fix is to cross-check the claimed-word list before writing the row (as flagged in the story's risk table)

**Edge Case 2: Category in roster row doesn't match the word embedded in simon.md**
- **Given** Story 1 embedded one category word in `simon.md`'s Focus section
- **When** the roster row's `Category` field is authored independently and drifts from that exact word (e.g. plural vs singular, or a synonym)
- **Then** `TestCommunityPersonas_FixtureAndPromptCategory`'s `require.Containsf` fails because the lowercase roster word is not a substring of `simon.md`'s text — the two must be verbatim-identical, not merely similar

**Edge Case 3: VendorToken chosen doesn't match simon.yaml's model casing**
- **Given** `simon.yaml`'s `model` field uses mixed case or a vendor name spelled differently than expected
- **When** the roster's `VendorToken` is authored without lowercasing or without verifying it's an actual substring
- **Then** `TestCommunityIndex_Registration`'s `require.Containsf(t, strings.ToLower(e.Model), p.VendorToken, ...)` fails because token comparison is case-sensitive against an already-lowercased model string, so `VendorToken` itself must be lowercase

## Error Conditions
**Error Scenario 1: Roster entry omitted entirely (partial registration)**
- Error message: `require.Len(t, names, len(communityPersonas))` failure in `TestCommunityAccessors` — fatal, blocks the entire `personas` test package from reporting other results meaningfully
- HTTP status / error code: N/A (Go test failure, `require.Len` triggers `t.FailNow()`)

**Error Scenario 2: Category collides with an existing roster entry**
- Error message: `"personas %q and %q share category %q — lenses must be distinct"` from `TestCommunityPersonas_DistinctCategories` (`t.Fatalf`)
- HTTP status / error code: N/A (Go test failure)

**Error Scenario 3: VendorToken not found in the bound model id**
- Error message: `"model %q must carry vendor token %q"` from `TestCommunityIndex_Registration` (`personas/community_test.go:390-391`)
- HTTP status / error code: N/A (Go test failure)

## Performance Requirements
- **Response Time:** No runtime performance impact — this is compile-time Go test data; `go test ./personas/...` must still complete within existing CI budgets (no new slow paths introduced)
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — in-repo test data, no runtime exposure
- **Input Validation:** `VendorToken` and `Category` are plain lowercase Go string literals; no external input, so no injection surface. `Category` must remain "a single lowercase category word" per the existing struct-field convention (`personas/community_test.go:110`)

## Test Implementation Guidance
**Test Type:** UNIT (`go test ./personas/...`)
**Test Data Requirements:** The roster row itself is the test data — no fixtures beyond `simon.yaml`/`simon.md` (Story 1) and `simon_fixture.patch` (AC 02-01) are needed for this AC's assertions to pass
**Mock/Stub Requirements:** None — pure string-comparison assertions against real committed files, no LLM or network calls

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `communityPersonas` in `personas/community_test.go` contains exactly one `simon` row with `Slug: "simon"`
- [ ] `Category` value is unclaimed by any of the other 13 roster entries and appears verbatim (case-insensitive) in `personas/community/simon.md`
- [ ] `VendorToken` value is a lowercase, case-insensitive substring of `simon.yaml`'s `model` field
- [ ] `TestCommunityAccessors`, `TestCommunityPersonas_FixtureAndPromptCategory`, and `TestCommunityPersonas_DistinctCategories` all pass with `simon` included

**Manual Review:**
- [ ] Code reviewed and approved
