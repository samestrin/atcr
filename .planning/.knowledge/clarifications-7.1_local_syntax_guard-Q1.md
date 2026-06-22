---
id: mem-2026-06-22-a2b0f8
question: "Should extractFencedCode in syntaxguard validate all markdown fences or just the first?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/verify/syntaxguard.go]
tags: [clarifications, epic-7.1_local_syntax_guard, architecture, scope, syntaxguard, conservative-recall]
retrievals: 0
status: active
type: clarifications skill — epic 7.1_local_syntax_guard
---

# Should extractFencedCode in syntaxguard validate all markdow

## Decision

Single-fence validation is an accepted limitation under the conservative-recall policy. The guard is calibrated to avoid false positives over false negatives; extending to multi-fence scanning changes that risk balance without a recorded decision authorizing it. A non-Go fence followed by a broken Go fence is a known false-negative gap. The correct resolution is a code comment on extractFencedCode documenting this, not a behavioral change.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/syntaxguard.go
