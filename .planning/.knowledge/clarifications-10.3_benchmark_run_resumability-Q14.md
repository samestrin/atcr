---
id: mem-2026-06-25-9614d7
question: "JSONEq vs raw-bytes equality for byte-identical contract (AC3): is replacing JSONEq with assert.Equal(string(a), string(b)) a test-only fix?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark_run_resume_test.go, cmd/atcr/benchmark_run.go, cmd/atcr/benchmark_checkpoint.go]
tags: [clarifications, epic-10.3_benchmark_run_resumability, testing, byte-identity, json-comparison, AC3]
retrievals: 0
status: active
type: clarifications skill — epic 10.3_benchmark_run_resumability, 2026-06-25
---

# JSONEq vs raw-bytes equality for byte-identical contract (AC

## Decision

Yes, replacing JSONEq with raw-bytes assert.Equal is a test-only fix when the production code is already deterministic. JSONEq normalizes key order, whitespace, and number formatting — it cannot prove a byte-identical contract (e.g. AC3: resumed RunResult must be byte-identical to uninterrupted run). The fix: replace assert.JSONEq / testify.JSONEq with assert.Equal(t, string(jBaseline), string(jActual)) to compare raw marshaled bytes. No production code change is required if the production path is already deterministic (sorted accumulation, injected generatedAt, compact JSON marshaling). Pattern: if the AC says byte-identical, use raw-bytes equality in the test, not semantic equality.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark_run_resume_test.go
- cmd/atcr/benchmark_run.go
- cmd/atcr/benchmark_checkpoint.go
