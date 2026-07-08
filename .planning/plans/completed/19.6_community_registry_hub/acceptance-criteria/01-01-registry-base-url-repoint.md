# Acceptance Criteria: Registry Base URL Repointed to samestrin/atcr

**Related User Story:** [01: Community-Canonical Fetch-and-Pin Distribution](../user-stories/01-community-canonical-fetch-and-pin-distribution.md)

**Design References:** [fetch-and-distribution.md](../documentation/fetch-and-distribution.md), [persona-yaml-schema.md](../documentation/persona-yaml-schema.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go constant + resolver function | `internal/personas/client.go` |
| Test Framework | Go `testing` + `httptest.NewServer` | existing pattern in `internal/personas` tests |
| Key Dependencies | none new — reuses `net/http`, `os` | `BaseURL()` env-override-else-constant logic unchanged |

### Related Files (from codebase-discovery.json)
- `internal/personas/client.go` (line 24 `RegistryBaseURL`, `BaseURL()`) — modify: repoint the default base URL from `https://raw.githubusercontent.com/atcr/personas/main` to the `samestrin/atcr` in-repo community path.
- `cmd/atcr/init.go` (`runInit`, lines 76-78 force/anyExist gate, lines 96-118 O_EXCL write guard) — modify: fetch-and-pin community personas from the repointed base URL instead of copying embedded built-ins.
- `cmd/atcr/quickstart.go` (`runQuickstart`) — modify: inherit the fetch-and-pin behavior through its `runInit` call.
- `internal/personas/install.go` — reference: validates and atomically writes fetched persona YAML; called by the fetch-and-pin path.
- `internal/personas/upgrade.go` — reference: compares installed version to remote version; works automatically once the base URL is repointed.
- `internal/personas/list.go` (`PersonaMeta`, source labeling, lines 38-47, 117-172) — reference: displays pinned versions and `Source: community` for fetch-installed personas.
- `personas/community/index.json` — create: in-repo canonical index served by the repointed `RegistryBaseURL`.
- `personas/community/<slug>.yaml` — create: community persona YAML files referenced by the new index.
- `docs/personas-install.md` — modify: update default URL references and document the fetch-and-pin flow.
- `README.md` — modify: update any documented default URL references.
- `internal/personas/search_test.go` — create: mock-registry tests verifying the repointed URL construction and fetch behavior.
- `personas/community_test.go` — create: end-to-end tests against the in-repo community index.

## Happy Path Scenarios
**Scenario 1: Default BaseURL resolves to samestrin/atcr**
- **Given** `ATCR_PERSONAS_URL` is unset in the environment
- **When** `personas.BaseURL()` is called
- **Then** it returns exactly `https://raw.githubusercontent.com/samestrin/atcr/main/personas/community` (the new canonical community path), not `https://raw.githubusercontent.com/atcr/personas/main`

**Scenario 2: Env override still takes precedence**
- **Given** `ATCR_PERSONAS_URL` is set to `https://example.test/mock-registry`
- **When** `personas.BaseURL()` is called
- **Then** it returns `https://example.test/mock-registry`, unaffected by the constant change

**Scenario 3: Content scenario — `RegistryBaseURL` constant value is byte-for-byte correct**
- **Given** the compiled `personas` package
- **When** the `RegistryBaseURL` string is read
- **Then** it equals `https://raw.githubusercontent.com/samestrin/atcr/main/personas/community` byte-for-byte

## Edge Cases
**Edge Case 1: Env var set to whitespace-only value**
- **Given** `ATCR_PERSONAS_URL="   "` (whitespace only)
- **When** `personas.BaseURL()` is called
- **Then** the trimmed value is empty, so it falls back to the new `RegistryBaseURL` default (existing trim-then-check behavior preserved)

**Edge Case 2: Env var set to empty string**
- **Given** `ATCR_PERSONAS_URL=""`
- **When** `personas.BaseURL()` is called
- **Then** it falls back to the new `RegistryBaseURL` default

**Edge Case 3: FetchIndex/FetchPersonaYAML called with the new default**
- **Given** the new `RegistryBaseURL` of `https://raw.githubusercontent.com/samestrin/atcr/main/personas/community` and no env override
- **When** `FetchIndex` or `FetchPersonaYAML("security")` builds a request URL
- **Then** the resulting URL is exactly `https://raw.githubusercontent.com/samestrin/atcr/main/personas/community/index.json` or `https://raw.githubusercontent.com/samestrin/atcr/main/personas/community/security.yaml` respectively, with no double slashes and no trailing-slash ambiguity

## Error Conditions
**Error Scenario 1: New default URL unreachable (repo not yet populated)**
- **Given** the repointed `RegistryBaseURL` and no env override
- **When** `FetchIndex` is called against a server returning HTTP 404 for `/personas/community/index.json`
- **Then** it returns `ErrIndexNotFound` wrapped as `failed to fetch community repo index: unexpected status 404`, and the process exits non-zero

**Error Scenario 2: New default URL returns a non-2xx status**
- **Given** the repointed `RegistryBaseURL` and no env override
- **When** `FetchIndex` is called against a server returning HTTP 500 for `/personas/community/index.json`
- **Then** it returns an error containing `unexpected status 500`, and the process exits non-zero

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
