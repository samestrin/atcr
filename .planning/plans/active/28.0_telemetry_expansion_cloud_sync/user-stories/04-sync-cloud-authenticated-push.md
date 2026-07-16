# User Story 4: `--sync-cloud` Authenticated Push

**Plan:** [28.0: Telemetry Expansion & Cloud Sync](../plan.md)

## User Story

**As an** engineering manager evaluating `atcr` ROI across a team
**I want** a `--sync-cloud` flag that authenticates with my `ATCR_API_KEY` and pushes the complete anonymized local scorecard (including time/credits saved metrics) to my `atcr.dev/dashboard` account
**So that** I can see aggregated adoption and ROI data across my team's runs without manually collecting and uploading scorecard files myself

## Story Context

- **Background:** `atcr` currently only writes scorecard data locally (`internal/scorecard`) and exposes a public, allowlist-scrubbed leaderboard export path (`cmd/atcr/leaderboard.go:runLeaderboardExport` calling `internal/scorecard/export.go`). No command pushes data to a remote, authenticated destination today. This story adds that push path as a new `--sync-cloud` flag registered via a `cmd/atcr/flags.go:addSyncCloudFlags` helper (following the existing `addRangeFlags` pattern in the same file), fired from the same run-completion points identified for telemetry (`cmd/atcr/review.go:runReview` and `cmd/atcr/reconcile.go:runReconcile`).
- **Assumptions:** Story 1's `internal/telemetry` client (or an HTTP-sending pattern derived from it) is available to reuse for the outbound POST; Story 3's Persona ID hashing path exists so the cloud-sync payload can include a hashed Persona ID rather than a raw identifier. The remote endpoint (`atcr.dev/dashboard`, or a documented/configurable equivalent) accepts a `Bearer`-authenticated POST of a JSON scorecard payload. `ATCR_API_KEY` is the sole authentication mechanism for this story; no interactive login/OAuth flow is in scope.
- **Constraints:** The pushed payload must be built from a **new, separate allowlist schema** in `internal/scorecard/export.go` that follows the same privacy discipline as the existing `PublicRecord`/`scrubField` leaderboard boundary, extended per Story 3 with the hashed Persona ID — it must never include raw source code, file paths, or un-hashed persona/reviewer identifiers, and must not modify or reuse the Epic 10.0 `PublicRecord` allowlist directly. A missing or invalid `ATCR_API_KEY` must terminate the command with a **new, distinct** exit code (e.g. `exitAuth=3` in `cmd/atcr/main.go`, via the existing `codedError`/`errors.As` pattern) rather than the generic `exitUsage=2` code, so scripts/CI can detect this specific failure. Unlike the background telemetry ping (Story 1), `--sync-cloud` is an explicit, user-invoked action — its network call may be synchronous and its failure must be visible (non-silent), but it must still not corrupt or block the underlying `review`/`reconcile` outcome that already succeeded before the sync step runs.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (Anonymous Usage Telemetry Ping) for the HTTP client/send pattern; Story 3 (Persona ID Hashing for the Persona Leaderboard) for the hashed Persona ID field in the payload |

## Success Criteria (SMART Format)

- **Specific:** A `--sync-cloud` flag, registered via `cmd/atcr/flags.go:addSyncCloudFlags`, is available on `review` and `reconcile`; when set, it reads `ATCR_API_KEY`, sends it as an `Authorization: Bearer <key>` header, and POSTs the run's complete anonymized scorecard (including time/credits saved metrics and hashed Persona ID) to the configured `atcr.dev/dashboard` endpoint after the run completes.
- **Measurable:** Unit/integration tests prove (1) a missing `ATCR_API_KEY` with `--sync-cloud` set causes the command to exit with the new dedicated auth exit code rather than `exitUsage`, (2) an invalid/rejected key (simulated 401/403 from a mock endpoint) produces the same dedicated exit code, (3) a successful push sends a `Bearer` header matching `ATCR_API_KEY` and a JSON body containing the expected allowlisted fields (no raw source, file paths, or un-hashed identifiers), and (4) omitting `--sync-cloud` entirely results in zero network calls to the cloud endpoint.
- **Achievable:** Reuses the HTTP client shape and JSON marshaling established in Story 1, and the hashed Persona ID field established in Story 3 — this story is primarily flag wiring, auth handling, and a new exit-code path, not new infrastructure.
- **Relevant:** Directly satisfies epic AC5 ("The `--sync-cloud` flag successfully authenticates and pushes run history to a designated endpoint") and is the commercial/enterprise-facing capability that demonstrates concrete ROI (time/credits saved) to engineering managers evaluating `atcr` at team scale.
- **Time-bound:** Deliverable within this sprint cycle as the fourth story in the plan's execution order, after Story 1 (telemetry client) and Story 3 (persona hashing) are complete, and independently testable via its own auth/exit-code contract.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [04-01](../acceptance-criteria/04-01-sync-cloud-flag-registration.md) | `--sync-cloud` Flag Registration | Unit |
| [04-02](../acceptance-criteria/04-02-successful-authenticated-push.md) | Successful Authenticated Cloud Push | Unit/Integration |
| [04-03](../acceptance-criteria/04-03-missing-api-key-exit-code.md) | Missing `ATCR_API_KEY` Dedicated Exit Code | Unit |
| [04-04](../acceptance-criteria/04-04-invalid-rejected-api-key-exit-code.md) | Invalid/Rejected `ATCR_API_KEY` Dedicated Exit Code | Unit/Integration |

