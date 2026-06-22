---
id: mem-2026-06-22-10dcd7
question: "When a TD fix description says \"Add a test asserting the chosen behavior,\" should the diff_smell hard/test_only gate block resolution?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/registry/config.go:529, internal/registry/executor_config_test.go]
tags: [td-clarification, td-only, testing, diff_smell, false-positive, resolve-td, test-only]
retrievals: 0
status: active
type: td-clarification
---

# When a TD fix description says "Add a test asserting the cho

## Decision

No — the diff_smell hard/test_only gate is a false positive when the TD row's Fix field explicitly calls for a test-only change. The gate correctly detects that no implementation changed, but that is the intended fix when the design decision is already made and documented in code. Example: TestExecutor_SystemPromptControlCharsAccepted (commit 2ea29f3) for config.go:529 — the fix was "Add a test asserting the chosen behavior" (that system_prompt allows control chars), so a test-only diff IS the correct artifact. The test serves as a regression guard. In resolve-td, override the gate and mark resolved when the Fix text is explicitly test-only.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/config.go:529
- internal/registry/executor_config_test.go
