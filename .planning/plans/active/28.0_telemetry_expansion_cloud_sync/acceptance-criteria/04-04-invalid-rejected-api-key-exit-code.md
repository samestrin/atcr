# Acceptance Criteria: Invalid/Rejected `ATCR_API_KEY` Dedicated Exit Code

**Related User Story:** [04: `--sync-cloud` Authenticated Push](../user-stories/04-sync-cloud-authenticated-push.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`cmd/atcr`, `internal/scorecard`) | Maps a remote `401`/`403` response to the same `exitAuth` coded error used for the missing-key case (AC 04-03) |
| Test Framework | `go test` (standard `testing`, `net/http/httptest`) | Test files: `cmd/atcr/review_test.go`, `cmd/atcr/reconcile_test.go`, `internal/scorecard/cloudsync_test.go` |
| Key Dependencies | Go stdlib `net/http`, `errors` | Reuses `exitAuth`/`authError` from AC 04-03 |

## Related Files
- `internal/scorecard/cloudsync.go` - modify: `Push` inspects the HTTP response status; a `401 Unauthorized` or `403 Forbidden` response is returned as a distinguishable sentinel error (e.g. `ErrCloudAuthRejected`) rather than a generic push-failure error.
- `cmd/atcr/main.go` - modify: extend the `errors.As`/`authError` dispatch (`cmd/atcr/main.go:109` area) so `scorecard.ErrCloudAuthRejected` (wrapped via `errors.Is`/`errors.As`) is mapped to `exitAuth`, the same dedicated code used for the missing-key case in AC 04-03.
- `cmd/atcr/review.go` - modify: propagate `scorecard.ErrCloudAuthRejected` from the push call up through `runReview`'s return path as an `authError`.
- `cmd/atcr/reconcile.go` - modify: apply the identical propagation for `runReconcile`.
- `internal/scorecard/cloudsync_test.go` - create or modify: unit test simulating a `401`/`403` mock endpoint response and asserting `ErrCloudAuthRejected` is returned.

## Happy Path Scenarios
**Scenario 1: N/A for this AC — see Error Conditions**
- This AC is entirely about the invalid/rejected-key failure path; the corresponding happy path (valid key, accepted push) is covered in AC 04-02.

## Edge Cases
**Edge Case 1: Endpoint returns `401` vs `403`**
- **Given** the mock cloud endpoint returns either `401 Unauthorized` or `403 Forbidden`
- **When** `Push` processes the response
- **Then** both statuses map identically to `ErrCloudAuthRejected` and therefore to `exitAuth` — the CLI does not need to distinguish "bad key" from "key lacks permission" for this story's scope

**Edge Case 2: Auth rejection occurs after a successful local run**
- **Given** `review`/`reconcile` already completed successfully and its local scorecard record was written before the cloud push fires
- **When** the endpoint rejects the key with `401`/`403`
- **Then** the local scorecard record remains intact and unaffected — only the process exit code changes to `exitAuth` (the underlying local run outcome is not corrupted, per the story's non-corruption constraint)

## Error Conditions
**Error Scenario 1: Endpoint returns `401 Unauthorized`**
- **Given** `atcr review --sync-cloud` (or `reconcile --sync-cloud`) is invoked with `ATCR_API_KEY` set to a syntactically valid but remotely-rejected value, against a mock endpoint configured to return `401`
- **When** the push request completes
- **Then** the command returns an `authError` wrapping `scorecard.ErrCloudAuthRejected`, and the process exits with the dedicated `exitAuth` code (`3`) — the same code as the missing-key case in AC 04-03
- Error message: "cloud sync rejected: ATCR_API_KEY was not accepted by the server (401)"
- HTTP status / error code: HTTP `401` from the mock endpoint; process exit code `3` (`exitAuth`)

**Error Scenario 2: Endpoint returns `403 Forbidden`**
- **Given** the same setup as Error Scenario 1, but the mock endpoint returns `403`
- **When** the push request completes
- **Then** the command exits with `exitAuth` (`3`), same as the `401` case
- Error message: "cloud sync rejected: ATCR_API_KEY was not accepted by the server (403)"
- HTTP status / error code: HTTP `403` from the mock endpoint; process exit code `3` (`exitAuth`)

**Error Scenario 3: `401`/`403` is distinguished from other 4xx/5xx errors**
- **Given** the mock endpoint returns `400 Bad Request` or `500 Internal Server Error` (a non-auth failure)
- **When** the push request completes
- **Then** the command does NOT exit `exitAuth` — it follows AC 04-02's generic push-failure error path instead, preserving `exitAuth` as specific to authentication rejection only

## Performance Requirements
- **Response Time:** The auth-rejection check is a synchronous inspection of the HTTP response status code within the same bounded timeout window established in AC 04-02 (e.g. 5s); no additional latency beyond the single push request.
- **Throughput:** N/A — single push attempt per invocation; no retry-on-401 loop (a rejected key does not warrant automatic retry).

## Security Considerations
- **Authentication/Authorization:** A rejected key must never be retried automatically or cached as valid; each invocation re-reads `ATCR_API_KEY` fresh from the environment.
- **Input Validation:** The response body (if any) from a `401`/`403` is not echoed verbatim into the CLI error message (avoids leaking server-side error internals); only the status code and a fixed, generic message are surfaced.

## Test Implementation Guidance
**Test Type:** UNIT / INTEGRATION
**Test Data Requirements:** Mock endpoint configured to return `401`, `403`, `400`, and `500` across separate test cases to verify only `401`/`403` map to `exitAuth`.
**Mock/Stub Requirements:** `net/http/httptest.NewServer` returning the configured status code per test case; assert the resulting process/command exit code via the `errors.As` dispatch path (or the command's returned error mapped through `main.go`'s exit-code resolution).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A `401` response from the cloud endpoint maps to `exitAuth` (`3`)
- [ ] A `403` response from the cloud endpoint maps to `exitAuth` (`3`)
- [ ] Non-auth failures (`400`, `500`, timeouts) do NOT map to `exitAuth` — they follow AC 04-02's generic error path
- [ ] The local scorecard record is unaffected by a cloud auth rejection

**Manual Review:**
- [ ] Code reviewed and approved
