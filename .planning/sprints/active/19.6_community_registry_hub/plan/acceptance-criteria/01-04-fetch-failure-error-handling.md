# Acceptance Criteria: Fetch Failure Produces a Descriptive, Non-Zero-Exit Error

**Related User Story:** [01: Community-Canonical Fetch-and-Pin Distribution](../user-stories/01-community-canonical-fetch-and-pin-distribution.md)
**Design References:** [fetch-and-distribution.md](../documentation/fetch-and-distribution.md), [testing-mock-registry.md](../documentation/testing-mock-registry.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go error handling / CLI exit code | `cmd/atcr/init.go`, `cmd/atcr/quickstart.go` |
| Test Framework | Go `testing` + `httptest.NewServer` | server returns 5xx/connection-refused/timeout to simulate failure |
| Key Dependencies | existing `fetch()` error wrapping in `internal/personas/client.go` | no new error type required, only surfacing at the CLI boundary |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/init.go` (`runInit`) — modify: propagate any `internal/personas.Install`/`FetchIndex` error as a non-nil return from `runInit`, with no silent fallback to embedded built-ins; wrap the error with `--offline` guidance.
- `cmd/atcr/quickstart.go` (`runQuickstart`) — modify: propagate the same `runInit` error, aborting the wizard before synthetic-provider setup.
- `internal/personas/client.go` (`fetch`, `fetchTimeout` 30s, `FetchIndex`) — reference: existing fetch error wrapping and timeout behavior.
- `internal/personas/install.go` — reference: validation path that may fail after a successful fetch.
- `cmd/atcr/init_test.go` / `cmd/atcr/quickstart_test.go` — modify: add tests pointing `ATCR_PERSONAS_URL` at a failing `httptest.NewServer` (HTTP 500 / connection refused / timeout), asserting non-zero exit and no partial files left on disk.
- `docs/personas-install.md` — modify: document fetch-failure behavior and the `--offline` fallback.


## Happy Path Scenarios
**Scenario 1: Fetch failure aborts `atcr init` with a descriptive error**
- **Given** `ATCR_PERSONAS_URL` points at a server returning HTTP 500 for `index.json`
- **When** `atcr init` runs without `--offline`
- **Then** the command returns a non-nil error mentioning the fetch failure and suggesting `--offline` as a fallback, and the process exits non-zero

**Scenario 2: Fetch failure aborts `atcr quickstart` before provider setup**
- **Given** the same failing mock server as Scenario 1
- **When** `atcr quickstart` runs without `--offline`
- **Then** `runQuickstart` returns the same descriptive error from its `runInit` call, and the synthetic-provider/key-env/workflow steps never execute

## Edge Cases
**Edge Case 1: Partial fetch failure (some personas succeed, one fails mid-roster)**
- **Given** a mock registry that serves the first two roster personas successfully and then fails (500) for the third
- **When** `atcr init` runs without `--offline`
- **Then** the roster install is all-or-nothing: the command exits non-zero with a descriptive error identifying the failing persona name, and NO partial files are left on disk — any personas written before the failure are rolled back so the workspace is byte-for-byte identical to its pre-run state. This atomic/rolled-back behavior is the single decisive contract (not a reported partial install) and is covered by a test asserting zero new persona files remain after the mid-roster failure.

**Edge Case 2: Network timeout rather than an HTTP error status**
- **Given** a mock server that never responds (simulating a hang past `fetchTimeout`)
- **When** `atcr init` runs without `--offline`
- **Then** the existing `fetchTimeout` (30s context deadline in `fetch()`) still applies, and the resulting context-deadline error is surfaced through `runInit` the same descriptive way as an HTTP error

## Error Conditions
**Error Scenario 1: Registry index unreachable (connection refused)**
- Error message: includes underlying failure detail plus explicit guidance, e.g. `"failed to fetch community personas: <dial error> — retry, or run with --offline to use the embedded built-in personas"`
- HTTP status / error code: N/A (connection-level failure, not an HTTP status); process exit code non-zero

**Error Scenario 2: Registry index returns non-2xx status**
- Error message: `"failed to fetch community repo index: unexpected status 500"` (existing `fetch()` wrapping) surfaced verbatim or further wrapped with the `--offline` suggestion by the `init`/`quickstart` caller
- HTTP status / error code: 500 (or any non-2xx, non-404 status per existing `fetch()` switch); process exit code non-zero

## Performance Requirements
- **Response Time:** Failure is detected and reported within `fetchTimeout` (30s) — no indefinite hang.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A.
- **Input Validation:** Error messages must not leak sensitive local filesystem paths beyond what is already surfaced by existing error wrapping (e.g. `.atcr/personas/<name>.yaml`), which is acceptable since it is user-local, non-secret information.

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** `httptest.NewServer` configured to return 500 for one or more endpoints, or a server that is closed before the request completes (connection-refused simulation).
**Mock/Stub Requirements:** `ATCR_PERSONAS_URL` override or `personasClient` swap; no live network in CI.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Fetch failure (without `--offline`) never silently falls back to embedded built-ins
- [ ] `runInit`/`runQuickstart` return a non-nil, descriptive error naming the failure and suggesting `--offline`
- [ ] Process exits non-zero on fetch failure
- [ ] Timeout and non-2xx-status failures are both covered by test
- [ ] Mid-roster fetch failure is all-or-nothing: no partial persona files remain on disk (roster install rolled back to pre-run state), covered by test

**Manual Review:**
- [ ] Code reviewed and approved
