# Acceptance Criteria: `--offline` Flag Preserves Embedded-Built-In Behavior

**Related User Story:** [01: Community-Canonical Fetch-and-Pin Distribution](../user-stories/01-community-canonical-fetch-and-pin-distribution.md)
**Design References:** [fetch-and-distribution.md](../documentation/fetch-and-distribution.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cobra flag + branch logic | `cmd/atcr/init.go`, `cmd/atcr/quickstart.go` |
| Test Framework | Go `testing` | assert zero network calls when `--offline` is set |
| Key Dependencies | `personas.Names()`/`personas.Get()` (embedded builtins) | existing today's-behavior code path, retained as the offline branch |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/init.go` — modify: add `--offline` flag to `newInitCmd`, thread `offline bool` into `runInit`, and branch to the existing embedded built-in copy loop when `offline == true`.
- `cmd/atcr/quickstart.go` — modify: add `--offline` flag to `newQuickstartCmd`, add `offline` to `quickstartOpts`, and pass it through to `runInit`.
- `cmd/atcr/personas.go` (line 81 `personasClient`) — reference: existing package-level HTTP client var that tests can stub to prove zero network calls.
- `internal/personas/client.go` (line 24 `RegistryBaseURL`, `BaseURL()`) — reference: default fetch URL, not used in offline branch.
- `personas/personas.go` (names slice ~line 20, embedded file guard) — reference: source of embedded built-in personas installed by the offline branch.
- `cmd/atcr/init_test.go` / `cmd/atcr/quickstart_test.go` — modify: add zero-network-call assertions for `atcr init --offline` and `atcr quickstart --offline`.
- `docs/personas-install.md` — modify: document the `--offline` flag and when to use it.


## Happy Path Scenarios
**Scenario 1: `atcr init --offline` installs embedded built-ins with no network access**
- **Given** an empty workspace and a `personasClient`/`HTTPClient` stub configured to fail the test on any `Do` call
- **When** `atcr init --offline` runs
- **Then** the command succeeds, installs the embedded built-in personas exactly as today's pre-story behavior, and the stub is never invoked

**Scenario 2: `atcr quickstart --offline` installs embedded built-ins with no network access**
- **Given** the same no-network stub as Scenario 1
- **When** `atcr quickstart --offline` runs
- **Then** the `.atcr/config.yaml` and persona scaffolding step completes using embedded built-ins, and the stub is never invoked for the persona-install phase (the synthetic-provider setup that follows is unaffected by this flag)

**Scenario 3: Empty workspace offline path succeeds with no network access**
- **Given** an empty workspace and a `personasClient`/`HTTPClient` stub configured to fail the test on any `Do` call
- **When** `atcr init --offline` runs
- **Then** the command exits 0, installs the embedded built-in personas, and writes `.atcr/config.yaml` and `.atcr/.gitignore` without any network call

**Scenario 4: Offline path never falls back to a network fetch on validation failure**
- **Given** a workspace and a `personasClient` stub that fails on any `Do` call
- **When** `atcr init --offline` encounters an embedded persona that cannot be read (deterministically simulated by injecting a read error through the embedded-persona accessor — e.g. a test seam over `personas.Get()`/`fs.ReadFile` that returns an error for one name)
- **Then** the command returns a non-zero error and does not silently invoke the fetch path to compensate; a test asserts both the non-nil error and zero `Do` calls on the network stub

## Edge Cases
**Edge Case 1: `--offline` combined with `--force`**
- **Given** an existing workspace with previously-installed and/or hand-edited persona files
- **When** `atcr init --offline --force` runs
- **Then** any user-modified persona file is preserved byte-for-byte and is NEVER overwritten — the same preservation guarantee as the online path (AC 01-05) — while only missing personas are installed from the embedded built-ins, and no network access is attempted. `--force` does not license overwriting a user-modified persona in either offline or online mode; the guarantee is uniform.

**Edge Case 2: `--offline` flag omitted (default)**
- **Given** a workspace and a reachable mock registry
- **When** `atcr init` runs with no `--offline` flag
- **Then** the default resolves to `offline == false` and the fetch-and-pin path (AC 01-02) is used, confirming the flag's zero-value default matches the new canonical behavior, not the old one

## Error Conditions
**Error Scenario 1: `--offline` used with an invalid flag combination (if one is introduced)**
- Error message: N/A for this story — no invalid combination is defined; `--offline` is a simple independent boolean with no conflicting flags
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** `atcr init --offline` / `atcr quickstart --offline` complete with no network round-trip latency at all (embedded-file copy only), strictly faster by avoiding all network round-trips than the non-offline path.
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
- [ ] `--offline --force` preserves user-modified persona files byte-for-byte (never overwrites) — the same guarantee as the online path in AC 01-05

**Manual Review:**
- [ ] Code reviewed and approved
