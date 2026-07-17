# User Story 3: Local `--preview` Surface for the Outbound Quality-Signal Payload

**Plan:** [30.0: Community Prompt Quality Signal](../plan.md)

## User Story

**As a** privacy-conscious atcr maintainer (or any user deciding whether to opt in)
**I want** a local `--preview` flag that renders the exact JSON payload the quality-signal transmission would send, without sending anything
**So that** I can verify with my own eyes — not just trust documentation — that only aggregate, content-free persona+model counters leave the machine, before ever opting in

## Story Context

- **Background:** Epic 24.0 already produces per-finding `wontfix`/`resolved` dismissal outcomes on debt records. Story 1 of this plan aggregates those into per-persona+model counters; Story 2 adds an independent opt-in gate mirroring the existing `telemetryGate` pattern. Neither of those stories is trustworthy to a skeptical maintainer without a way to *see* the wire payload before it is ever sent — the epic's Objective names this preview explicitly as the mechanism that makes the content-free guarantee verifiable rather than merely asserted. Codebase discovery confirms no dry-run/preview surface exists today for any outbound payload (`cmd/atcr/flags.go`) — neither the passive telemetry ping nor `--sync-cloud` can be inspected before it fires.
- **Assumptions:**
  - The new quality-signal payload type (`internal/telemetry/quality_signal.go`) already exists (built by Story 1) with a fixed, allowlisted field set mirroring `internal/telemetry/event.go`'s `Event` struct (no `omitempty`, no free-text, no code/finding-body fields).
  - `--preview` is a flag on whichever command triggers quality-signal aggregation/transmission (registered via an `addQualitySignalFlags`-style helper in `cmd/atcr/flags.go`, following the existing `addSyncCloudFlags` pattern).
  - `--preview` must work standing alone: it does not require the opt-in gate (Story 2) to be enabled, since its entire purpose is to let an *undecided* user inspect the payload before deciding to opt in.
- **Constraints:**
  - `--preview` MUST short-circuit before any network call, `net/http` client construction, or DNS resolution — no send path may execute, opted in or not.
  - The previewed payload MUST be produced via the same `json.Marshal` call (or an identical code path) used by the real send, so the preview can never drift from what is actually transmitted.
  - Output must be human-readable JSON (pretty-printed) written to the command's normal stdout stream, consistent with existing atcr CLI output conventions.
  - No new third-party dependency — stdlib `encoding/json` only, matching the epic's zero-new-dependency posture.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | Story 1 (per-persona+model counter aggregation and the `quality_signal.go` payload type must exist to preview) |

## Success Criteria (SMART Format)

- **Specific:** Running the relevant atcr command with `--preview` prints the exact outbound quality-signal JSON payload to stdout and exits without making any network call, regardless of the opt-in gate's state.
- **Measurable:** A test asserts zero HTTP client invocations occur when `--preview` is passed (via the same do-request seam pattern as `TestClient_Send_EmptyEndpointNoOps`), and a golden/round-trip test confirms the printed JSON unmarshal's into the same struct instance that a real send would serialize.
- **Achievable:** Reuses the existing allowlist-struct pattern (`internal/telemetry/event.go`) and flag-registration pattern (`cmd/atcr/cloudsync.go`'s `addSyncCloudFlags`) already proven in the codebase; no new transport or aggregation logic required.
- **Relevant:** Directly satisfies epic AC1's requirement for "a local preview [that] shows exactly what would be sent" — the trust mechanism the whole opt-in design depends on.
- **Time-bound:** Completed within this sprint cycle alongside Stories 1 and 2, before the maintainer-facing report (Story 4) is implemented.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [03-01](../acceptance-criteria/03-01-preview-flag-prints-exact-payload.md) | `--preview` Flag Renders the Exact Outbound JSON Payload | Integration |
| [03-02](../acceptance-criteria/03-02-preview-bypasses-network-and-optin-gate.md) | `--preview` Never Sends — No Network Call, Independent of Opt-In Gate State | Unit |
| [03-03](../acceptance-criteria/03-03-preview-never-drifts-from-real-send.md) | Regression Test Locks `--preview` Output to the Real Send's Marshal Path | Unit |

## Original Criteria Overview

1. `--preview` is available on the command(s) that would otherwise transmit the quality signal, and prints the exact allowlisted JSON payload without sending it.
2. `--preview` never opens a network connection or requires the opt-in gate to be enabled — it works identically whether opted in or not.
3. A regression test proves the preview output is byte-for-byte the same shape (same struct, same marshal path) as what a real send would transmit, so the preview can never silently drift from reality.

## Technical Considerations

- **Implementation Notes:** Add a `--preview` bool flag via a new `addQualitySignalFlags`-style helper in `cmd/atcr/flags.go`, following the chained-`PreRunE` convention already used by `addRangeFlags`/`addSyncCloudFlags` (prev-first, never overwritten). In the command's run path, build the `quality_signal.go` payload struct first, then branch: if `--preview` is set, `json.MarshalIndent` it to the command's stdout and return before any send/opt-in check; otherwise proceed to the normal gated-send path from Story 2.
- **Integration Points:** `cmd/atcr/flags.go` (flag registration), the command wiring introduced by Story 2 (opt-in gate check must be bypassed, not merely skipped-if-off), `internal/telemetry/quality_signal.go` (the payload struct from Story 1).
- **Data Requirements:** No new data — reuses the aggregated counters from Story 1. The preview output is exactly the JSON that `json.Marshal` produces for the allowlisted struct; no additional formatting metadata beyond indentation is added.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Preview code path diverges from the real send path over time (e.g., a future field added to the send call but not the preview), silently breaking the trust guarantee | High | Preview marshals the identical struct instance/function used by the real send (single source of marshaling), not a hand-copied reconstruction; add a regression test asserting this equivalence |
| `--preview` accidentally triggers a partial network call (e.g., DNS pre-resolution or client construction with side effects) before the short-circuit | Medium | Place the `--preview` branch before any client/transport construction in the command's run path; add a test asserting zero HTTP calls when `--preview` is set, mirroring `TestClient_Send_EmptyEndpointNoOps` |
| Maintainer or contributor mistakes `--preview` output for confirmation that data was sent (false sense of completion) | Low | Preview output includes an explicit "not sent" marker/message alongside the JSON, and docs (Story 5) state the preview never transmits |

---

**Created:** July 17, 2026
**Status:** Draft - Awaiting Acceptance Criteria
