---
id: mem-2026-07-19-1e233d
question: "Hard \"test_only\" diff_smell gate flags on TD items whose FIX text is entirely test rows/assertions — resolved or incomplete? (3rd/4th occurrence)"
created: 2026-07-19
last_retrieved: ""
sprints: [32.0_sandbox_execution_environment]
files: [internal/registry/sandbox_test.go, internal/registry/sandbox.go]
tags: [clarifications, sprint-32.0_sandbox_execution_environment, process, resolve-td, diff_smell, testing]
retrievals: 0
status: active
type: clarifications
---

# Hard "test_only" diff_smell gate flags on TD items whose FIX

## Decision

Confirmed pattern (see also the two earlier sprint-32.0 knowledge entries on this exact question): when a TD row's FIX text asks only for new test coverage (new table rows, or strengthened assertions like require.Contains replacing bare require.Error), a test-only committed diff that implements that FIX verbatim is the complete, legitimate fix. The resolve-td diff_smell gate's hard "test_only" flag is a blanket, non-overridable-by-self-review heuristic with no exception for test-authoring TD rows — it will reliably false-positive on this class of item, and a human/clarification pass is the expected, designed-for way to unblock it, not a signal of an incomplete fix. Verification checklist before confirming: (1) TD row's FIX column has no code-change verb, (2) the new test rows/assertions genuinely exercise the specific production branch/error message named in the PROBLEM column (not vacuous), (3) git show on the commit confirms zero production-file changes.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/sandbox_test.go
- internal/registry/sandbox.go
