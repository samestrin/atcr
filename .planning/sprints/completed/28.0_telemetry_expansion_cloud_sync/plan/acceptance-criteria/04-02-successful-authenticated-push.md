# Acceptance Criteria: Successful Authenticated Cloud Push

**Related User Story:** [04: `--sync-cloud` Authenticated Push](../user-stories/04-sync-cloud-authenticated-push.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/scorecard`, `cmd/atcr`) | New cloud-sync payload struct + POST call, reusing Story 1's HTTP-sending shape |
| Test Framework | `go test` (standard `testing`, `net/http/httptest`) | Test files: `internal/scorecard/cloudsync_test.go`, `cmd/atcr/review_test.go`, `cmd/atcr/reconcile_test.go` |
| Key Dependencies | Go stdlib `net/http`, `encoding/json`, `context`, `os` | No new third-party dependency |

### Related Files (from codebase-discovery.json)
- `internal/scorecard/cloudsync.go` - create: defines a dedicated allowlist struct (e.g. `CloudSyncRecord`) distinct from `PublicRecord` (`internal/scorecard/export.go:35`), including time/credits saved metrics and the hashed Persona ID (Story 3), plus a `Push(ctx context.Context, endpoint, apiKey string, rec CloudSyncRecord) error` function that POSTs the JSON payload with an `Authorization: Bearer <apiKey>` header.
- `cmd/atcr/review.go` - modify: after the run completes (`runReview`, `cmd/atcr/review.go:170`), when `--sync-cloud` is set, build a `CloudSyncRecord` from the finalized local scorecard record and call `scorecard.Push`.
- `cmd/atcr/reconcile.go` - modify: after `scorecard.EmitForReconcile` (`cmd/atcr/reconcile.go:148`), when `--sync-cloud` is set, build and push the equivalent `CloudSyncRecord`.
- `internal/scorecard/cloudsync_test.go` - create: unit tests asserting the JSON body's allowlisted fields and the `Authorization: Bearer` header.

## Happy Path Scenarios
**Scenario 1: Successful push on `review` completion**
- **Given** `atcr review --sync-cloud` runs to completion with `ATCR_API_KEY=valid-key` set and a reachable (mock) cloud endpoint
- **When** `runReview` reaches its completion point
- **Then** `scorecard.Push` is invoked with an `Authorization: Bearer valid-key` header and a JSON body containing the allowlisted fields (time saved, credits saved, hashed Persona ID, model, run outcome) — the request succeeds and the command exits `0`

**Scenario 2: Successful push on `reconcile` completion**
- **Given** `atcr reconcile --sync-cloud` runs to completion with a valid `ATCR_API_KEY` and reachable endpoint
- **When** `runReconcile` reaches its completion point
- **Then** the equivalent `CloudSyncRecord` payload is POSTed with the `Bearer` header, and the command exits `0`

**Scenario 3: `--cloud-endpoint` override is honored**
- **Given** `--sync-cloud --cloud-endpoint=https://mock.test/ingest` is passed
- **When** the push fires
- **Then** the POST is sent to `https://mock.test/ingest` rather than the default `atcr.dev/dashboard` endpoint

## Edge Cases
**Edge Case 1: `--sync-cloud` omitted entirely**
- **Given** neither `review` nor `reconcile` is passed `--sync-cloud`
- **When** the command completes
- **Then** zero network calls are made to any cloud endpoint (verified via a test HTTP server asserting no requests were received)

**Edge Case 2: Local run failed but scorecard was still finalized**
- **Given** the underlying `review`/`reconcile` run itself fails (non-zero local outcome) but its local scorecard record is still written
- **When** `--sync-cloud` is set
- **Then** the push still fires with the failure outcome captured in the payload, and a push failure does not further alter the already-determined command exit code (except for the dedicated auth-failure path in AC 04-03/04-04)

**Edge Case 3: Payload never includes raw source, file paths, or un-hashed identifiers**
- **Given** a completed run whose local scorecard contains file paths and raw identifiers
- **When** the `CloudSyncRecord` is built from the local record
- **Then** the resulting JSON contains only allowlisted fields — no `path`, `source`, `file`, or raw persona/reviewer identifier keys are present (a regression test enumerates the struct's fields and fails if any disallowed key is added)

## Error Conditions
**Error Scenario 1: Unreachable/slow endpoint**
- **Given** the configured cloud endpoint does not respond within the request timeout
- **When** the push is attempted
- **Then** the push fails with a bounded timeout (not an indefinite hang), the failure is surfaced to the user as a non-silent, clearly-labeled cloud-sync error, and the underlying `review`/`reconcile` exit code (already finalized before the push) is not corrupted
- Error message: "cloud sync failed: request to <endpoint> timed out"
- HTTP status / error code: no HTTP status (client-side timeout); process exit code is unaffected by this failure path (distinct from the auth failure in AC 04-03/04-04)

**Error Scenario 2: Endpoint returns a non-auth 5xx error**
- **Given** the cloud endpoint responds with `500 Internal Server Error`
- **When** the push completes
- **Then** the failure is reported to the user as a non-silent cloud-sync error, distinct from the `exitAuth` path, and does not change the command's underlying exit code
- Error message: "cloud sync failed: server returned 500"
- HTTP status / error code: 500 (mapped to a logged/reported error, not `exitAuth`)

## Performance Requirements
- **Response Time:** The push request is bounded by a short, explicit timeout (e.g. 5s) so a hung endpoint cannot indefinitely block `review`/`reconcile` completion, per the story's constraint that the push occurs synchronously but must not corrupt or block the already-finalized run outcome.
- **Throughput:** Single push per command invocation; no batching required.

## Security Considerations
- **Authentication/Authorization:** `ATCR_API_KEY` is sent exclusively as an `Authorization: Bearer <key>` header, never as a query parameter or in the request body, and never logged in plaintext.
- **Input Validation:** The `CloudSyncRecord` is built from a fixed allowlist struct (not a superset of `PublicRecord`); the endpoint URL from `--cloud-endpoint` is validated as `https://` before use (or `http://` only for local test servers under test-only conditions), rejecting non-URL strings without panicking.

## Test Implementation Guidance
**Test Type:** UNIT / INTEGRATION
**Test Data Requirements:** Sample finalized local scorecard records including time/credits-saved metrics and a hashed Persona ID (per Story 3), covering both success and failure run outcomes.
**Mock/Stub Requirements:** `net/http/httptest.NewServer` standing in for the `atcr.dev/dashboard` endpoint (via `--cloud-endpoint` override), asserting request method, `Authorization` header, and JSON body shape; no real network calls in tests.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `scorecard.Push` POSTs a `CloudSyncRecord` JSON body with an `Authorization: Bearer <ATCR_API_KEY>` header to the configured endpoint
- [ ] `runReview` and `runReconcile` both invoke the push only when `--sync-cloud` is set
- [ ] Omitting `--sync-cloud` results in zero network calls to the cloud endpoint
- [ ] The pushed payload excludes raw source, file paths, and un-hashed identifiers (allowlist-only)

**Manual Review:**
- [ ] Code reviewed and approved
