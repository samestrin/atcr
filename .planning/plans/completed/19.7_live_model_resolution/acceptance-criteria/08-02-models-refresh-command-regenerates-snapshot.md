# Acceptance Criteria: `atcr models refresh` Regenerates the Snapshot from Live `/api/v1/models`

**Related User Story:** [08: Catalog Snapshot Fixture, Refresh Command & Documentation](../user-stories/08-catalog-snapshot-refresh-command-and-docs.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cobra subcommand under `atcr models` (`cmd/atcr/models.go`) | Sibling to `atcr models check` from Story 5 |
| Test Framework | `testing` + `testify`, `httptest` | Mirrors `cmd/atcr/personas_test.go` and `internal/personas/client_test.go` patterns |
| Key Dependencies | Story 3's catalog client; `OPENROUTER_API_KEY` for the live call | Network is touched only when the command is explicitly invoked |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/models.go` — modify: add `newModelsRefreshCmd()` registered under the `models` family.
- `internal/personas/catalog.go` — modify/reference: the refresh command reuses the catalog client's live fetch path.
- `internal/personas/testdata/catalog_snapshot.json` — modify: the command rewrites this file.
- `cmd/atcr/models_test.go` — modify: add tests using an `httptest` fake catalog server.
- `cmd/atcr/main.go:202` (`newRootCmd` `AddCommand` list) — reference: the registration point for the `models` command family (modified in AC 05-01).
- `documentation/catalog-snapshot-fixture.md` — reference: explains the refresh command rationale and zero-live-network CI discipline.

## Happy Path Scenarios

**Scenario 1: Refresh command rewrites the fixture from a live response**
- **Given** `atcr models refresh` is run with a valid `OPENROUTER_API_KEY` (or an `httptest` fake in tests)
- **When** the command fetches `/api/v1/models` and receives a JSON model array
- **Then** it writes the array to `internal/personas/testdata/catalog_snapshot.json` (preserving readable formatting such as indentation) and prints a confirmation line naming the output path and the number of models written

**Scenario 2: Refresh command uses the same parser as production resolver tests**
- **Given** the newly written fixture
- **When** `internal/personas/catalog_test.go` loads it through the catalog client
- **Then** parsing succeeds and the same resolver tests that passed before the refresh continue to pass, proving the refreshed file is structurally compatible

## Edge Cases

**Edge Case 1: Refresh receives an empty model array**
- **Given** the live endpoint returns an empty `data` array
- **When** the command writes the fixture
- **Then** the command exits non-zero with a clear error ("refusing to overwrite fixture with empty catalog") and does not commit an empty snapshot

**Edge Case 2: Refresh fetch fails (network or auth error)**
- **Given** the catalog fetch returns a non-2xx status or a transport error
- **When** the command handles the error
- **Then** the existing fixture is left untouched and the command exits non-zero with the underlying error message

## Error Conditions

**Error Scenario 1: Missing `OPENROUTER_API_KEY` for the live default path**
- Error message: `"OPENROUTER_API_KEY is required to refresh the catalog snapshot"`
- Exit code: `2` (usage/configuration error)
- Behavior: no file is written

**Error Scenario 2: Fixture path is not writable**
- Error message: wrapped `os.WriteFile` error naming the path
- Exit code: `2`
- Behavior: the existing fixture remains unchanged

## Performance Requirements
- **Response Time:** Bounded by the catalog client's existing timeout/backoff policy; the refresh is a single fetch + disk write
- **Throughput:** N/A — one-shot command

## Security Considerations
- **Authentication/Authorization:** Reads `OPENROUTER_API_KEY` from the environment and sends it as a Bearer token to `api.openrouter.ai`; no key is logged or written to the fixture
- **Input Validation:** The fetched JSON is decoded into the catalog-entry struct before writing; malformed responses are rejected with an error and do not overwrite the fixture

## Test Implementation Guidance
**Test Type:** INTEGRATION (Cobra command boundary) with an `httptest` fake OpenRouter server
**Test Data Requirements:** A minimal valid `/api/v1/models` JSON response served by the fake server; a temp directory used as the fixture output path so tests do not mutate the real `testdata/` file
**Mock/Stub Requirements:** `httptest.NewServer` returning the catalog JSON; a test may also verify that the command respects an `--output` flag or env override pointing at the temp path

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `atcr models refresh` is registered under the `models` command family
- [ ] The command fetches `/api/v1/models` and writes the response to the fixture path
- [ ] Errors (missing key, empty response, fetch failure, unwritable path) are reported and leave the existing fixture untouched
- [ ] Tests prove the refreshed fixture is parseable by the catalog client

**Manual Review:**
- [ ] Code reviewed and approved
