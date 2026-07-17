# User Story 6: Gated Quality-Signal Transmission via the Epic 28.0 Transport

**Plan:** [30.0: Community Prompt Quality Signal](../plan.md)

## User Story

**As a** the atcr maintainer (Sam Estrin), relying on opted-in users' machines to actually deliver the aggregate signal
**I want** the aggregated per-persona+model counters (Story 1) transmitted over the existing Epic 28.0 content-free transport when — and only when — the independent opt-in gate (Story 2) resolves enabled
**So that** the quality signal actually reaches the aggregation endpoint and the living-library flywheel closes, while every non-opted-in user is guaranteed structurally — not just by documentation — that nothing ever leaves their machine

## Story Context

- **Background:** Stories 1-3 of this plan deliver the allowlisted payload type, the independent opt-in gate, and the local `--preview` surface — but each explicitly disclaims the actual transmission: Story 1 "does not implement the opt-in gate, the `--preview` render, the maintainer report, or the actual network send", Story 2 "emits no network calls and computes no payload ... the actual Send call site is out of scope", and Story 3 short-circuits before any network call by design. Without this story, the epic objective's "transported via 28.0" clause and AC1's "only per-persona+model counters and identifiers are transmitted" language have no implementation owner — a gate with nothing behind it guards nothing. The established call-site idiom is the passive ping's: `cmd/atcr/review.go:462-467` and `cmd/atcr/reconcile.go:186-191` wrap payload construction and `telemetry.FromContext(ctx).Send(...)` in `if telemetryGate() { ... }` at run completion.
- **Assumptions:**
  - Story 1 (aggregation + the `internal/telemetry/quality_signal.go` allowlisted payload type) and Story 2 (`qualitySignalGate()` + persisted config key) are complete — this story consumes both and builds neither.
  - The transport fork flagged by codebase discovery (`integration_gaps`: "Payload shape for the counters is an unresolved fork") is resolved at design-sprint: either a sibling allowlisted payload sent over `internal/telemetry/client.go`'s fail-open `Client` (whose `Send` is currently `Event`-typed and would gain a sibling method), or an extension of `internal/scorecard/cloudsync.go`'s `CloudSyncRecord`/`CloudSyncPersona` riding the existing `Push`. Either way, the payload on the wire is Story 1's locked struct and the gate consulted is Story 2's — never `telemetryGate()` or `resolveSyncCloud()`.
  - The send fires at run completion (review/reconcile finalization), on the transport's existing detached/fail-open path — it never blocks, delays, or alters the run's primary output.
- **Constraints:**
  - The gate is consulted BEFORE any payload construction, goroutine spawn, or client work — mirroring `telemetryGate`'s documented contract ("short-circuits BEFORE any goroutine spawns or payload is built — not merely before the HTTP call"). A disabled gate means zero network calls and zero payload allocation.
  - Fail-open is absolute: a transport failure (non-2xx, unreachable endpoint, timeout, or a panic inside the send path) never changes the review/reconcile run's exit code or stdout — mirroring `client.go`'s documented no-op/panic-safe contract and `resolveSyncCloudOutcome`'s (`cmd/atcr/cloudsync.go:86`) exit-code mapping.
  - `--preview` (Story 3) always wins: when the preview flag is set, the preview branch returns before this story's send path is reached, opted in or not.
  - The bytes transmitted are produced by the same payload-construction function `--preview` renders, so the preview can never drift from the real send (Story 3's AC 03-03 locks this; this story's AC 06-02 asserts it from the send side).
  - No new third-party dependency — stdlib plus the existing `internal/telemetry` / `internal/scorecard` transports, matching the epic's zero-new-dependency posture.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (aggregation + payload type), Story 2 (opt-in gate); transport itself already shipped in Epic 28.0 |

## Success Criteria (SMART Format)

