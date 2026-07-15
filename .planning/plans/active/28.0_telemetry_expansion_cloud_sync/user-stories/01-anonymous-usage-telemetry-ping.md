# User Story 1: Anonymous Usage Telemetry Ping

**Plan:** [28.0: Telemetry Expansion & Cloud Sync](../plan.md)

## User Story

**As a** product maintainer of `atcr`
**I want** the CLI to emit an anonymous, fail-open usage ping (`{"event": "review_run", "lang": "go", "lines": 450, "status": "success"}`) whenever a `review` or `reconcile` command completes
**So that** I can measure real-world adoption, run-success rates, and language distribution without ever capturing source code or risking a CLI hang/crash for the end user

## Story Context

- **Background:** `atcr` currently has no mechanism to measure top-of-funnel product adoption. `cmd/atcr/review.go:runReview` (around line 170) and `cmd/atcr/reconcile.go:runReconcile` (around line 71) are the two run-completion points in the codebase, and both already perform non-fatal, best-effort side effects (e.g. `scorecard.EmitForReconcile`) that log on failure and never alter the command's exit code. This story introduces a brand-new `internal/telemetry` package — no existing package in the codebase performs background/fire-and-forget HTTP calls — and wires a ping into both completion points using that exact fire-and-forget shape.
- **Assumptions:** A remote telemetry ingestion endpoint (or a stub/mock suitable for testing) is reachable via HTTPS; the event schema is limited to `event`, `lang`, `lines`, `status` fields (no source code, no file paths, no identifiers); the client is enabled by default and this story delivers only the client and its firing points, not the opt-out UX (covered by Story 2).
- **Constraints:** Must never block CLI execution — the ping fires in a goroutine with a bounded timeout. Must never panic the parent process on any internal error (network failure, marshal error, closed channel, etc.) per the "Panic Safety" / "Defer Cleanup" guidance in `implementation-standards.md`. Must never transmit source code, file contents, or file paths. Must follow the existing non-fatal side-effect pattern already used by `runReview` and `runReconcile` (log-and-continue, no exit-code change).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** A new `internal/telemetry` package provides a client whose `Send`/`Ping`-style call is invoked from both `cmd/atcr/review.go:runReview` and `cmd/atcr/reconcile.go:runReconcile` on command completion, emitting the `{event, lang, lines, status}` JSON payload.
- **Measurable:** Unit tests prove (1) the call returns/unblocks the parent command within a bounded time even when the network is unreachable or hangs, (2) the payload marshaled for transmission contains only the four allowlisted fields with no source code or file-path data, and (3) a simulated panic/error inside the telemetry goroutine does not propagate to or crash the parent command.
- **Achievable:** Built entirely on Go stdlib (`net/http`, `encoding/json`, `context` with timeout) — no new third-party dependency required, mirroring the plan's "Recommended Packages" conclusion.
- **Relevant:** Directly satisfies epic AC1 ("A background telemetry client exists and safely fails open") and AC2 ("The CLI securely sends anonymous usage events on run completion") — the foundational capability every other story in this plan (opt-out, persona hashing, cloud sync) builds on.
- **Time-bound:** Deliverable within this sprint cycle as the first story in the plan's execution order, ahead of Stories 2–5 which depend on the client existing.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-fire-and-forget-telemetry-send.md) | Fire-and-Forget Telemetry Send | Unit |
| [01-02](../acceptance-criteria/01-02-bounded-non-blocking-timeout.md) | Bounded, Non-Blocking Timeout | Unit |
| [01-03](../acceptance-criteria/01-03-panic-safe-fail-open.md) | Panic-Safe, Fail-Open Behavior | Unit |
| [01-04](../acceptance-criteria/01-04-schema-constrained-payload.md) | Schema-Constrained Payload (No Source Code or File Paths) | Unit |

## Original Criteria Overview

1. `internal/telemetry` exposes a client that sends a `{"event": "review_run", "lang": ..., "lines": ..., "status": ...}` JSON payload to a configurable endpoint over HTTPS, fired from a goroutine.
2. The call is bounded by a short timeout and is provably non-blocking: a hung or unreachable endpoint does not delay `runReview` or `runReconcile` completion or change their exit codes.
3. The client is panic-safe (recovers internally) and fail-open (logs failures at debug/trace level and continues) so no telemetry failure mode can crash or hang the CLI.
4. The transmitted payload is schema-constrained to `event`, `lang`, `lines`, `status` only — no source code, file contents, or file paths are ever included, verified by a test asserting the marshaled payload shape.


## Technical Considerations

- **Implementation Notes:** Implement `internal/telemetry` as a small, goroutine-based client with a package-level (or dependency-injected) instance constructed once at `cmd/atcr/main.go:newRootCmd` time, following the existing `LOG_LEVEL`-read pattern (`cmd/atcr/main.go` around line 217). The send call should accept a bounded `context.Context` (e.g. 2-3 second timeout) and use `defer recover()` around the goroutine body per the "Panic Safety" / "Defer Cleanup" guidance in `implementation-standards.md`. This story does not need to implement the opt-out check itself (Story 2) but should structure the client so a no-op/disabled mode can be added without reshaping the call sites.
- **Integration Points:** Call sites are `cmd/atcr/review.go:runReview` (~line 170) and `cmd/atcr/reconcile.go:runReconcile` (~line 71), invoked alongside (not replacing) the existing non-fatal side effects such as `scorecard.EmitForReconcile`. Consider a small shared helper (e.g. `run_helpers.go` in `cmd/atcr`) so both call sites build the event payload identically, per the codebase-discovery recommendation.
- **Data Requirements:** Event schema is exactly `{event string, lang string, lines int, status string}` — `lang` derived from the language already detected/used during the run, `lines` from the line count already computed for the scorecard, `status` from `"success"`/`"failure"` based on the command's outcome. No new persistent storage is required for this story.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A slow or unreachable telemetry endpoint delays or blocks CLI command completion | High | Fire the ping in a goroutine with a bounded context timeout and no blocking wait on the main command path; cover with a test that simulates a hung/unreachable endpoint and asserts the command still exits promptly |
| An unhandled panic inside the telemetry goroutine (e.g. malformed response, closed channel) crashes the parent CLI process | High | Wrap the goroutine body in `defer recover()` per implementation-standards.md's Panic Safety guidance; add a test that forces an internal panic and asserts the parent command still exits normally |
| Payload construction accidentally includes source code, file paths, or other sensitive data | Medium | Constrain the payload to a dedicated, narrow struct (`event`, `lang`, `lines`, `status` only) rather than reusing or extending existing scorecard structs; add a test asserting the marshaled JSON has exactly these four keys |

---

**Created:** July 15, 2026
**Status:** Draft - Awaiting Acceptance Criteria
