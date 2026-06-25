---
id: mem-2026-06-25-eb2df9
question: "Should the diff-file ingestion primitive accept only loose unified diffs or also full git diff patches with diff --git headers?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [internal/payload/diff.go, internal/benchmark/testdata/suite-valid/case-01.diff, internal/benchmark/testdata/suite-valid/case-02.diff, internal/benchmark/benchmark.go]
tags: [clarifications, epic-10.1_diff_file_ingestion, architecture, scope, diff-ingestion, format-parsing]
retrievals: 0
status: active
type: clarifications
---

# Should the diff-file ingestion primitive accept only loose u

## Decision

Both formats (Option A). Benchmark fixtures use loose `--- a/` / `+++ b/` / `@@` format exclusively, so the primitive must handle that. Accepting git-format output too costs nothing: `internal/payload/diff.go:279` (`splitDiffByFile`) already splits on `diff --git` boundaries, so the dual-detect strategy (check for `diff --git` first, else fall back to `---/+++` pairs) reuses proven code. Rejecting git-format would break users who feed `git diff HEAD~1 > my.diff` to the pipeline.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/payload/diff.go
- internal/benchmark/testdata/suite-valid/case-01.diff
- internal/benchmark/testdata/suite-valid/case-02.diff
- internal/benchmark/benchmark.go
