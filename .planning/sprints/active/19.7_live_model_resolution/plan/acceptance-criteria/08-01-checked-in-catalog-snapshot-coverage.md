# Acceptance Criteria: Checked-In Catalog Snapshot Covers Every Resolver Branch

**Related User Story:** [08: Catalog Snapshot Fixture, Refresh Command & Documentation](../user-stories/08-catalog-snapshot-refresh-command-and-docs.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Checked-in JSON test fixture (`internal/personas/testdata/catalog_snapshot.json`) | Drives zero-live-network resolver/catalog tests |
| Test Framework | `testing` + `testify` (`assert`/`require`), table-driven | Follows `internal/personas/client_test.go` fixture-loading patterns |
| Key Dependencies | Story 3's resolver branches; `httptest.NewServer` | No new dependency |

### Related Files (from codebase-discovery.json)
- `internal/personas/testdata/catalog_snapshot.json` — create: the checked-in snapshot of OpenRouter `/api/v1/models`.
- `internal/personas/catalog_test.go` — modify/create: tests load the fixture via an `httptest` server backed by this file.
- `internal/personas/catalog.go` — reference: the catalog client parses the fixture's schema.
- `internal/personas/client.go:35` (`HTTPClient`) — reference only: the fixture is served through an `httptest.NewServer`-backed `HTTPClient` so tests remain zero-live-network.
- `personas/community/index.json:1` — reference only: source of the 10 existing pinned slugs the fixture must include for zero-migration coverage.
- `documentation/catalog-snapshot-fixture.md` — reference: fixture contents, refresh command rationale, and zero-live-network CI discipline.

## Happy Path Scenarios

**Scenario 1: Fixture contains alias entries for all alias-covered vendors**
- **Given** the checked-in fixture is loaded by the catalog client
- **When** Story 3's alias resolver runs for anthony, sonny, gene, milo, gia, flint, and celeste
- **Then** each persona resolves to its expected `~`-prefixed `-latest` alias without requiring a catalog scan

**Scenario 2: Fixture contains multiple candidates under the `created`-timestamp prefixes**
- **Given** the fixture contains at least two models under `deepseek/`, two under `qwen/`, and two under `z-ai/`
- **When** Story 3's `created`-timestamp resolver runs for delia, quinn, and glenna
- **Then** each persona resolves to the newest-by-`created` model under its correct vendor prefix (including `z-ai/` for glenna, never `glm/`)

**Scenario 3: Fixture contains all 10 existing pinned slugs**
- **Given** the fixture includes the concrete slugs currently stored in `personas/community/index.json`
- **When** `models check` compares an installed persona's lock against the fixture
- **Then** no "missing slug" condition is reported for any of the 10 seed locks, proving zero-migration compatibility

## Edge Cases

**Edge Case 1: Preview/beta/exp-tokened model is the newest under a vendor prefix**
- **Given** the fixture contains a `qwen/` model whose `id` or `canonical_slug` includes a preview token and a `created` timestamp newer than the GA model
- **When** the resolver selects under `@stable`
- **Then** the preview-tokened model is skipped and the next-newest GA model is selected

**Edge Case 2: Newest model under a prefix carries a non-null `expiration_date`**
- **Given** the fixture contains a `deepseek/` model with the newest `created` timestamp and a non-null `expiration_date`
- **When** the resolver selects under `@stable`
- **Then** the expiring model is excluded and the next-newest non-expiring model is selected; separately, `models check` reports the deprecation condition for any installed lock that matches an expiring model

## Error Conditions

**Error Scenario 1: Fixture file is missing or unreadable**
- Error message: surfaced by `os.ReadFile` and wrapped by the test setup as a clear test failure
- Behavior: tests fail at setup time with a message naming the missing fixture path

## Performance Requirements
- **Response Time:** N/A — the fixture is loaded from disk; no network call is made in tests
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** None — the fixture contains only public catalog metadata
- **Input Validation:** Tests assert the fixture JSON decodes into the expected catalog-entry struct shape; a malformed fixture fails the package tests immediately

## Test Implementation Guidance
**Test Type:** UNIT (resolver tests in `internal/personas/catalog_test.go`)
**Test Data Requirements:** `internal/personas/testdata/catalog_snapshot.json` with the coverage described above
**Mock/Stub Requirements:** An `httptest.NewServer` that serves the fixture contents as the response body for `/api/v1/models`

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Fixture exists at `internal/personas/testdata/catalog_snapshot.json`
- [ ] Fixture covers every alias-covered vendor, the `deepseek/`/`qwen/`/`z-ai/` prefixes, preview tokens, `expiration_date`, and all 10 existing pinned slugs
- [ ] Resolver tests run against the fixture via `httptest` with zero live network

**Manual Review:**
- [ ] Code reviewed and approved
