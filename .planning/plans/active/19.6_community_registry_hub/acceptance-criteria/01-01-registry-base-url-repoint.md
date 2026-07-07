# Acceptance Criteria: Registry Base URL Repointed to samestrin/atcr

**Related User Story:** [01: Community-Canonical Fetch-and-Pin Distribution](../user-stories/01-community-canonical-fetch-and-pin-distribution.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go constant + resolver function | `internal/personas/client.go` |
| Test Framework | Go `testing` + `httptest.NewServer` | existing pattern in `internal/personas` tests |
| Key Dependencies | none new — reuses `net/http`, `os` | `BaseURL()` env-override-else-constant logic unchanged |

## Related Files
- `internal/personas/client.go` - modify: change `RegistryBaseURL` (line 24) from `https://raw.githubusercontent.com/atcr/personas/main` to the `samestrin/atcr` repo's in-repo persona path (e.g. `https://raw.githubusercontent.com/samestrin/atcr/main/personas/community`); update the doc comment above the constant to reflect the new canonical source.
- `internal/personas/client_test.go` - modify: add/update a test asserting `RegistryBaseURL` (or `BaseURL()` with `ATCR_PERSONAS_URL` unset) resolves to the new `samestrin/atcr` path.
- `docs/registry.md` (or equivalent onboarding doc) - modify: update any documented default URL reference to match the new constant.

## Happy Path Scenarios
**Scenario 1: Default BaseURL resolves to samestrin/atcr**
- **Given** `ATCR_PERSONAS_URL` is unset in the environment
- **When** `personas.BaseURL()` is called
- **Then** it returns the `samestrin/atcr` repo's in-repo persona path (not the old `atcr/personas` URL)

**Scenario 2: Env override still takes precedence**
- **Given** `ATCR_PERSONAS_URL` is set to `https://example.test/mock-registry`
- **When** `personas.BaseURL()` is called
- **Then** it returns `https://example.test/mock-registry`, unaffected by the constant change

## Edge Cases
**Edge Case 1: Env var set to whitespace-only value**
- **Given** `ATCR_PERSONAS_URL="   "` (whitespace only)
- **When** `personas.BaseURL()` is called
- **Then** the trimmed value is empty, so it falls back to the new `RegistryBaseURL` default (existing trim-then-check behavior preserved)

**Edge Case 2: FetchIndex/FetchPersonaYAML called with the new default**
- **Given** the new `RegistryBaseURL` and no env override
- **When** `FetchIndex` or `FetchPersonaYAML` builds a request URL
- **Then** the resulting URL is `<new-base>/index.json` or `<new-base>/<name>.yaml` respectively, with no double slashes or malformed joins

## Error Conditions
**Error Scenario 1: New default URL unreachable (repo not yet populated)**
- Error message: `"failed to fetch community repo index: unexpected status 404"` (or equivalent from existing `fetch()` error wrapping)
- HTTP status / error code: propagated from `fetch()` — 404 maps to `ErrIndexNotFound` / `ErrPersonaNotFound`, other non-2xx maps to a generic `unexpected status %d` error

## Performance Requirements
- **Response Time:** No change — `fetchTimeout` (30s) and `fetchBodyLimit` (5 MB) are unaffected by this AC.
- **Throughput:** N/A (single constant change, no new call volume).

## Security Considerations
- **Authentication/Authorization:** None required; the fetch path remains anonymous, unauthenticated HTTPS GET as before.
- **Input Validation:** No new inputs; existing `validatePersonaName` and `fetchBodyLimit` guards are unaffected by the URL change.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** No live network; assert on the string value returned by `BaseURL()` and `RegistryBaseURL`.
**Mock/Stub Requirements:** None needed for the constant-value test; existing `httptest.NewServer` + `ATCR_PERSONAS_URL` pattern reused for `FetchIndex`/`FetchPersonaYAML` URL-construction tests.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `RegistryBaseURL` constant updated to the `samestrin/atcr` in-repo persona path
- [ ] `BaseURL()` env-override behavior verified unchanged via test
- [ ] Doc comment and any docs referencing the old URL updated

**Manual Review:**
- [ ] Code reviewed and approved
