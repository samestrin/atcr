---
id: mem-2026-07-21-5cc5de
question: "Does a resolve-td diff_smell test-only flag always mean a missing production fix?"
created: 2026-07-21
last_retrieved: ""
sprints: [32.3_sandbox_ephemeral_copy_overlay]
files: [internal/verify/sandboxvalidate_test.go, internal/tools/exec_tools.go]
tags: [clarifications, sprint-32.3_sandbox_ephemeral_copy_overlay, process, resolve-td, testing, go]
retrievals: 0
status: active
type: clarifications
---

# Does a resolve-td diff_smell test-only flag always mean a mi

## Decision

No — a `/resolve-td` adversarial gate that flags a fix as NEEDS_REVIEW purely because the diff touches only test files (no production code) is a blunt structural heuristic; it cannot distinguish a reward-hack (weakened/vacuous test papering over a missing fix) from a legitimate test-only proof of behavior that Go's language semantics already guarantee (e.g. an unset struct bool field is its zero value `false`). When the TD item's actual claim is "prove property X holds for callers that don't opt in," and X is guaranteed by zero-value semantics rather than by any function's logic, a test that constructs the same struct literal pattern real callers use (without touching production code) is the correct and sufficient fix — introducing a new wrapper/helper function purely to give the gate a production-code diff to look at is manufacturing unneeded abstraction, and can actively violate an unrelated scope guard (e.g. "leave this call site's construction pattern untouched, it's the read-only control group for this feature"). Treat the diff_smell flag as a prompt for human/reviewer judgment on THIS specific case, not as evidence a production fix must exist.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/sandboxvalidate_test.go
- internal/tools/exec_tools.go
