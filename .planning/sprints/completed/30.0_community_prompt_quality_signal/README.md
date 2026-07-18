# Sprint 30.0: Community Prompt Quality Signal

**Metadata:** See [metadata.md](metadata.md)
**Sprint Plan:** [sprint-plan.md](sprint-plan.md)
**Plan Type:** ✨ Feature

---

## Overview

An opt-in, aggregate, content-free "quality signal" that tells the maintainer which reviewer prompts (persona+model pairs) are over- or under-reporting — derived from Epic 24.0's dismissal outcomes already recorded in `.atcr/debt/`. No code, file path, or finding text ever leaves the machine: only per-persona+model dismissed/confirmed counters and identifiers, gated behind an independent opt-in, with a `--preview` flag showing exactly what would be sent before anyone opts in. This closes the persona living-library flywheel by pointing the hermes drafting agent (19.8) and community submissions (19.9) at the prompts that most need tuning.

## Timeline

**Complexity:** 10/12 (VERY COMPLEX)
**Estimated Duration:** 14 days
**Phases:** 7

| Phase | Focus | Duration |
|-------|-------|----------|
| 1 | Foundation — Aggregation & Schema | 3 days |
| 2 | Independent Opt-In Gate | 1.5 days |
| 3 | Local `--preview` Surface | 1.5 days |
| 4 | Maintainer-Facing Report | 2.5 days |
| 5 | Gated Transport Wiring | 2.5 days |
| 6 | Documentation | 1 day |
| 7 | Validation | 2 days |

## Expected Outcomes

- `localdebt.Record` gains an optional `Model` field (`SchemaVersion` 1→2), populated at write time from `fanout.AgentStatus.Model`; a new `AggregateQualitySignal` folds the append-only stream by `ID` and groups by `(persona, model)`, summing dismissed/confirmed counts.
- `internal/telemetry/quality_signal.go` ships a locked, 4-field allowlisted payload type with a regression test that fails loudly on any future unauthorized field addition.
- An independent `qualitySignalEnabled`/`qualitySignalGate` opt-in gate (off by default) persists via `atcr config set quality_signal <bool>`, sharing no state with `telemetryGate`/`resolveSyncCloud`.
- A `--preview` flag renders the exact outbound JSON payload — byte-identical to what a real send would transmit — without ever making a network call.
- `cmd/atcr/telemetry_report.go` (a new, distinct subcommand) ranks persona+model pairs by dismissal rate for the maintainer, sourcing exclusively from the content-free aggregation.
- Gated transport wiring at the `review`/`reconcile` completion call sites, fail-open absolute on any transport failure.
- `docs/telemetry.md` documents the exact field allowlist, opt-in mechanism, `--preview` behavior, and restates the `HashPersonaID` unsalted-hash caveat (TD-007).

## Risk Summary (Top 3)

1. **A future field addition silently grows the outbound payload to leak code, file paths, or finding content.** Mitigated by a locked, no-`omitempty`, fixed-field-count allowlist struct plus a dedicated regression test mirroring `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys` — the test fails loudly on any unreviewed field addition.
2. **The opt-in gate silently couples to `telemetryGate`/`resolveSyncCloud`, so an unrelated feature's setting grants or revokes quality-signal consent.** Mitigated by a pure, independent combining function with its own test file, an exhaustive six-state truth table, and an explicit test asserting divergent results across the two surfaces are simultaneously achievable.
3. **`--preview` and the real send drift apart over time (a field added to one path but not the other).** Mitigated by both paths consuming a single shared payload-construction function/struct instance, locked by a byte-for-byte equivalence test (AC 03-03/06-02) from both the preview and send sides.

## Sprint Assets

- [sprint-plan.md](sprint-plan.md) — Phase-by-phase TDD task breakdown
- [metadata.md](metadata.md) — Tracking document
- [sprint-knowledge.yaml](sprint-knowledge.yaml) — Knowledge manifest
- [plan/](plan/) — Original plan, user stories, acceptance criteria, sprint design, documentation
