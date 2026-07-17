# User Story 2: Independent Opt-In Gate for Quality-Signal Transmission

**Plan:** [30.0: Community Prompt Quality Signal](../plan.md)

## User Story

**As a** privacy-conscious atcr user running reviews against private code
**I want** the community prompt quality signal to stay off by default and require an explicit, independent opt-in (env var or persisted config key) before anything is transmitted
**So that** I retain full control over whether aggregate persona+model counters ever leave my machine, without that consent being silently granted or revoked by an unrelated feature's telemetry setting

## Story Context

- **Background:** atcr already has two independent, non-overriding opt-in/opt-out surfaces: `telemetryGate()` in `cmd/atcr/telemetry.go` (env var `ATCR_TELEMETRY` OR-combined with a persisted `.atcr/config.yaml` `telemetry` key, via the pure `telemetryEnabled` truth-table function) governs the passive anonymous usage ping, and `resolveSyncCloud()` in `cmd/atcr/cloudsync.go` governs the explicit `--sync-cloud` push (flag presence plus a valid `ATCR_API_KEY`), deliberately NOT gated by `telemetryGate`. The epic's quality-signal payload (per-persona+model dismissed/confirmed counters from Epic 24.0) needs its own, third, independent opt-in surface — this story builds that gate and its config-persistence plumbing, but not the payload aggregation, preview rendering, or report surfacing themselves (those are Stories 1, 3, and 4).
- **Assumptions:** The gate follows the exact structural pattern of `telemetryEnabled`/`telemetryGate` — a pure, total combining function plus a thin I/O wrapper that reads env + persisted config — so it is exhaustively unit-testable and carries no precedence logic that a caller could get wrong. The persisted setting lives in the same `.atcr/config.yaml` file as the `telemetry` key, under a new key (e.g. `quality_signal`), using the existing atomic mkdir-lock write path in `internal/registry/telemetry_setting.go` rather than a new file or lock mechanism.
  - **Opt-in semantics are a structural mirror, NOT a semantic copy, of the passive ping.** The existing `telemetryEnabled` is opt-OUT: `telemetryEnabledFromEnv` (`cmd/atcr/main.go:262`) treats an unset `ATCR_TELEMETRY` as enabled (fail-open), and the pure function is `envEnabled && (cfgTelemetry == nil || *cfgTelemetry)` — each surface can only ever *disable*. The quality-signal gate is opt-IN ("nothing is sent by default", epic AC1), so it must not inherit either that env default or that AND shape: an unset quality-signal env var resolves disabled (fail-closed), and the combining function enables when **either** surface explicitly opts in — `qualitySignalEnabled(envEnabled, cfgQualitySignal) = envEnabled || (cfgQualitySignal != nil && *cfgQualitySignal)` — so that `atcr config set quality_signal true` alone is sufficient consent. Expected truth table (env unset-or-disabled × config unset/true/false): `(unset, unset) → disabled`; `(unset, true) → enabled`; `(unset, false) → disabled`; `(enabled, unset) → enabled`; `(enabled, true) → enabled`; `(enabled, false) → enabled` — once a user has explicitly opted in via env, a stale `false` in config must not silently revoke that consent; revocation is `atcr config set quality_signal false` plus unsetting the env var, and the exact override semantics between the two opt-in surfaces are confirmed at design-sprint. A malformed persisted value fails safe to disabled, per `LoadTelemetrySetting`'s existing contract.
