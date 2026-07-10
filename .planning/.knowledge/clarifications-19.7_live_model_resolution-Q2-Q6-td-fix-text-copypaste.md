---
id: mem-2026-07-09-22e27e
question: "Are the copy-pasted \"Reconcile AC 08-02 Security Considerations OR attach Bearer header\" and \"pre-filter index names\" fix descriptions in duplicated TD rows meant to be applied literally, or are they leftover boilerplate from a neighboring row?"
created: 2026-07-09
last_retrieved: ""
sprints: [19.7_live_model_resolution]
files: [cmd/atcr/init.go, cmd/atcr/models.go, .planning/technical-debt/README.md, .planning/sprints/active/19.7_live_model_resolution/plan/acceptance-criteria/08-02-models-refresh-command-regenerates-snapshot.md]
tags: [clarifications, sprint-19.7_live_model_resolution, correctness, tech-debt, resolve-td]
retrievals: 0
status: active
type: clarifications
---

# Are the copy-pasted "Reconcile AC 08-02 Security Considerati

## Decision

When multiple adjacent TD README rows for the same file share byte-identical Fix-column text, treat it as a copy-paste artifact and verify against each row's own Problem text + intent_note before acting — the Fix often only genuinely applies to one of the rows. Confirmed pattern in atcr's TD README: rows 78-80 (cmd/atcr/init.go) all shared "pre-filter index names failing validatePersonaName," which only actually applied to row 78; rows 82-84 (cmd/atcr/models.go) all shared "Reconcile AC 08-02 Security Considerations OR attach Bearer header," which only applied to rows 83-84 (OPENROUTER_API_KEY transmission prose), not row 82 (stderr/exit-code enumeration contract, which needed no code fix at all — AC-mandated, deferred to Epic 19.8). Each row's own intent_note (deferred per sprint-plan §X.Y) and the cited code/AC file are the ground truth, not the shared Fix text. File refs: cmd/atcr/init.go:138-169, cmd/atcr/models.go:106-224, plan/acceptance-criteria/08-02-models-refresh-command-regenerates-snapshot.md:61, sprint-plan.md:1318.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/init.go
- cmd/atcr/models.go
- .planning/technical-debt/README.md
- .planning/sprints/active/19.7_live_model_resolution/plan/acceptance-criteria/08-02-models-refresh-command-regenerates-snapshot.md
