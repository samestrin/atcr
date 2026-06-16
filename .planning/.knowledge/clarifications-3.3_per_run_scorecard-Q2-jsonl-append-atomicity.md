---
id: mem-2026-06-16-19e9c2
question: "Should the scorecard JSONL store batch writes for performance, or is one Write per record required?"
created: 2026-06-16
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/scorecard/store.go, internal/tools/transcript.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, scorecard, jsonl, concurrency, atomicity]
retrievals: 0
status: active
type: clarifications
---

# Should the scorecard JSONL store batch writes for performanc

## Decision

One Write per record is required; batching is deliberately rejected (won't-fix). The atomicity invariant (internal/scorecard/store.go:23-31): a write() to an O_APPEND regular file atomically seeks to EOF and writes contiguously, and that guarantee is per-write() independent of record size. Batching multiple records through one buffered flush coalesces them into a single larger write whose contiguity vs. other writers is no longer guaranteed, tearing lines under concurrent reconcile runs. No batching approach preserves per-record atomicity without defeating the purpose of batching. The implementation matches the doc: one json.Marshal + append newline, then exactly one f.Write per call on a file opened O_CREATE|O_WRONLY|O_APPEND. Mirrors the internal/tools/transcript.go pattern. A portability caveat for non-POSIX append semantics is tracked separately as TD-004.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/store.go
- internal/tools/transcript.go
