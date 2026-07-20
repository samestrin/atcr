# Plan 32.1: Multi-Tier Fix Execution Engine

## Overview

Adds a configurable complexity ceiling to atcr's fix-generation executor so cheap/local models can be reserved for simple findings while expensive frontier models handle the rest. Discovery found the complexity signal itself (`EstMinutes`) already exists end-to-end in the review pipeline — the net-new work is the config surface, the routing/skip logic in `generateFixes`, skip visibility, and docs.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/32.1_multi_tier_fix_execution/`
- [x] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/32.1_multi_tier_fix_execution/`
- [ ] **Design Sprint** - `/design-sprint @.planning/plans/active/32.1_multi_tier_fix_execution/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/32.1_multi_tier_fix_execution/`

## Timeline & Milestones

No fixed deadline. Sized Semi-Complex (5 estimated user stories); expect a single sprint once design questions below are resolved.

## Resource Requirements

Single Go backend component (`internal/registry`, `internal/verify`, plus `docs/`). No new external dependencies. No new infrastructure.

## Expected Outcomes

- `atcr.yaml` can express a complexity ceiling on the executor (`max_estimated_minutes`), validated like every other executor field.
- Findings whose `EstMinutes` exceeds the configured ceiling are skipped (not attempted) and the skip is visibly logged, not silently dropped.
- A documented, worked example shows a cheap-tier pass followed by a frontier-tier pass against the same findings, delivering the "multi-tier" workflow the original epic asked for.

## Risk Summary

Primary risk is a design ambiguity, not a technical one: whether "multi-tier" requires atcr to walk an in-process ordered chain of executors automatically, or whether a single executor + ceiling + a second independently-configured run satisfies the intent (the original epic's own AC4 phrasing suggests the latter). This must be resolved as a clarification before `/design-sprint` locks the phase structure — picking the wrong reading late would mean redesigning `Registry.Executor`'s schema mid-sprint. Secondary risks (estimate noise in `EstMinutes`, silent-drop of over-ceiling findings) have mitigations already captured in `plan.md`.

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [User Stories](user-stories/) (pending)
- [Acceptance Criteria](acceptance-criteria/) (pending)
