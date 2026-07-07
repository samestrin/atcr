# Acceptance Criteria: `--offline` Flag Preserves Embedded-Built-In Behavior

**Related User Story:** [01: Community-Canonical Fetch-and-Pin Distribution](../user-stories/01-community-canonical-fetch-and-pin-distribution.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cobra flag + branch logic | `cmd/atcr/init.go`, `cmd/atcr/quickstart.go` |
| Test Framework | Go `testing` | assert zero network calls when `--offline` is set |
| Key Dependencies | `personas.Names()`/`personas.Get()` (embedded builtins) | existing today's-behavior code path, retained as the offline branch |

## Related Files
- `cmd/atcr/init.go` - modify: add `cmd.Flags().Bool("offline", false, ...)` to `newInitCmd`, thread an `offline bool` parameter into `runInit`, and branch: `offline == true` uses the existing `builtins.Names()`/`builtins.Get()` copy loop; `offline == false` (default) uses the new fetch-and-pin path from AC 01-02.
- `cmd/atcr/quickstart.go` - modify: add `cmd.Flags().Bool("offline", false, ...)` to `newQuickstartCmd`, add an `offline bool` field to `quickstartOpts`, and pass it through to the `runInit` call inside `runQuickstart`.
- `cmd/atcr/init_test.go` - modify: add a test asserting `atcr init --offline` installs the embedded built-ins with an `HTTPClient`/`personasClient` stub that fails the test if `Do` is ever invoked (proves zero network calls).
- `cmd/atcr/quickstart_test.go` - modify: mirror the same zero-network-call assertion for `atcr quickstart --offline`.

## Happy Path Scenarios
**Scenario 1: `atcr init --offline` installs embedded built-ins with no network access**
- **Given** an empty workspace and a `personasClient`/`HTTPClient` stub configured to fail the test on any `Do` call
- **When** `atcr init --offline` runs
- **Then** the command succeeds, installs the embedded built-in personas exactly as today's pre-story behavior, and the stub is never invoked

**Scenario 2: `atcr quickstart --offline` installs embedded built-ins with no network access**
- **Given** the same no-network stub as Scenario 1
- **When** `atcr quickstart --offline` runs
- **Then** the `.atcr/config.yaml` and persona scaffolding step completes using embedded built-ins, and the stub is never invoked for the persona-install phase (the synthetic-provider setup that follows is unaffected by this flag)

## Edge Cases
**Edge Case 1: `--offline` combined with `--force`**
- **Given** an existing workspace with previously-installed personas
- **When** `atcr init --offline --force` runs
- **Then** existing files are overwritten with the embedded built-ins (matching the pre-story `--force` contract), with no network access attempted

**Edge Case 2: `--offline` flag omitted (default)**
- **Given** a workspace and a reachable mock registry
- **When** `atcr init` runs with no `--offline` flag
- **Then** the default resolves to `offline == false` and the fetch-and-pin path (AC 01-02) is used, confirming the flag's zero-value default matches the new canonical behavior, not the old one

## Error Conditions
**Error Scenario 1: `--offline` used with an invalid flag combination (if one is introduced)**
- Error message: N/A for this story — no invalid combination is defined; `--offline` is a simple independent boolean with no conflicting flags
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** `atcr init --offline` / `atcr quickstart --offline` complete with no network round-trip latency at all (embedded-file copy only), strictly faster than the non-offline path.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — offline path touches no network.
- **Input Validation:** No new inputs; the offline branch reuses the existing embedded-builtin copy loop verbatim, so no new validation surface is introduced.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** None beyond existing embedded builtin fixtures.
**Mock/Stub Requirements:** An `HTTPClient` stub (or `personasClient` swap) whose `Do` method calls `t.Fatal`/`t.Error` if invoked, proving the offline path makes zero fetch calls.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `--offline` flag added to both `atcr init` and `atcr quickstart`
- [ ] `--offline` reproduces the exact pre-story embedded-built-in install behavior
- [ ] Test proves zero network calls occur when `--offline` is set
- [ ] Default (`--offline` absent) resolves to the new fetch-and-pin path, not the old embedded-copy path

**Manual Review:**
- [ ] Code reviewed and approved
