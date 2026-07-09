# Acceptance Criteria: Online init/quickstart Install a Working, Non-Empty Community Roster

**Related User Story:** [07: init/quickstart Roster Reconciliation](../user-stories/07-init-quickstart-roster-reconciliation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI init/quickstart flow (`cmd/atcr` package) | Reconciliation may land in `installCommunityPersonas`, its shared caller-side roster input, or `personas/community/index.json`, per whichever of the two Proposed Solution options is selected at implementation/design-sprint time — this AC does not pick one |
| Test Framework | `testing` + `testify/require` + `net/http/httptest` | Matches the existing pattern in `cmd/atcr/init_test.go` (`TestInstallCommunityPersonas_*`) |
| Key Dependencies | `internal/personas` (`FetchIndex`, `HTTPClient`), `personas` (`builtins.Names()`) | No new dependencies required |

## Related Files
- `cmd/atcr/init.go` - modify: the roster passed at `init.go:47` (and/or `installCommunityPersonas` at `init.go:96`) must resolve to at least one persona actually present in `personas/community/index.json`
- `cmd/atcr/quickstart.go` - modify: the roster passed at `quickstart.go:102` must consume the same reconciled source as `init.go`, producing the identical non-empty install outcome
- `personas/community/index.json` - modify (only if Option A — publishing built-ins into the index — is the chosen path): add the model-agnostic carve-out entries; unchanged if Option B is chosen
- `cmd/atcr/init_test.go` - modify: add/extend a test that runs `installCommunityPersonas` (or its reconciled equivalent) against the real, unmodified `personas/community/index.json` content and asserts at least one persona file is written to disk

## Happy Path Scenarios
**Scenario 1: Online `init` installs a non-empty community persona set**
- **Given** a clean directory with no `.atcr/` present and network access to the (real-shaped) community index
- **When** `atcr init` runs without `--offline`
- **Then** at least one persona `.yaml` (and co-located `.md` where the persona ships a custom prompt) is written under `.atcr/personas/`, and the command exits 0

**Scenario 2: Online `quickstart` installs the identical roster outcome as `init`**
- **Given** a clean directory and network access, running `atcr quickstart` with community fetch enabled (not `--offline`)
- **When** the quickstart flow reaches its `installCommunityPersonas` call (`quickstart.go:102`)
- **Then** the same non-empty persona set installs as in Scenario 1, using the same reconciled roster/index source as `init.go`

## Edge Cases
**Edge Case 1: Community index reachable but momentarily short a roster member**
- **Given** the index is fetched successfully but happens to omit one name from the reconciled roster (e.g., a persona was pulled from the catalog)
- **When** `installCommunityPersonas` runs
- **Then** the remaining roster members that ARE in the index still install successfully; the run is not aborted for the one absent name (existing skip-then-continue behavior is preserved for genuinely absent entries)

**Edge Case 2: Reconciliation choice (Option A) must not weaken built-in model-agnosticism**
- **Given** Option A (publishing built-ins into the community channel) is the implementation chosen
- **When** the built-in-sourced entries are added to `personas/community/index.json` or its equivalent carve-out
- **Then** none of those entries carry a `provider`/`model` binding — they remain usable with any provider/model, preserving constraint C2

## Error Conditions
**Error Scenario 1: Community index fetch fails entirely (pre-existing behavior, must not regress)**
- Error message: `"failed to fetch community personas: %w — retry with --force --offline to use the embedded built-in personas"`
- HTTP status / error code: N/A (Go `error`, wraps the underlying transport/HTTP error); CLI exits non-zero

**Error Scenario 2: Fetched index is empty (pre-existing behavior, must not regress)**
- Error message: `"community persona index is empty: no personas to install — retry with --force --offline to use the embedded built-in personas"`
- HTTP status / error code: N/A (Go `error`); CLI exits non-zero

## Performance Requirements
- **Response Time:** No new network round-trips introduced beyond the existing single `FetchIndex` call per `init`/`quickstart` invocation
- **Throughput:** N/A (single-user CLI invocation, not a concurrent service)

## Security Considerations
- **Authentication/Authorization:** N/A — community index fetch is unauthenticated, unchanged by this story
- **Input Validation:** If Option A is chosen, added index entries must pass the same `FetchIndex`/unit-install validation already applied to existing entries (no relaxed parsing path for built-in-sourced entries)

## Test Implementation Guidance
**Test Type:** INTEGRATION (existing `httptest.Server`-backed pattern in `cmd/atcr/init_test.go`)
**Test Data Requirements:** A fixture community index matching the real `personas/community/index.json` shape (or the file itself, loaded directly) so the test proves the fix against production data, not a synthetic stand-in
**Mock/Stub Requirements:** `httptest.NewServer` serving the index JSON, matching `TestInstallCommunityPersonas_FetchAndPin`'s existing setup; no mocking of `installCommunityPersonas` itself — exercise it directly

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A test proves online `init` installs at least one community persona against the real index content
- [ ] A test proves online `quickstart` installs the identical non-empty roster via the same reconciled source
- [ ] If Option A is chosen, a test asserts built-in-sourced index entries carry no provider/model binding

**Manual Review:**
- [ ] Code reviewed and approved
