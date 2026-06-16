---
id: mem-2026-06-15-cc591b
question: "Should internal/scorecard store reads be reworked into a streaming fold (O(groups) peak memory) instead of materializing records into a slice?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/scorecard/store.go, internal/scorecard/aggregate.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, performance, scorecard, jsonl]
retrievals: 0
status: active
type: clarifications
---

# Should internal/scorecard store reads be reworked into a str

## Decision

No. The sprint's recorded intent ("single-pass JSONL streaming, no load-entire-file-into-memory pattern", sprint-plan.md:696) is already satisfied: ReadRecords uses a bufio.NewReaderSize line reader (internal/scorecard/store.go:79-105) and never reads a whole file into one buffer. Materializing parsed records into a []Record slice is intentional and trivially cheap at the documented scale (~500 bytes/record, ~500KB per 1000 runs/month). The O(groups) streaming-fold (a ReadRecordsFunc callback variant, or folding aggregation into the scan) is a speculative optimization with no measurable benefit at this scale and would change the read API plus all four callers (ReadAll, FindByRunID, Aggregate, export) — it needs explicit scope sign-off and is out of scope absent real data-volume pressure. Distinct from any future "cost per verified finding" work.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/store.go
- internal/scorecard/aggregate.go
