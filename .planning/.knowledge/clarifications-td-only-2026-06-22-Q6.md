---
id: mem-2026-06-22-0ddd82
question: "When diff_smell flags a TD fix as test_only but the TD fix description explicitly permitted a test-only assertion, should the item be accepted as resolved?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/reconcile/disagree_test.go, internal/report/disagree_test.go]
tags: [td-clarification, td-only, diff_smell, test-only, resolve-td, acceptance-criteria, escTrunc, radar-renderer]
retrievals: 0
status: active
type: clarifications skill 2026-06-22
---

# When diff_smell flags a TD fix as test_only but the TD fix d

## Decision

Yes — accept and mark [x]. The diff_smell gate is a heuristic that flags commits where no production code changed. When the TD fix description itself says "add an assertion" or "add a test" (or its alternative says "document that coverage lives in an existing test"), a test-only commit IS the correct implementation of the fix. The gate is overridden by the explicit fix description. For the specific case of internal/reconcile/disagree_test.go:171: commit 3b04d7d added 2 assertions in internal/report/disagree_test.go:181-182 (`assert.Contains(t, out, strings.Repeat("A", 497)+"...", "escTrunc caps at 500 total runes")` and the NotContains companion) which satisfy the TD's alternative "document that coverage lives in the existing report truncation test." Mark the row [x].

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/disagree_test.go
- internal/report/disagree_test.go
