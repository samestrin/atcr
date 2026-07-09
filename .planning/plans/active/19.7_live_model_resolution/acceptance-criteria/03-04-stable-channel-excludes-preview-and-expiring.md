# Acceptance Criteria: `@stable` Channel Excludes Preview/Beta/Exp-Tagged and Expiring Models in the `created`-Timestamp Scan

**Related User Story:** [03: Hybrid Resolver (Alias / Created-Timestamp / Explicit-Pin)](../user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go filter function reused from Story 1's `@stable` heuristic, applied inside the `created`-timestamp scan | Per the story's third Potential Risk: "Reuse Story 1's single `@stable` filter function verbatim ... rather than reimplementing the exclusion logic locally" |
| Test Framework | `testing` + `httptest.NewServer`, table-driven subtests | Fixture must include a preview-tokened newest entry so exclusion is provably load-bearing (not a no-op) |
| Key Dependencies | Catalog `expiration_date` (`string \| null`) and slug/`id` string tokens (`preview`, `beta`, `exp`) per `documentation/openrouter-catalog-api.md`'s schema and heuristic notes | No new dependency — string-token matching plus null-check on `expiration_date` |

## Related Files
- `internal/personas/catalog.go` - create: the `@stable` exclusion filter (shared with/reused from Story 1) invoked inside the `created`-timestamp scan for delia/quinn/glenna when channel is `@stable`
- `internal/personas/catalog_test.go` - create: unit tests proving a newest-`created` entry tagged `preview`/`beta`/`exp` is skipped, and a newest entry with a non-null `expiration_date` is skipped, under `@stable`
- `internal/personas/testdata/catalog_snapshot.json` - create: fixture containing at least one preview/beta/exp-tokened entry and at least one entry with a non-null `expiration_date`, both positioned as the numerically newest `created` candidate in their vendor prefix, per `documentation/catalog-snapshot-fixture.md`
- `documentation/openrouter-catalog-api.md` - reference only: source of the "no explicit stable/GA/preview flag" gap and the heuristic definition (exclude preview/beta/exp tokens + honor `expiration_date`)

## Happy Path Scenarios
**Scenario 1: `@stable` skips a preview-tagged newest entry and picks the next-newest eligible one**
- **Given** the `deepseek/` prefix has three entries: the newest by `created` is `deepseek/deepseek-v5-preview`, the second-newest is `deepseek/deepseek-v4-pro` (no preview/beta/exp token, no `expiration_date`)
- **When** delia is resolved with channel `@stable`
- **Then** the resolver returns `deepseek/deepseek-v4-pro` — the newest entry is excluded by the `@stable` heuristic and the next-newest eligible entry is selected instead

**Scenario 2: `@stable` skips a newest entry with a non-null `expiration_date`**
- **Given** the `qwen/` prefix has a newest-by-`created` entry carrying a non-null `expiration_date`, and an older eligible entry with `expiration_date: null`
- **When** quinn is resolved with channel `@stable`
- **Then** the resolver returns the older, non-expiring entry

**Scenario 3: `@stable` accepts an entry with no exclusion signals**
- **Given** the `z-ai/` prefix's newest-by-`created` entry has no preview/beta/exp token in its slug and `expiration_date: null`
- **When** glenna is resolved with channel `@stable`
- **Then** that entry is returned directly — `@stable` is a pure exclusion filter, not an additional inclusion requirement beyond "not flagged as preview/expiring"

## Edge Cases
**Edge Case 1: Token match is on slug substring, not exact segment**
- **Given** an entry slug contains `preview` as a substring anywhere (e.g. `deepseek/deepseek-v5-preview-01`)
- **When** the `@stable` filter evaluates it
- **Then** it is excluded — the heuristic matches the documented tokens (`preview`, `beta`, `exp`) as substrings per `documentation/openrouter-catalog-api.md`'s heuristic description, not requiring an exact path-segment boundary (documented explicitly in code/tests to avoid ambiguity about match strictness)

**Edge Case 2: All eligible entries under a vendor prefix are excluded by `@stable`**
- **Given** every `deepseek/` entry in the snapshot is either preview-tagged or expiring
- **When** delia is resolved with channel `@stable`
- **Then** resolution fails with a descriptive error (see Error Conditions) rather than falling back to an excluded entry

**Edge Case 3: `expiration_date` is an empty string rather than JSON `null`**
- **Given** a catalog entry has `expiration_date: ""` (empty string, not `null`)
- **When** the `@stable` filter evaluates it
- **Then** the filter treats an empty/whitespace string equivalently to `null` (not deprecated) — this is documented explicitly since the schema is `string | null` and a client could plausibly return `""` in either case

## Error Conditions
**Error Scenario 1: No `@stable`-eligible entry exists under the vendor prefix**
- Error message: `"no stable (non-preview, non-expiring) %s-prefixed model found in catalog for persona %q"`
- HTTP status / error code: N/A (library error) — the resolver fails closed; it never silently falls back to a `@latest`-only entry when the persona's channel is `@stable`

## Performance Requirements
- **Response Time:** The `@stable` filter is a per-entry string-contains check plus a null/empty check on `expiration_date`, applied during the same linear scan as the `created`-timestamp comparison — no additional pass over the catalog
- **Throughput:** No measurable overhead beyond the base `created`-timestamp scan's performance bound (AC 03-02)

## Security Considerations
- **Authentication/Authorization:** N/A — pure in-memory filtering over already-fetched catalog data
- **Input Validation:** Token matching operates on catalog-supplied strings (`id`/`canonical_slug`); no user-supplied input reaches this filter, so no injection surface is introduced

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Catalog snapshot fixture with at least one preview/beta/exp-tokened entry and one non-null-`expiration_date` entry positioned as the newest `created` candidate for at least one of the three `created`-timestamp vendors, per `documentation/catalog-snapshot-fixture.md`'s explicit requirement
**Mock/Stub Requirements:** `httptest.NewServer` serving the fixture; no additional mocking needed since the filter is pure function logic over parsed JSON

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A preview/beta/exp-tokened newest entry is excluded under `@stable` and the next-eligible entry is selected
- [ ] An entry with a non-null `expiration_date` is excluded under `@stable`
- [ ] The `@stable` filter function is reused (not reimplemented) from Story 1's heuristic
- [ ] All eligible entries excluded under a vendor prefix produces a descriptive error, not a silent fallback

**Manual Review:**
- [ ] Code reviewed and approved
