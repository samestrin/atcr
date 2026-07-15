---
id: mem-2026-07-15-82b575
question: "Is deleting a dead struct field from a _test.go file (touching no other file) a legitimate fix, or does the diff-smell gate's `test_only` flag mean it needs a broader change?"
created: 2026-07-15
last_retrieved: ""
sprints: []
files: [personas/community_test.go]
tags: [clarifications, epic-27.0_local_model_ollama_persona, testing, diff-smell]
retrievals: 0
status: active
type: clarifications
---

# Is deleting a dead struct field from a _test.go file (touchi

## Decision

A test-only diff is a false positive for the `test_only` diff-smell heuristic when the field being removed is pure dead metadata with zero reads anywhere in the codebase (verified via `grep -rn '\.Tier' personas/` returning no matches). The heuristic exists to catch reward-hacks (weakened assertions, stubbed bodies) — deleting genuinely dead test-helper fields and their literal initializers is a complete, correct fix entirely within the test file, not evidence the fix is incomplete. Confirmed via personas/community_test.go's `communityPersona` struct (Slug/VendorToken/Category only, no Tier) and git history (commit 2847bca5) showing a pure subtraction diff with zero assertion changes.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- personas/community_test.go
