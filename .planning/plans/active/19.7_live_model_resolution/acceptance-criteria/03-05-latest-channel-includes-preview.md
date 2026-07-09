# Acceptance Criteria: `@latest` Channel Includes Preview-Tagged Models in the `created`-Timestamp Scan

**Related User Story:** [03: Hybrid Resolver (Alias / Created-Timestamp / Explicit-Pin)](../user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go resolver channel branch: `@latest` skips the `@stable` exclusion filter entirely in the `created`-timestamp scan | Per Proposed Solution #4 (Channels): "`@latest` (includes preview)"; per story AC2 overview, the exclusion applies "unless the channel is `@latest`" |
| Test Framework | `testing` + `httptest.NewServer`, table-driven subtests directly comparable against the AC 03-04 `@stable` cases | Same fixture, different channel value, to make the behavioral contrast explicit in test names/output |
| Key Dependencies | Same catalog schema fields as AC 03-04 (`created`, slug tokens, `expiration_date`); no new dependency | `expiration_date` honoring — deprecation — is a separate signal from the preview/beta/exp exclusion; this AC clarifies which of the two `@latest` actually bypasses |

## Related Files
- `internal/personas/catalog.go` - create: the channel-conditional branch in the `created`-timestamp scan — `@latest` selects the newest-by-`created` entry without applying the preview/beta/exp token exclusion
- `internal/personas/catalog_test.go` - create: unit tests using the same preview-tagged fixture entries as AC 03-04's tests, asserting `@latest` returns the preview-tagged newest entry that `@stable` would have skipped
- `internal/personas/testdata/catalog_snapshot.json` - create: shared fixture with AC 03-04 (same file); no separate fixture needed since both channels are exercised against identical data
- `.planning/plans/active/19.7_live_model_resolution/user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md` - reference: AC2 overview text specifying the `@stable`/`@latest` conditional exclusion

## Happy Path Scenarios
**Scenario 1: `@latest` selects a preview-tagged newest entry that `@stable` would skip**
- **Given** the `deepseek/` prefix's newest-by-`created` entry is `deepseek/deepseek-v5-preview`
- **When** delia is resolved with channel `@latest`
- **Then** the resolver returns `deepseek/deepseek-v5-preview` — directly contrasting with AC 03-04 Scenario 1, where the same fixture under `@stable` returns the older non-preview entry

**Scenario 2: `@latest` still performs a `created`-timestamp comparison, not "always return every preview build"**
- **Given** the `qwen/` prefix has two preview-tagged entries at different `created` timestamps plus one non-preview entry
- **When** quinn is resolved with channel `@latest`
- **Then** the resolver returns the single newest-by-`created` entry among all of them (preview or not) — `@latest` widens eligibility to include preview-tokened entries, it does not change the "exactly one newest" selection rule

**Scenario 3: `@latest` on a vendor prefix with no preview-tagged entries behaves identically to `@stable`**
- **Given** the `z-ai/` prefix's newest-by-`created` entry carries no preview/beta/exp token and no non-null `expiration_date`
- **When** glenna is resolved under both `@stable` and `@latest`
- **Then** both channels return the identical slug — proving `@latest` is a strict superset of `@stable`'s eligible set, never a different selection when no exclusion would have applied

## Edge Cases
**Edge Case 1: `@latest` still honors `expiration_date` (deprecation), only the preview/beta/exp token exclusion is bypassed**
- **Given** the newest-by-`created` entry under a vendor prefix has a non-null `expiration_date` (deprecated) but no preview/beta/exp token
- **When** resolution runs with channel `@latest`
- **Then** the expected/documented behavior is made explicit by this AC's test: either (a) `@latest` also returns a deprecated entry (channels only ever affect preview inclusion, deprecation is always excluded), or (b) `@latest` excludes it too — whichever the implementation chooses must be a single, tested, documented rule, since the story text ties the exclusion clause ("unless the channel is `@latest`") to the whole `@stable` heuristic sentence and this is the one place the two signals (preview-token vs. `expiration_date`) could be conflated; this AC requires the test suite to assert one explicit, unambiguous behavior rather than leaving it implicit

**Edge Case 2: `@latest` on an alias-covered persona is a no-op**
- **Given** an alias-covered persona (e.g. gia) has channel `@latest`
- **When** resolved
- **Then** behavior is unchanged from AC 03-01 — the alias path does not consult channel at all, confirming `@latest`/`@stable` only affects the `created`-timestamp strategy for delia/quinn/glenna

## Error Conditions
**Error Scenario 1: Channel value is neither `@stable` nor `@latest` (typo or unrecognized value)**
- Error message: `"unrecognized channel %q for persona %q: expected \"@stable\" or \"@latest\""`
- HTTP status / error code: N/A (library error) — the resolver fails closed on an unrecognized channel value rather than silently defaulting to either behavior

## Performance Requirements
- **Response Time:** `@latest` skips one filter predicate compared to `@stable`'s scan — strictly cheaper or equal, never slower
- **Throughput:** No measurable overhead beyond AC 03-02's base scan performance bound

## Security Considerations
- **Authentication/Authorization:** N/A — pure in-memory channel-conditional filtering
- **Input Validation:** The channel string originates from the persona's binding (Story 2's schema); this resolver validates it against the two known literals and rejects anything else per Error Scenario 1, rather than treating an unrecognized value as an implicit `@stable` or `@latest`

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Same catalog snapshot fixture as AC 03-04 (shared preview-tagged and expiring entries), exercised with channel `@latest` instead of `@stable` to prove the behavioral contrast
**Mock/Stub Requirements:** `httptest.NewServer` serving the shared fixture; no additional mocking

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `@latest` returns a preview-tagged newest entry that `@stable` excludes, against the identical fixture
- [ ] `@latest` still performs exactly-one-newest selection, not "return all preview builds"
- [ ] The `expiration_date` vs. preview-token distinction under `@latest` is resolved to one explicit, tested rule (not left ambiguous)
- [ ] An unrecognized channel value produces a descriptive error rather than a silent default
- [ ] Alias-covered personas are unaffected by channel value (confirms channel only gates the `created`-timestamp strategy)

**Manual Review:**
- [ ] Code reviewed and approved
