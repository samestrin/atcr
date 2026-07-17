# User Story 2: Independent Opt-In Gate for Quality-Signal Transmission

**Plan:** [30.0: Community Prompt Quality Signal](../plan.md)

## User Story

**As a** privacy-conscious atcr user running reviews against private code
**I want** the community prompt quality signal to stay off by default and require an explicit, independent opt-in (env var or persisted config key) before anything is transmitted
**So that** I retain full control over whether aggregate persona+model counters ever leave my machine, without that consent being silently granted or revoked by an unrelated feature's telemetry setting

## Story Context

- **Background:** atcr already has two independent, non-overriding opt-in/opt-out surfaces: `telemetryGate()` in `cmd/atcr/telemetry.go` (env var `ATCR_TELEMETRY` OR-combined with a persisted `.atcr/config.yaml` `telemetry` key, via the pure `telemetryEnabled` truth-table function) governs the passive anonymous usage ping, and `resolveSyncCloud()` in `cmd/atcr/cloudsync.go` governs the explicit `--sync-cloud` push (flag presence plus a valid `ATCR_API_KEY`), deliberately NOT gated by `telemetryGate`. The epic's quality-signal payload (per-persona+model dismissed/confirmed counters from Epic 24.0) needs its own, third, independent opt-in surface — this story builds that gate and its config-persistence plumbing, but not the payload aggregation, preview rendering, or report surfacing themselves (those are Stories 1, 3, and 4).
- **Assumptions:** The gate follows the exact structural pattern of `telemetryEnabled`/`telemetryGate` — a pure, total combining function plus a thin I/O wrapper that reads env + persisted config — so it is exhaustively unit-testable and carries no precedence logic that a caller could get wrong. The persisted setting lives in the same `.atcr/config.yaml` file as the `telemetry` key, under a new key (e.g. `quality_signal`), using the existing atomic mkdir-lock write path in `internal/registry/telemetry_setting.go` rather than a new file or lock mechanism.
- **Constraints:** The new gate MUST NOT read, combine with, or be short-circuited by `telemetryGate()` or `resolveSyncCloud()`'s state — no shared boolean, no shared precedence table, no fallthrough between the three surfaces. A malformed persisted value must fail safe to disabled (never silently re-enable transmission), matching `LoadTelemetrySetting`'s existing contract. This story emits no network calls and computes no payload — it delivers only the boolean gate plus the `atcr config set quality_signal <bool>` CLI surface; the actual Send call site is out of scope until the aggregation (Story 1) and transport wiring exist.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** A new `qualitySignalEnabled(envEnabled bool, cfgQualitySignal *bool) bool` pure function and a `qualitySignalGate() bool` I/O wrapper exist (mirroring `telemetryEnabled`/`telemetryGate`), a `LoadQualitySignalSetting`/`SetQualitySignalSetting` pair exists in `internal/registry` (mirroring `LoadTelemetrySetting`/`SetTelemetrySetting`), and `atcr config set quality_signal <true|false>` is accepted by the config-key allowlist in `cmd/atcr/config.go`.
- **Measurable:** The gate defaults to disabled with no env var and no persisted config set (nothing sent by default per epic AC1); the four-combination truth table (env × config) for the new gate is covered by unit tests, matching the existing `telemetryEnabled` test's exhaustiveness; `atcr config set quality_signal true` and `atcr config set quality_signal false` round-trip correctly through `.atcr/config.yaml` without disturbing the existing `telemetry` key.
- **Achievable:** Directly reuses the existing atomic yaml-node-edit write path (`withConfigLock`, `configMapping`, `setMappingBool`) and the existing pure-combiner test pattern — no new file format, no new lock mechanism, no new CLI parsing pattern.
- **Relevant:** This is the consent mechanism the epic's AC1 depends on — without an independent, off-by-default gate, no downstream story (aggregation, preview, report) can be safely wired to a transmission path.
- **Time-bound:** Completable within a single sprint phase alongside the other Theme stories, ahead of any story that wires an actual Send call site to this gate.

## Acceptance Criteria Overview

1. With no `ATCR_QUALITY_SIGNAL` (or equivalent) env var set and no persisted `.atcr/config.yaml` `quality_signal` key, the gate resolves to disabled — quality-signal transmission never fires by default.
2. The gate is implemented as a pure, total combining function with an exhaustive four-combination truth table (env enabled/disabled x config unset/true/false), unit-tested independently of `telemetryEnabled`'s existing tests, and never reads or is short-circuited by `telemetryGate()` or `resolveSyncCloud()` state.
3. `atcr config set quality_signal <true|false>` persists the key to `.atcr/config.yaml` via the existing atomic mkdir-lock write path, leaving the `telemetry` key and all other config keys untouched; a malformed persisted value fails safe to disabled and surfaces as a loud error, never a silent re-enable.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/30.0_community_prompt_quality_signal/`_

## Technical Considerations

- **Implementation Notes:** Add `qualitySignalEnabled`/`qualitySignalGate` to `cmd/atcr` (new file, e.g. `cmd/atcr/qualitysignal.go`, or colocated with the eventual aggregation code from Story 1 — confirm placement during design-sprint). Add `LoadQualitySignalSetting`/`SetQualitySignalSetting` to `internal/registry`, either as new functions in `telemetry_setting.go` or a sibling file, reusing `withConfigLock`, `configMapping`, and `setMappingBool` verbatim (they are already key-agnostic). Extend the `runConfigSet` allowlist in `cmd/atcr/config.go:59` from the single literal `"telemetry"` check to admit `"quality_signal"` as a second allowed key, dispatching to the correct Load/Set pair per key.
- **Integration Points:** `internal/registry/telemetry_setting.go` (pattern to mirror, and possibly the file that gains the new functions), `cmd/atcr/config.go` (`runConfigSet`'s key allowlist), `cmd/atcr/telemetry.go` (structural pattern reference only — no shared state). The eventual Send call site for the aggregated payload (Stories 1/3) will call `qualitySignalGate()` the same way `review.go`/`reconcile.go` call `telemetryGate()`, but wiring that call site is not this story's scope.
- **Data Requirements:** One new boolean key in `.atcr/config.yaml` (e.g. `quality_signal: true|false`), unset by default; no new file, no new schema migration, no data beyond the boolean flag itself.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| New gate accidentally shares logic or state with `telemetryGate()`/`resolveSyncCloud()`, coupling consent across features | High | Mirror the pattern structurally but keep the function and its config key entirely separate; add a test asserting the new gate's result is independent of `telemetry`'s persisted value (e.g. `telemetry: false` + `quality_signal: true` still resolves quality-signal enabled) |
| Config-key allowlist extension in `runConfigSet` introduces a typo or silently accepts an unintended key | Medium | Extend via an explicit switch/allowlist (not a loosened prefix match), with a table-driven test covering both valid keys and a rejected unknown key |
| Malformed persisted `quality_signal` value fails open instead of safe | High | Mirror `LoadTelemetrySetting`'s existing fail-safe contract exactly (parse error surfaces as a loud error, never defaults to enabled); add a regression test for a corrupt value |

---

**Created:** July 17, 2026
**Status:** Draft - Awaiting Acceptance Criteria
</content>
