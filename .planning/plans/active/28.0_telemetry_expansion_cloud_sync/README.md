# Plan 28.0: Telemetry Expansion & Cloud Sync

## Overview
Opt-in, fail-open anonymous usage telemetry plus a `--sync-cloud` payload mechanism for the upcoming `atcr.dev` enterprise dashboard, with a strict opt-out and Persona ID hashing for a crowdsourced Persona Leaderboard. Originated from Epic Plan `28.0` (`.planning/epics/active/28.0_telemetry_expansion_cloud_sync.md`), routed through `/init-plan` because its derived scope (5 tasks / 4 components) exceeds `/execute-epic`'s scope guard (≤6 tasks / ≤2 components) and crosses a system boundary (external cloud sync).

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/28.0_telemetry_expansion_cloud_sync/`
- [ ] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/28.0_telemetry_expansion_cloud_sync/`
- [ ] **Design Sprint** - `/design-sprint @.planning/plans/active/28.0_telemetry_expansion_cloud_sync/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/28.0_telemetry_expansion_cloud_sync/`

## Timeline & Milestones
Timeline TBD — to be estimated during `/design-sprint` once user stories and acceptance criteria are in place. Complexity is assessed as **Complex** (cross-system: external network calls to `atcr.dev`; touches 4 components: `internal/scorecard`, `internal/telemetry` (new), `cmd/atcr`, `docs/`).

## Resource Requirements
Backend Team (Go). No new third-party dependencies required — see `plan.md` Recommended Packages. Requires coordination with whoever owns the `atcr.dev/dashboard` backend endpoint for the `--sync-cloud` payload contract and `ATCR_API_KEY` issuance (outside this plan's scope — the SaaS dashboard UI itself is explicitly out of scope).

## Expected Outcomes
- Anonymous, fail-open usage telemetry on `atcr` command completion.
- A strict, documented opt-out (`ATCR_TELEMETRY=0`, `atcr config set telemetry false`).
- Persona ID hashing added to the scorecard export schema for the Persona Leaderboard, without weakening the existing Epic 10.0 leaderboard export's privacy guarantees.
- A `--sync-cloud` flag that authenticates via `ATCR_API_KEY` (Bearer token) and pushes anonymized local scorecard data (including time/credits saved metrics) to `atcr.dev/dashboard`, with a distinct exit code on auth failure.

## Risk Summary
Primary risk is architectural: `internal/scorecard/export.go`'s `scrubField`/`PublicRecord` allowlist is a documented, by-design privacy boundary for the existing Epic 10.0 leaderboard export, and the new Persona ID hashing must not be implemented as a bypass of that same code path. Secondary risks are a slow/unreachable telemetry or cloud-sync endpoint blocking CLI execution (mitigated by fail-open, bounded-timeout background calls) and ambiguous auth-failure exit codes for `--sync-cloud` (mitigated by a dedicated, tested exit code). Full detail in `plan.md` Risk Mitigation.

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [User Stories](user-stories/) (pending)
- [Acceptance Criteria](acceptance-criteria/) (pending)
