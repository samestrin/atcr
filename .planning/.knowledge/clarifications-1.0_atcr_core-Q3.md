---
id: mem-2026-06-11-b408a9
question: "How does gitrange classify git rev-parse --verify failures, and are env errors reachable at resolveRef?"
created: 2026-06-11
last_retrieved: ""
sprints: [1.0_atcr_core]
files: [internal/gitrange/resolver.go, internal/gitrange/resolver_test.go]
tags: []
retrievals: 0
status: active
type: project
---

# How does gitrange classify git rev-parse --verify failures, 

## Decision

resolveRef deliberately collapses both `err != nil` and empty stdout from `git rev-parse --verify --quiet` into ErrInvalidRef (internal/gitrange/resolver.go:209-212), because the command is a ref-validation probe — a bad ref is the expected, well-defined failure. Do NOT "fix" this to wrap the raw git error; doing so breaks TestResolve_InvalidRef, _LeadingDashRefRejected, and _ExplicitRequiresBoth (resolver_test.go:144-152,174-181,80-87). Genuine environment errors are already classified upstream inside run(): context-cancel -> ctx.Err() (resolver.go:168-170), not-a-repo -> ErrNotARepository (resolver.go:172-173); not-a-repo is additionally shadowed by the earlier isShallow() probe (resolver.go:72-76,194-201). No reachable mis-classified env error exists at resolveRef.</answer>
<parameter name="tags">clarifications, sprint-1.0_atcr_core, gitrange, error-classification, correctness, testing

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/gitrange/resolver.go
- internal/gitrange/resolver_test.go
