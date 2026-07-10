---
id: mem-2026-07-09-8d35b6
question: "Does closing 19.6's TD-011 (init/quickstart roster/index disjoint) also wire the community persona roster into .atcr/config.yaml so pinned personas become active by default?"
created: 2026-07-09
last_retrieved: ""
sprints: [19.7_live_model_resolution]
files: [cmd/atcr/init.go, .planning/sprints/active/19.7_live_model_resolution/plan/plan.md, .planning/sprints/active/19.7_live_model_resolution/sprint-plan.md]
tags: [clarifications, sprint-19.7_live_model_resolution, scope, architecture, roster-reconciliation]
retrievals: 0
status: active
type: clarifications
---

# Does closing 19.6's TD-011 (init/quickstart roster/index dis

## Decision

No. Epic 19.7's locked AC7 Clarification (Option B, recorded 2026-07-08 in plan/plan.md) only changed what roster `installCommunityPersonas` fetches-and-pins into the resolver pin dir — it derives that roster from the fetched `personas/community/index.json` entries instead of the disjoint hardcoded `builtins.Names()`. It explicitly does NOT touch the active `.atcr/config.yaml`/registry roster, which stays on `builtins.Names()`. So after online init/quickstart, the 10 index-derived community personas are installed as a resolvable pool but are not active on a default `atcr review` until the user hand-edits the roster. Wiring the active roster is an intentionally deferred future decision (TD row: cmd/atcr/init.go, intent_note "deferred per sprint-plan §7.6 — within locked Option B contract; roster wiring is a future decision, not a defect"), not a defect to fix. File refs: plan/plan.md Clarifications "AC7 Roster Reconciliation — LOCKED", cmd/atcr/init.go:110-131 (installCommunityPersonas), sprint-plan.md Phase 7 (§7.1-7.11, all landed to this exact scope).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/init.go
- .planning/sprints/active/19.7_live_model_resolution/plan/plan.md
- .planning/sprints/active/19.7_live_model_resolution/sprint-plan.md
