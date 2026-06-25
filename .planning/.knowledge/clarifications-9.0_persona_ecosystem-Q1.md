---
id: mem-2026-06-24-590ced
question: "Should archer-backup group-4 TD rows citing internal/verify/select.go at lines beyond 190 be resolved, and what is the action for TD-015 phantom duplicates?"
created: 2026-06-24
last_retrieved: ""
sprints: [9.0_persona_ecosystem]
files: [internal/verify/select.go, internal/personas/bundles.go, .planning/technical-debt/README.md]
tags: [clarifications, sprint-9.0_persona_ecosystem, td-resolution, phantom-td-rows, archer-backup, td-015]
retrievals: 0
status: active
type: clarifications skill 2026-06-24
---

# Should archer-backup group-4 TD rows citing internal/verify/

## Decision

All group-4 TD rows from the `archer-backup` reviewer that cite `internal/verify/select.go` at lines ≥191 are phantom misattributions — the file is only 190 lines. The problem text "Defensive bundle/ recompute redundant with Install" actually describes code in `internal/personas/bundles.go InstallBundle`, which is the real TD-015 row (group-2, checked [x]). The sprint-plan Phase 5 gate explicitly recorded this as "harmless defense-in-depth on a network-bound path." All phantom rows (select.go:875, :1001, :1265, :1529, :1661, :1793, :1925, :2057) should be bulk-closed [x] — they are duplicate captures of the already-deferred TD-015 item. No concrete code action is needed.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/select.go
- internal/personas/bundles.go
- .planning/technical-debt/README.md
