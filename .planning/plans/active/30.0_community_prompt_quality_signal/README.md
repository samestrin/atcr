# Plan 30.0: Community Prompt Quality Signal

## Overview
Close the persona living-library flywheel by aggregating an opt-in, content-free quality signal (per-persona+model dismissed/confirmed counters, derived from Epic 24.0's dismissal data and transported via Epic 28.0's telemetry/cloud-sync pipeline) and surfacing it to the maintainer — with a local `--preview` of exactly what would be sent and an absolute no-code/no-finding-content privacy line.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/30.0_community_prompt_quality_signal/`
- [ ] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/30.0_community_prompt_quality_signal/`
- [ ] **Design Sprint** - `/design-sprint @.planning/plans/active/30.0_community_prompt_quality_signal/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/30.0_community_prompt_quality_signal/`

## Timeline & Milestones
TBD — estimated after `/design-sprint` scores complexity and phase structure.

## Resource Requirements
Single-developer implementation (Go/Cobra CLI + `internal/telemetry`/`internal/registry` changes plus documentation); no new third-party dependencies. Depends on completed Epics 19.9, 24.0, and 28.0 already being in the codebase (confirmed present).

## Expected Outcomes
- A new, independently-opted-in, content-free aggregate quality signal keyed by persona+model.
- A local `--preview` surface showing the exact outbound payload before anything is sent.
- A maintainer-facing report identifying over/under-reporting personas+models.
- Documentation of the exact telemetry contract, closing the loop to the 19.8 drafting agent and 19.9 community submissions.

## Risk Summary
- New payload type must follow the existing strict allowlist-plus-regression-test pattern to keep the no-code/no-finding-content guarantee enforceable, not just documented.
- New opt-in gate must stay fully independent of the existing passive-ping and `--sync-cloud` gates.
- Whether persona+model attribution already exists on dismissal records is unconfirmed — may need an explicit task.

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [User Stories](user-stories/) (pending)
- [Acceptance Criteria](acceptance-criteria/) (pending)