## Original Criteria Overview

1. `--sync-cloud` is registered as a flag on `review` and `reconcile` via a new `addSyncCloudFlags` helper in `cmd/atcr/flags.go`, following the existing `addRangeFlags` PreRunE-chaining convention.
2. When `--sync-cloud` is set, the CLI reads `ATCR_API_KEY`, sends it as a `Bearer` token, and POSTs the complete anonymized scorecard (time/credits saved metrics, hashed Persona ID) to the configured cloud endpoint after the run completes.
3. A missing or invalid `ATCR_API_KEY` when `--sync-cloud` is set causes the command to exit with a new, dedicated exit code (distinct from the generic usage-error code), and this behavior is covered by tests for both the missing-key and invalid/rejected-key cases.

## Technical Considerations

- **Implementation Notes:** Add `addSyncCloudFlags(cmd *cobra.Command)` to `cmd/atcr/flags.go` alongside `addRangeFlags`, registering the `--sync-cloud` bool flag and any supporting flags (e.g. an optional `--cloud-endpoint` override for testing, defaulting to the documented `atcr.dev/dashboard` endpoint). Add a new `exitAuth` constant in `cmd/atcr/main.go` alongside `exitFailure`/`exitUsage`, and a corresponding `codedError` value returned when `ATCR_API_KEY` is unset or the remote endpoint responds with an auth failure (401/403), surfaced via the existing `errors.As` dispatch. The cloud-sync push should call into the scorecard export pipeline (`internal/scorecard/export.go`) to build the payload struct, reusing Story 3's hashed-Persona-ID path rather than the existing `PublicRecord`/`scrubField` leaderboard allowlist directly.
- **Integration Points:** Fires from `cmd/atcr/review.go:runReview` (~line 170) and `cmd/atcr/reconcile.go:runReconcile` (~line 71) after the run's local scorecard record is finalized — same call sites as Story 1's telemetry ping, but as a distinct, explicitly user-invoked action rather than an always-on background ping. Reuses (or shares a small internal helper with) Story 1's `internal/telemetry` HTTP-sending code where the shape overlaps (timeout handling, JSON marshaling), while keeping the payload schema, auth header, and endpoint entirely separate from the anonymous telemetry ping.
- **Data Requirements:** The push payload is the complete anonymized local scorecard record — including time/credits saved metrics already computed for the local scorecard, plus the hashed Persona ID from Story 3 — serialized as JSON via a dedicated allowlist struct (not a superset of the existing public `PublicRecord` leaderboard schema, per the plan's risk mitigation on preserving that boundary). `ATCR_API_KEY` is read via `os.Getenv`, trimmed and validated, following the `cmd/atcr/main.go` `LOG_LEVEL`-read pattern.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A missing/invalid `ATCR_API_KEY` is not clearly distinguishable from other CLI usage errors, breaking scripted/CI detection of auth failures | High | Introduce a new dedicated `exitAuth` exit code (distinct from `exitUsage`) via the existing `codedError`/`errors.As` pattern; add explicit tests for both the missing-key and invalid/rejected-key paths asserting the specific exit code |
| A slow or unreachable `atcr.dev/dashboard` endpoint blocks or delays `review`/`reconcile` completion when `--sync-cloud` is set | Medium | Bound the push with a short request timeout and perform it after the run's primary outcome (exit code, local scorecard write) is already finalized, so a sync failure surfaces as a clear, separate error rather than corrupting or delaying the underlying command result |
| The cloud-sync payload accidentally reuses or extends the existing public `PublicRecord`/`scrubField` leaderboard allowlist, weakening its documented Epic 10.0 privacy guarantee | Medium | Define the cloud-sync payload as its own explicitly-named allowlist struct built from Story 3's hashed-Persona-ID path, not a superset of `PublicRecord`; add a regression test asserting the leaderboard `--export` path's schema and scrubbing behavior is unchanged |

---

**Created:** July 15, 2026
**Status:** Draft - Acceptance Criteria Generated
