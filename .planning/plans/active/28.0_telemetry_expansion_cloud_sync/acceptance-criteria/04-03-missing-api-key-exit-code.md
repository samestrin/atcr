# Acceptance Criteria: Missing `ATCR_API_KEY` Dedicated Exit Code

**Related User Story:** [04: `--sync-cloud` Authenticated Push](../user-stories/04-sync-cloud-authenticated-push.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`cmd/atcr`) | New `exitAuth` constant + `codedError` construction, wired through the existing `errors.As` dispatch |
| Test Framework | `go test` (standard `testing`) | Test files: `cmd/atcr/main_test.go`, `cmd/atcr/review_test.go`, `cmd/atcr/reconcile_test.go` |
| Key Dependencies | Go stdlib `errors`, `os` | Matches the existing `codedError`/`usageError` pattern in `cmd/atcr/main.go` |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/main.go` - modify: add `exitAuth = 3` alongside the existing `exitFailure = 1` and `exitUsage = 2` constants (`cmd/atcr/main.go:84-85`), and add an `authError(err error) *codedError` (or equivalent) constructor mirroring `usageError` (`cmd/atcr/main.go:100`) but tagged with `exitAuth`; ensure the existing `errors.As` dispatch (`cmd/atcr/main.go:109`) resolves `exitAuth` correctly for this new coded-error case.
- `cmd/atcr/review.go` - modify: when `--sync-cloud` is set and `ATCR_API_KEY` is unset/empty after `strings.TrimSpace`, return an `authError(...)` before attempting the push (`cmd/atcr/review.go:170`).
- `cmd/atcr/reconcile.go` - modify: apply the identical missing-key check before pushing (`cmd/atcr/reconcile.go:71`).
- `cmd/atcr/main_test.go` - create or modify: unit test asserting `exitAuth` resolves to `3` via the `errors.As` dispatch path.

## Happy Path Scenarios
**Scenario 1: N/A for this AC — see Error Conditions**
- This AC is entirely about the missing-key failure path; there is no "happy path" for a missing key. The corresponding happy path (valid key, successful push) is covered in AC 04-02.

## Edge Cases
**Edge Case 1: `ATCR_API_KEY` set to whitespace-only value**
- **Given** `ATCR_API_KEY="   "` (whitespace only) and `--sync-cloud` is set
- **When** the command runs
- **Then** the key is treated as unset after `strings.TrimSpace` (matching the `LOG_LEVEL`-read pattern at `cmd/atcr/main.go:217`), and the command exits with `exitAuth`

**Edge Case 2: `ATCR_API_KEY` unset but `--sync-cloud` also unset**
- **Given** `ATCR_API_KEY` is unset and `--sync-cloud` is NOT passed
- **When** the command runs
- **Then** no auth check occurs and the command exits normally based on the underlying `review`/`reconcile` outcome (exit code `0` or `1`, never `exitAuth`)

## Error Conditions
**Error Scenario 1: `--sync-cloud` set, `ATCR_API_KEY` unset**
- **Given** `atcr review --sync-cloud` (or `reconcile --sync-cloud`) is invoked with `ATCR_API_KEY` unset in the environment
- **When** the command reaches the cloud-sync auth check
- **Then** the command returns an `authError` and the process exits with the dedicated `exitAuth` code (`3`), distinct from `exitUsage` (`2`) and `exitFailure` (`1`)
- Error message: "ATCR_API_KEY is not set; --sync-cloud requires a valid API key"
- HTTP status / error code: process exit code `3` (`exitAuth`); no HTTP request is attempted (fail fast, before any network call)

**Error Scenario 2: `--sync-cloud` set, `ATCR_API_KEY` empty string**
- **Given** `ATCR_API_KEY=""` is explicitly set to an empty string and `--sync-cloud` is passed
- **When** the command reaches the cloud-sync auth check
- **Then** the same `exitAuth` (`3`) path is taken as the fully-unset case
- Error message: "ATCR_API_KEY is not set; --sync-cloud requires a valid API key"
- HTTP status / error code: process exit code `3` (`exitAuth`)

## Performance Requirements
- **Response Time:** The missing-key check is a synchronous, in-process string check performed before any network call — effectively instantaneous, no timeout applies.
- **Throughput:** N/A — single check per invocation.

## Security Considerations
- **Authentication/Authorization:** This AC is the primary auth-gating check; a missing key must never fall through to an attempted push (fail closed, not fail open).
- **Input Validation:** `ATCR_API_KEY` is read via `os.Getenv` and trimmed; no key value is logged or echoed in the error message (only the fact that it is missing is reported).

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Environment variable states: unset, empty string, whitespace-only, combined with `--sync-cloud` set/unset.
**Mock/Stub Requirements:** No HTTP mock needed for this AC — the test asserts the command exits `exitAuth` (`3`) WITHOUT any network call occurring (assert via a test server that must receive zero requests, reusing the pattern from AC 04-02's Edge Case 1).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `exitAuth = 3` is defined in `cmd/atcr/main.go` alongside `exitFailure`/`exitUsage`
- [ ] A missing `ATCR_API_KEY` with `--sync-cloud` set exits `exitAuth`, not `exitUsage`
- [ ] An empty or whitespace-only `ATCR_API_KEY` is treated identically to an unset key
- [ ] No network call is attempted when the key is missing (fail fast)

**Manual Review:**
- [ ] Code reviewed and approved
