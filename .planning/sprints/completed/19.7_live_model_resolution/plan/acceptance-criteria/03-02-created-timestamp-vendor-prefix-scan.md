# Acceptance Criteria: `created`-Timestamp Newest-in-Vendor-Prefix Scan Resolves delia/quinn/glenna (z-ai/ Correctness)

**Related User Story:** [03: Hybrid Resolver (Alias / Created-Timestamp / Explicit-Pin)](../user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go resolver function: filter catalog entries by vendor prefix, sort/select by `created` descending | Reuses Story 1's `@stable` exclusion heuristic verbatim rather than reimplementing it (per Risk Mitigation) |
| Test Framework | `testing` + `httptest.NewServer`, table-driven subtests including an explicit `z-ai/` regression case | |
| Key Dependencies | Catalog model entries with `id`/`canonical_slug`/`created`/`expiration_date` (per `documentation/openrouter-catalog-api.md`); a vendor-prefix table sourced from `personas/community/index.json` | No sorting library needed — a linear max-by-`created` scan suffices |

### Related Files (from codebase-discovery.json)
- `internal/personas/catalog.go` — create: the `created`-timestamp newest-in-vendor-prefix scan function, plus the vendor-prefix table `delia → "deepseek/"`, `quinn → "qwen/"`, `glenna → "z-ai/"` (explicitly NOT `"glm/"`).
- `internal/personas/catalog_test.go` — create: unit tests covering delia/quinn/glenna resolution, plus a dedicated regression test asserting glenna's scan filters on `z-ai/` and returns zero matches (or an error) if incorrectly filtered on `glm/`.
- `internal/personas/testdata/catalog_snapshot.json` — create: fixture with multiple `deepseek/`, `qwen/`, and `z-ai/` models at varying `created` timestamps, per `documentation/catalog-snapshot-fixture.md`'s required content list.
- `personas/community/index.json:1` — reference only: source of truth confirming glenna's catalog slug is `z-ai/glm-5.2` (around line 98), not any `glm/`-prefixed slug.
- `documentation/openrouter-catalog-api.md` — reference: catalog schema (`id`, `canonical_slug`, `created`, `expiration_date`) and the absence of a stability flag that makes the `created`-timestamp scan necessary for these three personas.

## Happy Path Scenarios
**Scenario 1: delia resolves to the newest DeepSeek model**
- **Given** the catalog snapshot contains three `deepseek/` models with distinct `created` timestamps and none carry preview/beta/exp tokens or a non-null `expiration_date`
- **When** delia (bound to family `deepseek`, channel `@stable`) is resolved
- **Then** the resolver returns the `deepseek/`-prefixed slug with the numerically largest `created` value

**Scenario 2: quinn resolves to the newest Qwen model**
- **Given** the catalog snapshot contains multiple `qwen/` models with distinct `created` timestamps
- **When** quinn (bound to family `qwen`, channel `@stable`) is resolved
- **Then** the resolver returns the `qwen/`-prefixed slug with the largest `created` value among eligible (non-excluded) entries

**Scenario 3: glenna resolves against the `z-ai/` prefix, not `glm/`**
- **Given** the catalog snapshot contains multiple `z-ai/` models (e.g. `z-ai/glm-5.1`, `z-ai/glm-5.2`) with distinct `created` timestamps, and contains no `glm/`-prefixed entries at all
- **When** glenna (bound to family `glm`, channel `@stable`) is resolved
- **Then** the resolver returns the newest `z-ai/`-prefixed slug (e.g. `z-ai/glm-5.2`) — proving the vendor-prefix table maps glenna's family to the catalog namespace `z-ai/`, not a `glm/` namespace that does not exist

## Edge Cases
**Edge Case 1: Vendor-prefix table entry mismatched against persona display name would silently break glenna**
- **Given** a hypothetical (incorrect) implementation that derives the vendor prefix from the persona's display name/family label (`glm`) by naive string reuse (e.g. `family + "/"`) instead of consulting the actual catalog namespace
- **When** glenna is resolved against a fixture containing only `z-ai/`-prefixed entries
- **Then** the test for this AC fails loudly (zero matches or an explicit error) rather than silently resolving to an empty slug — this is the regression test encoded from the Potential Risks table (`personas/community/index.json` was the source of the original catch during `/refine-epic`)

**Edge Case 2: Only one eligible model exists under a vendor prefix**
- **Given** the catalog snapshot has exactly one non-excluded `deepseek/` entry
- **When** delia is resolved
- **Then** that single entry's slug is returned (no ambiguity handling needed for a singleton match)

**Edge Case 3: Two candidate entries share an identical `created` timestamp**
- **Given** two `qwen/` entries tie on `created`
- **When** quinn is resolved
- **Then** the resolver deterministically selects the entry whose slug (`id`/`canonical_slug`) sorts last lexicographically (descending string comparison); catalog-array order is NOT used as a tiebreak because array order is not a guaranteed-stable property of the OpenRouter response. A test asserts identical selection across repeated invocations AND against a shuffled-order copy of the same fixture.

**Edge Case 4: A candidate entry has an absent, zero, or unparseable `created` timestamp**
- **Given** a `deepseek/`-prefixed entry whose `created` field is absent, zero, or otherwise unparseable
- **When** delia is resolved via the `created`-timestamp scan
- **Then** that entry is treated as ineligible for newest-selection (never chosen, never crashes the scan); selection proceeds among the remaining entries with a valid `created`, and if no entry under the prefix has a valid `created` the resolver fails closed per Error Scenario 1

## Error Conditions
**Error Scenario 1: No eligible entries exist under the vendor's prefix**
- Error message: `"no eligible %s-prefixed model found in catalog for persona %q"` (e.g. `"no eligible z-ai/-prefixed model found in catalog for persona \"glenna\""`)
- HTTP status / error code: N/A (library error) — the resolver must fail closed rather than fall back to a stale or zero-value slug

**Error Scenario 2: Vendor-prefix table has no entry for the persona's family (defensive check)**
- Error message: `"no vendor-prefix mapping configured for persona family %q"`
- HTTP status / error code: N/A — guards against a future persona being added to the `created`-timestamp path without its prefix being registered

## Performance Requirements
- **Response Time:** A single linear scan over the catalog's ~344 entries (per the live spike count in `documentation/openrouter-catalog-api.md`) filtered by string-prefix match, completing in under 5ms per persona resolution
- **Throughput:** All 3 `created`-timestamp personas (delia, quinn, glenna) resolve in a single combined pass under 20ms against the fixture

## Security Considerations
- **Authentication/Authorization:** N/A for the scan logic itself; the catalog fetch that supplies the model list reuses `internal/personas/client.go:35`'s `HTTPClient` seam and its existing timeout/retry/size-cap guards (`client.go:56-73`)
- **Input Validation:** Vendor-prefix string comparison is exact-prefix (`strings.HasPrefix`), never a substring or regex match, so a crafted catalog entry like `z-ai-evil/model` cannot be mistaken for `z-ai/model`

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Catalog snapshot fixture with ≥2 `deepseek/`, ≥2 `qwen/`, and ≥2 `z-ai/` entries at distinct `created` timestamps, zero `glm/`-prefixed entries (to prove the resolver never depends on that namespace existing), per `documentation/catalog-snapshot-fixture.md`
**Mock/Stub Requirements:** `httptest.NewServer` serving the fixture, reusing the pattern already documented in `documentation/catalog-snapshot-fixture.md`'s Code Examples section

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] delia and quinn resolve to the newest eligible `deepseek/`/`qwen/`-prefixed slug respectively
- [ ] glenna resolves to the newest eligible `z-ai/`-prefixed slug, with an explicit regression test proving the resolver never assumes a `glm/` namespace exists
- [ ] A tie on `created` timestamp resolves deterministically (descending lexicographic slug sort) and repeatably, including against a shuffled-order fixture
- [ ] A candidate with absent/zero/unparseable `created` is excluded from newest-selection without crashing the scan
- [ ] No eligible entries under a vendor prefix produces a descriptive error, not a silent empty/zero-value slug

**Manual Review:**
- [ ] Code reviewed and approved
