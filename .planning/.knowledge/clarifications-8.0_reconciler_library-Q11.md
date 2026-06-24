---
id: mem-2026-06-23-f09706
question: "When diff_smell flags a test-only commit as test_only for a testing-category TD item whose FIX says \"Add table-driven tests\", is the fix accepted or does production code also need to change?"
created: 2026-06-23
last_retrieved: ""
sprints: [8.0_reconciler_library]
files: [reconcile/adapter/json/adapter.go, reconcile/adapter/json/adapter_test.go]
tags: [clarifications, sprint-8.0_reconciler_library, testing, diff-smell, false-positive, test-only, decode, json-adapter, table-driven-tests]
retrievals: 0
status: active
type: clarifications
---

# When diff_smell flags a test-only commit as test_only for a 

## Decision

Accepted as-is — test-only is the complete and intended fix. For reconcile/adapter/json adapter_test.go:138, the FIX explicitly said "Add table-driven decode tests"; the diff_smell test_only verdict is a false positive. Confirmed via go test: all four TestDecode_EdgeCases sub-cases (BOM, leading-whitespace+array, wrong-version-at-index-1) pass green against current production code with no production changes. The production Decode already handles BOM at adapter.go:65 (bytes.TrimPrefix with utf8BOM), leading whitespace at adapter.go:66 (bytes.TrimLeft), and version-at-index-1 at adapter.go:89-90 (indexed error format). The production behavior was already correct; the gap was test coverage only. Pattern: when a testing-category TD item's FIX says "Add tests", the test-only diff_smell verdict is always a false positive — the test-only commit IS the intended change.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/adapter/json/adapter.go
- reconcile/adapter/json/adapter_test.go