- **Constraints:** The new gate MUST NOT read, combine with, or be short-circuited by `telemetryGate()` or `resolveSyncCloud()`'s state — no shared boolean, no shared precedence table, no fallthrough between the three surfaces. A malformed persisted value must fail safe to disabled (never silently re-enable transmission), matching `LoadTelemetrySetting`'s existing contract. This story emits no network calls and computes no payload — it delivers only the boolean gate plus the `atcr config set quality_signal <bool>` CLI surface; the actual Send call site is out of scope until the aggregation (Story 1) and transport wiring exist.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** A new `qualitySignalEnabled(envEnabled bool, cfgQualitySignal *bool) bool` pure function and a `qualitySignalGate() bool` I/O wrapper exist (mirroring `telemetryEnabled`/`telemetryGate`), a `LoadQualitySignalSetting`/`SetQualitySignalSetting` pair exists in `internal/registry` (mirroring `LoadTelemetrySetting`/`SetTelemetrySetting`), and `atcr config set quality_signal <true|false>` is accepted by the config-key allowlist in `cmd/atcr/config.go`.
- **Measurable:** The gate defaults to disabled with no env var and no persisted config set (nothing sent by default per epic AC1); the full env × config truth table for the new gate (the six states enumerated in Assumptions above) is covered by unit tests, matching the exhaustiveness of the existing `TestTelemetryEnabled_FourWayMatrix` test; `atcr config set quality_signal true` and `atcr config set quality_signal false` round-trip correctly through `.atcr/config.yaml` without disturbing the existing `telemetry` key.
- **Achievable:** Directly reuses the existing atomic yaml-node-edit write path (`withConfigLock`, `configMapping`, `setMappingBool`) and the existing pure-combiner test pattern — no new file format, no new lock mechanism, no new CLI parsing pattern.
- **Relevant:** This is the consent mechanism the epic's AC1 depends on — without an independent, off-by-default gate, no downstream story (aggregation, preview, report) can be safely wired to a transmission path.
- **Time-bound:** Completable within a single sprint phase alongside the other Theme stories, ahead of any story that wires an actual Send call site to this gate.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [02-01](../acceptance-criteria/02-01-quality-signal-off-by-default.md) | Quality Signal Resolves Disabled With No Env Var and No Persisted Config | Unit |
| [02-02](../acceptance-criteria/02-02-independent-truth-table-no-shared-state.md) | Pure Four-Combination Gate, Independent of `telemetryGate`/`resolveSyncCloud` | Unit |
| [02-03](../acceptance-criteria/02-03-config-set-quality-signal-persists.md) | `atcr config set quality_signal <bool>` Persists Atomically, Fails Safe on Corruption | Unit/Integration |

## Original Criteria Overview

1. With no `ATCR_QUALITY_SIGNAL` (or equivalent) env var set and no persisted `.atcr/config.yaml` `quality_signal` key, the gate resolves to disabled — quality-signal transmission never fires by default.
2. The gate is implemented as a pure, total combining function with an exhaustive truth table over the full env × config matrix (env enabled/unset × config unset/true/false — the six opt-in states enumerated in Assumptions), unit-tested independently of `telemetryEnabled`'s existing tests, and never reads or is short-circuited by `telemetryGate()` or `resolveSyncCloud()` state.
3. `atcr config set quality_signal <true|false>` persists the key to `.atcr/config.yaml` via the existing atomic mkdir-lock write path, leaving the `telemetry` key and all other config keys untouched; a malformed persisted value fails safe to disabled and surfaces as a loud error, never a silent re-enable.

## Technical Considerations

- **Implementation Notes:** Add `qualitySignalEnabled`/`qualitySignalGate` to `cmd/atcr` (new file, e.g. `cmd/atcr/qualitysignal.go`, or colocated with the eventual aggregation code from Story 1 — confirm placement during design-sprint). Add `LoadQualitySignalSetting`/`SetQualitySignalSetting` to `internal/registry`, either as new functions in `telemetry_setting.go` or a sibling file, reusing `withConfigLock`, `configMapping`, and `setMappingBool` verbatim (they are already key-agnostic). Extend the `runConfigSet` allowlist in `cmd/atcr/config.go:59` from the single literal `"telemetry"` check to admit `"quality_signal"` as a second allowed key, dispatching to the correct Load/Set pair per key.
- **Integration Points:** `internal/registry/telemetry_setting.go` (pattern to mirror, and possibly the file that gains the new functions), `cmd/atcr/config.go` (`runConfigSet`'s key allowlist), `cmd/atcr/telemetry.go` (structural pattern reference only — no shared state). The Send call site for the aggregated payload (owned by Story 6) will call `qualitySignalGate()` the same way `review.go`/`reconcile.go` call `telemetryGate()`, but wiring that call site is not this story's scope.
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
