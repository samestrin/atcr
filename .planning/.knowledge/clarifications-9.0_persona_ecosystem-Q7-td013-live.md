---
id: mem-2026-06-24-e898cb
question: "Is the TD-013 score-map key normalization fix (strings.ToLower in SelectEligibleSkeptics) already in production code?"
created: 2026-06-24
last_retrieved: ""
sprints: [9.0_persona_ecosystem]
files: [internal/verify/select.go, .planning/technical-debt/README.md]
tags: [clarifications, sprint-9.0_persona_ecosystem, td-cleanup, phantom-entries, score-map, td-013]
retrievals: 0
status: active
type: clarifications skill — resolve-td batch 2026-06-24
---

# Is the TD-013 score-map key normalization fix (strings.ToLow

## Decision

Yes — the TD-013 fix is already live at select.go:148: `si, sj := scores[strings.ToLower(matched[i])], scores[strings.ToLower(matched[j])]`. The TD README contains a cascade of phantom archer-backup reviewer duplicate entries at escalating ghost line numbers (214, 311, 795, 913, 1045, 1177, 1309...) all deferring to TD-013 with empty FIX text. These are all artifacts from the reviewer's runaway multi-pass scan. The canonical TD-013 row (README.md:42) can be marked resolved once the live fix is verified; all phantom duplicates should be batch-closed. select.go is 190 lines — any citation beyond line 190 is definitively phantom.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/select.go
- .planning/technical-debt/README.md
