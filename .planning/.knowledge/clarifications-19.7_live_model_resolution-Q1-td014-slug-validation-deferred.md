---
id: mem-2026-07-09-be4a38
question: "Should cmd/atcr/models.go's runModelsRefresh validate fetched catalog id/canonical_slug (via validateResolvedSlug) before persisting the snapshot fixture, given the catalog GET is unauthenticated?"
created: 2026-07-09
last_retrieved: ""
sprints: [19.7_live_model_resolution]
files: [cmd/atcr/models.go, internal/personas/catalog.go, .planning/sprints/active/19.7_live_model_resolution/tech-debt-captured.md, .planning/sprints/active/19.7_live_model_resolution/sprint-plan.md]
tags: [clarifications, sprint-19.7_live_model_resolution, security, tech-debt, td-014]
retrievals: 0
status: active
type: clarifications
---

# Should cmd/atcr/models.go's runModelsRefresh validate fetche

## Decision

Not in Epic 19.7's scope — this is TD-014, deliberately accepted-and-deferred (not a defect to fix now). Rationale recorded in tech-debt-captured.md TD-014 and sprint-plan.md:1277: no exploit path today (the fixture is regenerated only by an explicit maintainer run and is human-reviewed in the PR diff before commit; no shipping persona carries a `binding:` that would resolve against a refreshed catalog), and adding validate-and-skip now would change the "faithfully snapshot the live catalog" contract and risk rejecting legitimate-but-unusual live entries. When this IS eventually addressed (a future robustness pass, or once the first `binding:`-carrying persona ships), the concrete fix is: validate each fetched `id`/`canonical_slug` via the existing `validateResolvedSlug` (internal/personas/catalog.go:344, already used at resolution time in catalog.go:197,247 and drift.go:219) before `WriteSnapshot`/`MarshalSnapshot` persists it, skip-with-warning or reject invalid entries, and add a hostile-slug test. Do not confuse this with TD-016 (the neighboring row about OPENROUTER_API_KEY Bearer-token AC prose) — TD README rows for cmd/atcr/models.go shared identical Fix-column text across TD-014/TD-016 from a copy-paste artifact; each row's own intent_note is the ground truth.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/models.go
- internal/personas/catalog.go
- .planning/sprints/active/19.7_live_model_resolution/tech-debt-captured.md
- .planning/sprints/active/19.7_live_model_resolution/sprint-plan.md
