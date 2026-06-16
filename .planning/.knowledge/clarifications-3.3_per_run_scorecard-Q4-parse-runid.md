---
id: mem-2026-06-15-030b7a
question: "Should run_id parsing in internal/scorecard be centralized into a single parseRunID(s) (time.Time, ok), or should the three strategies (runIDTime, monthFromRunID, monthsToScan) stay as-is?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/scorecard/aggregate.go, internal/scorecard/paths.go, internal/scorecard/store.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, scorecard, run-id-parsing, refactoring, security-guard]
retrievals: 0
status: active
type: clarifications
---

# Should run_id parsing in internal/scorecard be centralized i

## Decision

Keep the three run_id parsing strategies separate — do NOT unify them into a single parseRunID(s) (time.Time, ok). They share only a surface theme (run_id begins with an RFC3339-ish timestamp) but have genuinely divergent return contracts: runIDTime (aggregate.go:102) needs a full time.Time for --since window comparison (ts.Before(cutoff)); monthFromRunID (paths.go:58) needs only the "YYYY-MM" file stem for JSONL rotation and never parses a real timestamp; monthsToScan (store.go:149) needs a raw day-of-month slice (runID[8:10]), not a parsed time. Routing all three through (time.Time, ok) forces two of them to re-derive month/day, which is more code, not less. monthFromRunID's (string, error) signature is load-bearing across four callers (Append, FindByRunID, IsRunID, and tests TestStore_FindByRunID_InvalidFormat / TestStore_FindByRunID_RejectsInvalidMonth / TestIsRunIDTraversalSuffix) — three depend on the error, not an ok bool — so do not change it. The traversal-guard comment protects a real security invariant: monthFromRunID's runID[:7] prefix-only slice prevents a ../../../ suffix from bleeding into the filename; a time.Time-centric parser discards the framing that makes the guard self-evident. The atomicity comment (store.go) is about Append's single-Write O_APPEND guarantee, unrelated to parsing. The ONLY safe consolidation is extracting the shared RFC3339-prefix shape-check into a private helper, leaving all three public signatures and the guard/atomicity comments untouched. Note the validation regexes deliberately differ in strictness (aggregate.go rfc3339Prefix tolerates numeric offsets to avoid silently dropping --since records; paths.go monthRe validates only the YYYY-MM stem), so even the validation does not unify cleanly without re-litigating each function's tolerance.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/aggregate.go
- internal/scorecard/paths.go
- internal/scorecard/store.go
