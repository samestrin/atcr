# Acceptance Criteria: Single Shared Reconciliation Point and Backward Compatibility

**Related User Story:** [07: init/quickstart Roster Reconciliation](../user-stories/07-init-quickstart-roster-reconciliation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI init/quickstart flow (`cmd/atcr` package) | Regression guard against the TD-006/TD-007 two-call-site drift pattern flagged in the 19.6 postmortem |
| Test Framework | `testing` + `testify/require` + `net/http/httptest` | Same call-site-parity assertion style should run against both `init.go:47` and `quickstart.go:102` |
| Key Dependencies | `internal/personas` (`FetchIndex`, `HTTPClient`), `personas` (`builtins.Names()`) | No new dependencies required |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/init.go:96` (`installCommunityPersonas`) — modify: the roster/index reconciliation must be expressed in exactly ONE shared location — either inside `installCommunityPersonas` itself, or via a single shared roster-producing helper both `init.go:47` and `quickstart.go:102` call — not duplicated per call site.
- `cmd/atcr/init.go:47` — modify: must consume the same reconciled roster source as `quickstart.go:102`.
- `cmd/atcr/quickstart.go:102` — modify: must consume the identical shared source as `init.go:47`; no call-site-local roster logic.
- `cmd/atcr/init_test.go` — modify: add a test that runs the same reconciliation assertion (non-empty install, zero skip-warnings) against the `init` call path.
- `cmd/atcr/quickstart_test.go` — modify: add the parallel test running the identical assertion against the `quickstart` call path, proving both consume the same source rather than two independently-patched copies.
- `personas/personas.go:19` (`builtins.Names()`) — reference: the 9 embedded model-agnostic built-ins that currently form the disjoint roster side of TD-011.
- `documentation/existing-resolver-patterns.md` — reference: documents the two-call-site drift risk (TD-006/TD-007) and why AC7 must be fixed in one shared location.

## Happy Path Scenarios
**Scenario 1: A single source change propagates to both call sites**
- **Given** the reconciliation is implemented as one shared roster-derivation point (LOCKED: Option B — deriving the roster from the fetched index, per plan.md Clarifications)
- **When** that shared source is exercised via `init.go:47`
- **Then** the identical resolved roster is also what `quickstart.go:102` consumes — verified by running the same test assertion (non-empty install; zero skip-warnings) against both call paths and observing identical results

**Scenario 2: Existing on-disk personas are preserved untouched**
- **Given** a `.atcr/personas/` directory that already contains a previously-installed or hand-edited community persona (e.g., an existing `<name>.yaml` and/or `<name>.md`)
- **When** online `init` or `quickstart` re-runs `installCommunityPersonas` with the reconciled roster
- **Then** the never-overwrite guard at `init.go:139` still applies unchanged — the existing file(s) are left byte-for-byte untouched, and the "already installed — leaving it untouched" message prints for that name

## Edge Cases
**Edge Case 1: All-or-nothing rollback behavior is preserved**
- **Given** a mid-roster install failure occurs (e.g., a fetch/validation error partway through installing the reconciled roster)
- **When** `installCommunityPersonas` aborts
- **Then** every persona file this run created is rolled back (removed), while any pre-existing file is left untouched — matching the existing behavior verified by `TestInstallCommunityPersonas_MidRosterFailure_RollsBack`

**Edge Case 2: Built-in `.md` scaffolds from `atcr init`'s non-community step are untouched**
- **Given** `atcr init` has already written the 9 embedded built-in `.md` files via `runInit`/`initTargets` (a step unrelated to `installCommunityPersonas`)
- **When** the reconciled community roster subsequently installs (or attempts to install) alongside them
- **Then** the built-in scaffold files are not modified, renamed, or removed by the community-install step (Option B never touches the embedded built-in scaffold or `builtins.Names()` at all — the two are now fully decoupled)

## Error Conditions
**Error Scenario 1: Drift regression — one call site fixed, the other not**
- Error message: N/A — this is a structural regression this AC's tests must catch, not a runtime error message
- Detection: the parallel `init`-path and `quickstart`-path tests (see Related Files) must both pass; a fix landed in only one call site produces a failing test on the other, mirroring how TD-006/TD-007 previously escaped detection

**Error Scenario 2: Rollback or never-overwrite guard regresses (pre-existing behavior, must not break)**
- Error message: same fetch/validation error wrapping as today (`"failed to fetch community personas: %w%s"`, `offlineHint`)
- HTTP status / error code: N/A (Go `error`); CLI exits non-zero; rollback of this run's own created files must still occur

## Performance Requirements
- **Response Time:** No new network calls or added latency versus the current single-fetch, single-install flow
- **Throughput:** N/A (single-user CLI invocation)

## Security Considerations
- **Authentication/Authorization:** N/A — unchanged from existing `installCommunityPersonas` behavior
- **Input Validation:** The never-overwrite guard (`fileExists(yamlPath) || fileExists(mdPath)`) must continue to run before any write, for every name in the reconciled roster, preventing the reconciliation change from accidentally clobbering hand-edited or symlinked existing files (per the prior symlink-escape hardening in `internal/personas/unit.go`)

## Test Implementation Guidance
**Test Type:** INTEGRATION (existing `httptest.Server`-backed pattern in `cmd/atcr/init_test.go` and `cmd/atcr/quickstart_test.go`)
**Test Data Requirements:** The real `personas/community/index.json`; a pre-seeded existing persona file (yaml and/or lone md) in the destination dir to exercise the never-overwrite path; a roster large enough to exercise mid-run rollback on a forced failure
**Mock/Stub Requirements:** `httptest.NewServer` for the index fetch; no mocking of the reconciliation logic itself — run it end-to-end via both `init.go`'s and `quickstart.go`'s actual call paths (e.g., via `runInit`/`runQuickstart` or the exported `installCommunityPersonas` entry point each uses) to prove shared behavior rather than assuming it from code inspection

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A test proves `init.go:47` and `quickstart.go:102` resolve to the identical roster/index source (parallel assertions, both passing)
- [ ] A test proves the never-overwrite guard (`init.go:139`) still applies unchanged under the reconciled roster
- [ ] A test proves all-or-nothing rollback still applies unchanged under the reconciled roster (`TestInstallCommunityPersonas_MidRosterFailure_RollsBack`-equivalent, run against the reconciled roster)
- [ ] Built-in `.md` scaffolds written by `atcr init`'s non-community step are unaffected by the community-install step

**Manual Review:**
- [ ] Code reviewed and approved
