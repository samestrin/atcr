---
id: mem-2026-07-22-d5691b
question: "Test-only fix for a testing-category TD item flagged by resolve-td's non-overridable diff-smell gate (test_only hard verdict) — is a test-only diff correct when the TD item's own FIX is inherently test-scoped, or does it always need a production-code component?"
created: 2026-07-22
last_retrieved: ""
sprints: [33.0_final_documentation_sweep]
files: [internal/personas/remove.go, personas/personas.go, personas/community.go, internal/personas/remove_test.go]
tags: [clarifications, sprint-33.0_final_documentation_sweep, testing, Go, resolve-td, diff-smell-gate]
retrievals: 0
status: active
type: clarifications
---

# Test-only fix for a testing-category TD item flagged by reso

## Decision

The test-only fix is correct as implemented; no production-code change is warranted or possible. Example: `isBuiltin` (internal/personas/remove.go:47-50) is a lookup against `builtinSet`, populated exclusively from the hardcoded personas.Names() slice (personas/personas.go:20); CommunityNames() (personas/community.go:38-52) reads a structurally disjoint embedded directory that never feeds builtinSet — so "isBuiltin returns false for community personas" is a structural guarantee, not a runtime behavior that could regress. General pattern: when a TD item's Category is "testing" and its own PROBLEM/FIX text describes adding/relaxing a test assertion (not alleging a production defect), resolve-td's ADVERSARIAL-stage diff-smell gate flagging the resulting diff as test_only/NEEDS_REVIEW is expected behavior, not evidence the fix is wrong or incomplete — the gate is a categorical reward-hack signal, and for TD items that are correctly scoped as test-coverage-only, it structurally can never auto-approve them. Verify by reading the actual production code path the test exercises to confirm no production defect underlies the TD item before accepting the test-only diff.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/remove.go
- personas/personas.go
- personas/community.go
- internal/personas/remove_test.go
