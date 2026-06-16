---
id: mem-2026-06-15-d04868
question: "Should run_id parsing be centralized into a single parseRunID function in internal/scorecard, or should the three existing strategies (runIDTime in aggregate.go, monthFromRunID in paths.go, monthsToScan day-slicing in store.go) stay as-is?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/scorecard/aggregate.go, internal/scorecard/paths.go, internal/scorecard/store.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, refactoring, scorecard, run_id]
retrievals: 0
status: active
type: clarifications skill, sprint 3.3_per_run_scorecard, 2026-06-15
---

# Should run_id parsing be centralized into a single parseRunI

## Decision

Keep the three strategies as-is. The three functions solve distinct sub-problems at different layers and are not duplicates: runIDTime (aggregate.go:173-183) extracts a full time.Time via RFC3339 regex for --since window comparisons; monthFromRunID (paths.go:57-63) extracts only the YYYY-MM 7-char prefix via simple slice + monthRe for filename routing; monthsToScan (store.go:151-168) additionally slices runID[8:10] for the day digit for JSONL file boundary detection. Changing monthFromRunID's error-returning signature would break Append (store.go:33), FindByRunID (store.go:106), IsRunID (paths.go:43), and their tests for zero functional gain. If future consolidation into a shared parseRunID is desired, scope it as a dedicated TD cleanup sprint only after adding IsRunID coverage at paths.go:40-46 (currently 0.0%).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/aggregate.go
- internal/scorecard/paths.go
- internal/scorecard/store.go