- **Specific:** Call sites in `cmd/atcr/review.go` and `cmd/atcr/reconcile.go` (adjacent to the existing passive-ping emission) check `qualitySignalGate()` first; when enabled, they build Story 1's aggregated payload via the same constructor Story 3's `--preview` renders and hand it to the design-sprint-chosen Epic 28.0 transport; when disabled, they return before any payload construction or network work.
- **Measurable:** Tests prove three binary conditions: (a) gate disabled → zero HTTP requests observed at the do-request seam and no payload struct constructed (mirroring `TestClient_Send_EmptyEndpointNoOps`); (b) gate enabled → exactly one send per run whose body unmarshals into Story 1's payload struct and equals the `--preview` rendering byte-for-byte; (c) endpoint 500 / DNS failure / timeout → the run's exit code and stdout are identical to the gate-disabled baseline.
- **Achievable:** Pure call-site wiring plus a design-sprint transport choice — reuses `telemetry.New`/`Client.Send`'s detached-goroutine, HTTPS-only, empty-endpoint-no-op contract or `scorecard.Push` + `resolveSyncCloudOutcome`'s exit mapping verbatim; no new transport, no new config surface.
- **Relevant:** This is the epic objective's "transported via 28.0" made real — the only story that moves the aggregated counters off the user's machine, and the mechanism that makes Stories 2's consent gate and 3's preview meaningful.
- **Time-bound:** Implemented after Stories 1 and 2 land (payload type and gate must exist), within the same sprint, alongside Story 3.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [06-01](../acceptance-criteria/06-01-gate-disabled-short-circuit.md) | Gate-Disabled Short-Circuit — No Payload Built, No Network Call | Unit |
| [06-02](../acceptance-criteria/06-02-opted-in-send-transmits-allowlisted-payload.md) | Opted-In Run Sends Exactly One Allowlisted Payload, Byte-Identical to `--preview` | Unit/Integration |
| [06-03](../acceptance-criteria/06-03-transport-failure-fails-open.md) | Transport Failure Fails Open — Run Outcome and Exit Code Unchanged | Unit |

## Original Criteria Overview

1. With the quality-signal gate disabled, a review/reconcile run builds no quality-signal payload and attempts no network call — the disabled state is observable at the call site (before payload construction), not merely as a post-hoc no-op inside the transport client.
2. With the gate enabled, a completed run transmits exactly one payload — Story 1's allowlisted struct, byte-identical to what `--preview` renders for the same data — over the design-sprint-chosen Epic 28.0 transport.
3. A transport failure (non-2xx, unreachable endpoint, timeout, panic in the send path) never fails, blocks, or changes the output of the review/reconcile run — fail-open per `internal/telemetry/client.go`'s documented contract.

## Technical Considerations

- **Implementation Notes:** Add the call site adjacent to the existing passive-ping emission (`cmd/atcr/review.go:462`, `cmd/atcr/reconcile.go:186`): resolve `qualitySignalGate()` fresh per run (never cached across runs), and only inside the enabled branch construct Story 1's payload via the shared constructor and invoke the transport. If the design-sprint fork resolves to `telemetry.Client`, add a sibling send method for the new payload type (`Client.Send` is currently `Event`-typed) preserving the detached-goroutine, HTTPS-only, nil/empty-endpoint-no-op contract (`internal/telemetry/client.go:106`, `isHTTPS` at `:93`). If it resolves to the cloud-sync extension, wire through `runSyncCloud`'s path and reuse `resolveSyncCloudOutcome` for exit-code mapping. Ordering with Story 3: the `--preview` branch returns before this story's send path. Do not resolve the transport fork inside this story's tests — test gate behavior and fail-open semantics against a do-request seam regardless of which transport wins.
- **Integration Points:** `cmd/atcr/review.go:462-467` and `cmd/atcr/reconcile.go:186-191` (call-site idiom to mirror), `cmd/atcr/qualitysignal.go` (Story 2's `qualitySignalGate()`), `internal/telemetry/quality_signal.go` (Story 1's payload type + constructor), `internal/telemetry/client.go` (fail-open send contract), `internal/scorecard/cloudsync.go` + `cmd/atcr/cloudsync.go` (`Push` transport and `resolveSyncCloudOutcome` exit mapping, if the fork resolves that way), Story 3's `--preview` branch (strictly ordered before the send path).
- **Data Requirements:** No new data or config — the payload is exactly Story 1's allowlisted struct; endpoint/credential resolution follows the chosen transport's existing mechanism (the telemetry endpoint configuration, or `ATCR_API_KEY` + cloud endpoint for the `Push` fork).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Send path executes despite a disabled gate (call site placed after payload construction, or a stale cached gate result) | Critical (privacy line breach) | Gate-first ordering at the call site, asserted by AC 06-01 at the do-request seam; the gate is resolved fresh per run, never cached — mirroring `telemetryGate`'s per-run resolution |
| Transport fork is resolved inconsistently between `--preview` (Story 3) and this send path, so the preview no longer shows what is actually sent | High | A single payload-construction function shared by preview and send; AC 06-02 asserts the transmitted bytes equal the preview-rendered bytes for the same fixture (complementing Story 3's AC 03-03 from the send side) |
| Send failure (endpoint down, 500, timeout) fails or hangs the user's review run | High | Fail-open transports only — `client.go`'s detached goroutine + no-op contract, or `resolveSyncCloudOutcome`-style error mapping; AC 06-03 covers non-2xx, DNS failure, and timeout |
| Duplicate sends when one session runs both `review` and `reconcile` | Medium | Define per-run (not per-command) send semantics at design-sprint; the payload is a deterministic aggregate of the same underlying records, so a duplicate is idempotent and detectable rather than corrupting |

---

**Created:** July 17, 2026
**Status:** Draft - Awaiting Acceptance Criteria
