---
id: mem-2026-06-25-775d32
question: "Checkpoint full-rewrite IO trade-off: should O(n²) full-rewrite per case be accepted or switched to append-friendly format?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark_checkpoint.go, internal/fanout/resume.go]
tags: [clarifications, epic-10.3_benchmark_run_resumability, architecture, implementation, checkpoint]
retrievals: 0
status: active
type: clarifications skill — epic 10.3_benchmark_run_resumability, 2026-06-25
---

# Checkpoint full-rewrite IO trade-off: should O(n²) full-rew

## Decision

Accept the full-rewrite-per-case IO pattern. The O(n²) IO cost (saveCheckpoint at cmd/atcr/benchmark_checkpoint.go:98-104 marshals the entire runCheckpoint struct via json.Marshal and writes atomically via writeExportFile on every case) is negligible compared to LLM API latency — seconds per case vs. single-digit milliseconds for IO. For a 200-case suite with 3 reviewers the total bytes written across the run is ~15 MB. An append-friendly format (e.g., NDJSON: header line + one record per case) would require per-line parsing, index-based deduplication, and header/record separation on load, forfeiting the atomic temp+rename consistency guarantee without a measurable performance win. The fanout precedent (internal/fanout/resume.go:87-130) avoids O(n²) by writing one constant-size file per agent — a structural difference that doesn't translate to the single-file checkpoint design.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark_checkpoint.go
- internal/fanout/resume.go
