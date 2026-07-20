---
id: mem-2026-07-19-794182
question: "Auto-fix sandbox dispatch: should nil-backend host fallback require an explicit noSandbox bool, and where (gate vs dispatch)?"
created: 2026-07-19
last_retrieved: ""
sprints: [32.0_sandbox_execution_environment]
files: [cmd/atcr/autofix.go, internal/verify/autofix_exec.go]
tags: [clarifications, sprint-learning, 32.0_sandbox_execution_environment, architecture, fail-closed, defense-in-depth]
retrievals: 0
status: active
type: clarifications
---

# Auto-fix sandbox dispatch: should nil-backend host fallback 

## Decision

Not urgent, but if hardened: the fail-closed check belongs at the dispatch call site (e.g. runAutoFix), not the gate (validateAutoFixBackend). Reasoning: a gate that always passes enabled=true to its resolver can't itself produce the risky nil-without-opt-out case — that risk only appears if a future/test caller constructs the backend struct directly and bypasses the gate. Only a dispatch-side check (independent of why the field is nil) catches that. General pattern: when a "nil means fallback" invariant is enforced only by one call site always passing a hardcoded flag, the safety belongs at the point that actually branches on nil, not at the gate that happens to be the only current producer of a safe nil. cmd/atcr/autofix.go — gate hardcodes enabled=true at its resolver call; dispatch treats backend!=nil as its sole routing signal with no check of why it's nil.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/autofix.go
- internal/verify/autofix_exec.go
