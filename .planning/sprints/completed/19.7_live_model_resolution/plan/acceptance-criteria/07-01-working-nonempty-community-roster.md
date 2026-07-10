# Acceptance Criteria: Online init/quickstart Install a Working, Non-Empty Community Roster

**Related User Story:** [07: init/quickstart Roster Reconciliation](../user-stories/07-init-quickstart-roster-reconciliation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI init/quickstart flow (`cmd/atcr` package) | **LOCKED (Option B):** the roster is derived from the fetched community index's own entries; no change to `personas/community/index.json`'s contents or schema |
| Test Framework | `testing` + `testify/require` + `net/http/httptest` | Matches the existing pattern in `cmd/atcr/init_test.go` (`TestInstallCommunityPersonas_*`) |
| Key Dependencies | `internal/personas` (`FetchIndex`, `HTTPClient`), `personas` (`builtins.Names()`) | No new dependencies required |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/init.go:47` — modify: the roster passed to `installCommunityPersonas` becomes the fetched index's own entry names, not `builtins.Names()`.
- `cmd/atcr/init.go:96` (`installCommunityPersonas`) — modify: derive the install roster from the already-fetched `[]PersonaIndexEntry` internally (or accept the derived roster from a single shared caller-side helper) so the fix is expressed once.
- `cmd/atcr/quickstart.go:102` — modify: the roster passed must consume the same reconciled (index-derived) source as `init.go:47`, producing the identical non-empty install outcome.
- `personas/community/index.json:1` — reference only: no schema or content change; the index's existing 10 entries are what the derived roster installs.
- `cmd/atcr/init_test.go` — modify: add/extend a test that runs `installCommunityPersonas` (or its reconciled equivalent) against the real, unmodified `personas/community/index.json` content and asserts at least one persona file is written to disk.
- `personas/personas.go:19` (`builtins.Names()`) — reference: the 9 embedded model-agnostic built-ins; no longer passed as the community fetch-and-pin roster (still used unchanged by `runInit`'s separate embedded-scaffold step).
- `documentation/existing-resolver-patterns.md` — reference: documents the two-call-site drift risk (TD-006/TD-007) and why AC7 must be fixed in one shared location.

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

**Edge Case 2: A future index change installs automatically, without a code change**
- **Given** `personas/community/index.json` gains an 11th entry (or loses one) upstream, after this fix ships
- **When** online `init`/`quickstart` next runs
- **Then** the derived roster reflects the new index contents automatically (installs the added persona, or simply omits the removed one) — no `builtins.Names()`-style hardcoded list needs updating

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
- **Input Validation:** No change — the derived roster's entries pass through the same `FetchIndex`/unit-install validation already applied today; deriving the roster from `entries` rather than an external list does not introduce a new parsing or validation path

## Test Implementation Guidance
**Test Type:** INTEGRATION (existing `httptest.Server`-backed pattern in `cmd/atcr/init_test.go`)
**Test Data Requirements:** A fixture community index matching the real `personas/community/index.json` shape (or the file itself, loaded directly) so the test proves the fix against production data, not a synthetic stand-in
**Mock/Stub Requirements:** `httptest.NewServer` serving the index JSON, matching `TestInstallCommunityPersonas_FetchAndPin`'s existing setup; no mocking of `installCommunityPersonas` itself — exercise it directly

## Definition of Done
**Auto-Verified:**
- [x] All tests passing — `go test ./...` full suite green
- [x] No linting errors — `golangci-lint run ./cmd/atcr/` 0 issues; `go vet`/`gofmt` clean
- [x] Build succeeds — `go build ./...` exit 0

**Story-Specific:**
- [x] A test proves online `init` installs at least one community persona against the real index content — `TestInit_Online_InstallsNonEmptyCommunityRoster` + `TestInit_Online_NoSkipWarnings` (real index, `NotEmpty` pin dir)
- [x] A test proves online `quickstart` installs the identical non-empty roster via the same reconciled source — `TestQuickstart_Online_InstallsNonEmptyCommunityRoster` + `TestRosterReconciliation_InitQuickstartParity` (identical index-derived set both paths)
- [x] A test proves the roster is derived from the fetched index (not a hardcoded list) — `TestInstallCommunityPersonas_RosterTracksIndexContents` (grow index → install set changes, no code change) + `TestInstallCommunityPersonas_NilRosterDerivesFromIndex` (disjoint-from-builtins names install)

**Manual Review:**
- [ ] Code reviewed and approved
