# Plan 30.0: Community Prompt Quality Signal

## Plan Overview
**Last Modified:** 2026-07-17 (refined via `/refine-plan --deep`)
**Plan Type:** feature
**Plan Goal:** Aggregate an opt-in, content-free quality signal (dismissed/confirmed counters per persona+model) from the Epic 24.0 dismissal data, transport it via the Epic 28.0 telemetry/cloud-sync pipeline, and surface it to the maintainer so weak reviewer prompts can be identified and routed back into the 19.8 drafting / 19.9 submission flywheel — with an absolute no-code, no-finding-content privacy guarantee and a local preview of exactly what would be sent.
**Target Users:** The atcr maintainer (Sam Estrin) — the sole consumer of the aggregated report; secondarily, contributors submitting persona prompts via Epic 19.9, who are indirectly targeted by the signal.
**Framework/Technology:** Go / Cobra CLI, `internal/telemetry` (fail-open HTTP client), `internal/registry` (config + debt store), the existing `--sync-cloud` cloud-sync pipeline.

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 5 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/30.0_community_prompt_quality_signal/`

## Feature Analysis Summary
This epic closes the persona living-library flywheel by turning Epic 24.0's raw per-finding `wontfix`/`resolved` dismissal outcomes into an aggregated, opt-in, content-free quality signal keyed by persona+model. It rides the existing Epic 28.0 telemetry/cloud-sync transport and its allowlist-payload enforcement pattern (a fixed-field struct with a locking regression test) rather than inventing a new transport. A new, independent opt-in gate (mirroring `telemetryEnabled`/`telemetryGate`) governs transmission, and a `--preview` surface renders the exact outbound payload so the maintainer (and any user) can inspect it before anything is ever sent. The maintainer-facing report surface identifies which persona+model combinations over- or under-report, feeding directly into the 19.7 drift-detection arc, the 19.8 drafting agent, and 19.9 community submission targets.

## Technical Planning Notes
- The existing `internal/telemetry.Event` struct is locked to 4 fields with a dedicated allowlist test — the new quality-signal payload needs its own equally strict, separately-tested struct, not an extension of `Event`.
- `--sync-cloud`'s existing `personas` payload entries already carry `persona_id_hash` + `model` — confirm during design-sprint whether the new counters extend this existing payload shape or introduce a sibling one.
- Epic 24.0's dismissal signal lives as a `wontfix`/`resolved` terminal status on debt records (`cmd/atcr/debt_resolve.go`); confirm whether persona/model attribution is already present on those records.
- The new opt-in must be independent of the existing passive-ping and `--sync-cloud` gates — no shared precedence logic.
- Docs must state the exact allowlisted fields, the opt-in mechanism, `--preview` behavior, and the absolute no-code/no-finding-content line (AC3).

## Implementation Strategy
Extend `internal/telemetry` with a new allowlisted quality-signal payload type and aggregation logic that reads dismissal outcomes from the Epic 24.0 debt-record data, add an independent opt-in gate and config-persisted setting following the existing `telemetryGate` pattern, implement a `--preview` flag that renders the exact payload locally without sending it, add a maintainer-facing report subcommand (following the `cmd/atcr/report.go` pattern) that surfaces over/under-reporting persona+model pairs, and document the full contract in `docs/telemetry.md`.

## Recommended Packages
No high-ROI packages identified — this epic reuses existing in-repo primitives (`internal/telemetry`, `internal/registry`, stdlib `crypto/sha256`/`encoding/json`), consistent with Epic 28.0's zero-new-dependency approach.

## User Story Themes
1. Aggregate per-persona+model dismissed/confirmed counters from the Epic 24.0 dismissal signal.
2. Independent opt-in gate for quality-signal transmission (config + env, non-overriding, mirroring existing telemetry gates).
3. Local `--preview` surface rendering the exact outbound payload before anything is sent.
4. Maintainer-facing report identifying over/under-reporting persona+model pairs.
5. Documentation of the full content-free telemetry contract (fields, opt-in, preview, privacy line).

## Planning Success Criteria
- Quality signal is opt-in, aggregate, and content-free; nothing is sent by default; `--preview` shows the exact outbound payload.
- Maintainer report identifies over/under-reporting personas+models, closing the loop to the 19.7 drift-detection arc, 19.8 drafting, and 19.9 submissions.
- `go test ./...` passes.
- Docs fully specify the telemetry contract (fields, opt-in, preview, privacy line).

## Risk Mitigation
- **Risk:** New payload type accidentally carries finding content or code. **Mitigation:** mirror the existing `Event` allowlist-test pattern (a dedicated regression test asserting the exact field set) for the new type.
- **Risk:** New opt-in gate gets coupled to or overridden by the existing passive-ping/`--sync-cloud` gates. **Mitigation:** implement as a fully independent gate/config key, matching the documented non-precedence pattern already in use.
- **Risk:** Persona+model attribution is missing from existing debt/dismissal records. **Mitigation:** confirm during design-sprint/user-stories whether this needs to be added as an explicit task, per `/refine-epic`'s ambiguous-T2 flag.

## Scope Boundaries (Out of Scope)
Per the source epic, this plan explicitly does **not** include:
- The submit flow and two-tier curation — owned by Epic 19.9; consumed here only as the persona keying dimension for the signal.
- The dismissal signal source (Epic 24.0) and the telemetry transport (Epic 28.0) themselves — this plan consumes them; it does not build them.
- Transmitting any code or finding content — the privacy line is absolute; only content-free aggregate counters + identifiers, opt-in.
- The hermes drafting agent (Epic 19.8) — separate; this plan only feeds it targets via the signal loop.

## Next Steps
1. `/find-documentation @.planning/plans/active/30.0_community_prompt_quality_signal/`
2. `/create-documentation @.planning/plans/active/30.0_community_prompt_quality_signal/`
3. `/create-user-stories @.planning/plans/active/30.0_community_prompt_quality_signal/`
4. `/create-acceptance-criteria @.planning/plans/active/30.0_community_prompt_quality_signal/`
5. `/design-sprint @.planning/plans/active/30.0_community_prompt_quality_signal/`
6. `/create-sprint @.planning/plans/active/30.0_community_prompt_quality_signal/`
