---
id: mem-2026-06-28-3dfa34
question: "Should pyparser's scanTripleQuotes/stripComment be made fully quote- and escape-aware (a tokenizer state-machine upgrade) in the PoC?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [internal/astgroup/parsers/src/pyparser/main.go, internal/astgroup/host_test.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, pyparser, quote-state-machine, heuristic, poc-scope]
retrievals: 0
status: active
type: clarifications
---

# Should pyparser's scanTripleQuotes/stripComment be made full

## Decision

No — the documented heuristic limitation is acceptable for the PoC. Do not add a full quote-state machine in 13.1.

Key facts:
1. The limitation is already documented in the production code: pyparser/main.go:72-78 (significantLines docstring) explicitly names the heuristic risk: "a \"\"\" embedded in a single/double-quoted string can still flip the state machine — PoC-grade, not a tokenizer." Lines 152-156 carry the same disclaimer for bracketDelta.
2. scanTripleQuotes at pyparser/main.go:175-199 is NOT a stub — it already tracks delimiters character-by-character for both \"\"\" and '''. The missing cases are edge cases: (a) embedded \"\"\" inside single-quoted strings, (b) # inside strings. These only trigger on adversarial patterns.
3. stripComment at pyparser/main.go:294-299 is a one-liner (strings.IndexByte on '#'). Making it string-aware requires threading quote-state, with blast radius on the structural hash.
4. The "regression fixtures the finding lists" phrase in the TD item describes NEW work that does not yet exist — deferring loses nothing currently guarded.
5. Epic binding: "The PoC Python parser is a best-effort heuristic, not a full grammar." Graceful degradation to proximity grouping handles edge-case misclassifications.
6. Hash stability on a later state-machine upgrade is inherent to PoC-to-production parser evolution — deferring does not worsen the problem.

Correct disposition: close as accepted PoC scope; no code change needed.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/pyparser/main.go
- internal/astgroup/host_test.go
