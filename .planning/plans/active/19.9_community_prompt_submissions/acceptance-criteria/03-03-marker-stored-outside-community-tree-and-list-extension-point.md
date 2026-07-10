# Acceptance Criteria: Marker Lives Outside `personas/community/` and `List` Extension Point Leaves Existing Output Unchanged

**Related User Story:** [03: `submitted` Status Distinct from `Source`/Provenance](../user-stories/03-submitted-status-distinct-from-source.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Path constant/convention + read-only extension function | A status-tracking path sits alongside (not inside) the vetted `personas/community/` tree |
| Test Framework | Go `testing` package | Golden-output comparison for `personas list` command and direct `List`/`ListTiers` assertions; testify is not used in this codebase |
| Key Dependencies | `internal/personas/list.go` (`List`, `ListTiers`) — read-only integration, no signature change | |

### Related Files (from codebase-discovery.json)
- `internal/personas/list.go` (modify) — add an additive, separately-named extension point (e.g., a new exported function such as `ListSubmissions` or a method on the new marker type) that can surface `submitted` status without changing `List`/`ListTiers`'s existing return type or values
- `internal/personas/list_test.go` (modify) — add a test asserting `personas list` (i.e., `List`/`ListTiers`) output and row count/order are identical whether or not `submitted` markers exist on disk
- `<marker storage path, e.g. internal/personas/submissions/ or .atcr/submissions/>` (create) — path constant/location definition, distinct from and outside `personas/community/`

## Design References
- [Status/Provenance Separation and Atomic Persistence](../documentation/status-provenance-and-atomic-writes.md) — why the marker must live outside `personas/community/` and why `List`/`ListTiers` output must remain unchanged

## Happy Path Scenarios
**Scenario 1: Marker storage path never resolves under `personas/community/`**
- **Given** the marker storage path constant used by the submit flow
- **When** a marker is written for any persona name (including namespaced names with `/` segments)
- **Then** the resolved absolute path never has `personas/community/` (or the configured community dir) as a prefix — verified by a test comparing `filepath.Clean` of both paths

**Scenario 2: `List` output is byte-for-byte identical with and without submitted markers present**
- **Given** a fixed set of built-in/community/project personas, once with no `submitted` markers and once with markers present for a subset of them
- **When** `List` and `ListTiers` are called in both scenarios
- **Then** the returned `[]PersonaMeta` slices are equal (same names, versions, sources, order) in both cases — the extension point that surfaces `submitted` status is a separate call, not a mutation of `List`'s existing return

## Edge Cases
**Edge Case 1: Extension point is called for a persona with no submission marker**
- **Given** a persona that has never been submitted
- **When** the new `submitted`-status extension point is queried for it
- **Then** it returns a clear "not submitted" / zero-value result rather than an error, and `List`'s own output for that persona is unaffected

**Edge Case 2: Namespaced/nested persona name submitted marker**
- **Given** a project persona with a nested name (e.g., `team/reviewer`, per `listProject`'s slash-path handling at `internal/personas/list.go:112`)
- **When** a marker is written for it
- **Then** the marker path construction produces an on-disk location that stays within the configured marker storage directory (verified by `filepath.Clean` and a `filepath.Rel` check against the storage root), remains outside `personas/community/`, and rejects `..`/absolute-path segments without colliding with or escaping into the persona's own namespaced directory structure

## Error Conditions
**Error Scenario 1: Extension point queried against a personas dir that cannot be walked**
- Error message: consistent with existing `List`/`listCommunity` wrapped errors, e.g. `"could not read personas directory %s: %w"` pattern already used at `internal/personas/list.go:206`
- HTTP status / error code: N/A (CLI, non-zero exit / returned Go error)

**Error Scenario 2: A future accidental write targets `personas/community/` directly**
- Error message: caught by a dedicated unit test asserting the marker path constant's prefix, failing the build/test suite rather than surfacing at runtime — this is a design-time guarantee, not a user-facing runtime error
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** No regression to existing `personas list` command latency — the extension point must be an opt-in call (invoked by future `submitted`/status-aware commands only), not something `List`/`ListTiers` invoke internally by default
- **Throughput:** N/A (local CLI, single-user invocation)

## Security Considerations
- **Authentication/Authorization:** N/A for this AC's read-only listing scope
- **Input Validation:** Marker path derivation from a persona name must reject or safely encode path-traversal characters (`..`, absolute paths) the same way `listProject`/`listCommunity` already guard via `filepath.Rel`/`filepath.WalkDir` symlink skipping, so a maliciously named submission cannot escape the marker storage directory

## Test Implementation Guidance
**Test Type:** UNIT (with one INTEGRATION-flavored test exercising the full `personas list` CLI output path)
**Test Data Requirements:** Fixture personas dir with built-in + community + project rows; a parallel fixture with `submitted` markers written for a subset of those personas at the designated storage path
**Mock/Stub Requirements:** None — filesystem-only; no network or external service mocking needed

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Marker storage path constant is defined outside `personas/community/` and covered by a path-prefix test
- [ ] `List`/`ListTiers` output is identical (values, order, count) with and without `submitted` markers present, verified by test
- [ ] A new, separately named extension point exists to surface `submitted` status without altering `List`/`ListTiers` signatures or behavior
- [ ] Existing `personas list` command tests pass unmodified

**Manual Review:**
- [ ] Code reviewed and approved
