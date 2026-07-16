# Sprint 28.0: Telemetry Expansion & Cloud Sync

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.
**Sprint Plan:** [sprint-plan.md](sprint-plan.md)
**Plan Type:** ✨ Feature
**Execution Mode:** Gated 🚧 (stops at each phase-boundary GATE for review)

---

## Overview

Introduce opt-in, lightweight telemetry to track actual product adoption, gather anonymous data for a crowdsourced Persona Leaderboard, and provide a `--sync-cloud` payload mechanism for the upcoming enterprise SaaS dashboard. The CLI emits a fail-open, panic-safe usage ping on `review`/`reconcile` completion; a strict `ATCR_TELEMETRY=0` env var and `atcr config set telemetry false` command let privacy-conscious teams opt out; Persona IDs are hashed for leaderboard aggregation without touching the existing Epic 10.0 privacy boundary; and `--sync-cloud` pushes an authenticated, anonymized scorecard to `atcr.dev/dashboard`.

## Timeline

**Complexity:** 11/12 (VERY COMPLEX)
**Total Duration:** 13 days across 6 phases

| Phase | Focus | Duration |
|-------|-------|----------|
| 1 | Research & Spike | 1 day |
| 2 | Foundation — Telemetry Client & Persona Hashing (Stories 1, 3) | 4 days |
| 3 | Gating — Telemetry Opt-Out (Story 2) | 2 days |
| 4 | Advanced — Cloud Sync Push (Story 4) | 3 days |
| 5 | Documentation (Story 5) | 1 day |
| 6 | Integration & Validation | 2 days |

## Expected Outcomes

- A new `internal/telemetry` package providing a fire-and-forget, panic-safe, bounded-timeout HTTP client wired into `runReview`/`runReconcile`
- `ATCR_TELEMETRY=0` and `atcr config set telemetry <bool>` as strict-OR'd opt-out surfaces, persisted via `ProjectConfig.Telemetry *bool`
- `HashPersonaID` (SHA-256, deterministic, non-reversible) and a dedicated telemetry/cloud-sync schema, fully isolated from `PublicRecord`/`scrubField`
- `--sync-cloud` flag authenticating via `ATCR_API_KEY` (`Bearer` token), with a dedicated `exitAuth` exit code for missing/invalid keys
- `docs/telemetry.md` (indexed from `docs/README.md`) plus an updated `docs/scorecard.md` Privacy Model section

## Risk Summary (Top 3)

1. **Privacy boundary erosion:** Bypassing `scrubField` for telemetry could silently weaken the Epic 10.0 public leaderboard export's documented privacy guarantee. Mitigated with a separate, explicitly-named schema/function plus a byte-for-byte regression test (AC 03-03).
2. **Blocking/hanging CLI:** A slow or unreachable telemetry or cloud-sync endpoint could block or delay CLI completion. Mitigated with goroutine + bounded timeout (telemetry) and a short request timeout executed after the run's outcome is finalized (`--sync-cloud`).
3. **Ambiguous auth failures:** Undistinguished auth-failure exit codes for `--sync-cloud` would break scripted/CI detection. Mitigated with a dedicated `exitAuth` code tested against both missing-key and invalid-key paths.

## Sprint Assets

- [sprint-plan.md](sprint-plan.md) — Full TDD task breakdown, gated phase-boundary reviews
- [metadata.md](metadata.md) — Sprint tracking and execution metrics
- [sprint-knowledge.yaml](sprint-knowledge.yaml) — Knowledge manifest (no prior-art matches found)
- [plan/sprint-design.md](plan/sprint-design.md) — Architecture, decomposition, test strategy, risk analysis
- [plan/original-requirements.md](plan/original-requirements.md) — Source-of-truth original request
- [plan/user-stories/](plan/user-stories/) — 5 user stories
- [plan/acceptance-criteria/](plan/acceptance-criteria/) — 19 acceptance criteria
- [plan/documentation/source.md](plan/documentation/source.md) — Documentation scan index (no specs scored ≥5/10)

---

**Next:** `/execute-sprint @.planning/sprints/active/28.0_telemetry_expansion_cloud_sync/`
