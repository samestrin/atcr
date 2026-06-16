---
id: mem-2026-06-16-4a9cf3
question: "Should Append() in the scorecard package receive an io.Writer param when adding the diagnostics writer to the scorecard package?"
created: 2026-06-16
last_retrieved: ""
sprints: []
files: [/Users/samestrin/Documents/GitHub/atcr/internal/scorecard/store.go, /Users/samestrin/Documents/GitHub/atcr/internal/scorecard/scorecard.go]
tags: [clarifications, epic-3.3_per_run_scorecard, scope, scorecard, Append, io.Writer, dead parameter]
retrievals: 0
status: active
type: clarifications
---

# Should Append() in the scorecard package receive an io.Write

## Decision

No — leave Append unchanged. Append (internal/scorecard/store.go:33-60) is pure data persistence: MkdirAll → json.Marshal → os.OpenFile → f.Write → return error. It emits zero diagnostic output of its own. All scorecard failure diagnostics live in Emit (internal/scorecard/scorecard.go:196-199), the sole Append caller, which writes them via fmt.Fprintf directly. AC2 requires "diagnostics route through the writer" — there are no diagnostics to route in Append. Adding an io.Writer param to Append would be a dead parameter and constitutes speculative scope creep against the "minimum code that solves the problem" principle.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- /Users/samestrin/Documents/GitHub/atcr/internal/scorecard/store.go
- /Users/samestrin/Documents/GitHub/atcr/internal/scorecard/scorecard.go
