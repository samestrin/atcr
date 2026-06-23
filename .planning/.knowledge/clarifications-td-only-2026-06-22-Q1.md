---
id: mem-2026-06-22-c5103b
question: "Should looksLikeNonGoBraces in syntaxguard.go be extended to detect unquoted-key config (YAML/TOML) as a non-Go signal?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/verify/syntaxguard.go, internal/verify/syntaxguard_test.go]
tags: [td-clarification, td-only, syntaxguard, looksLikeNonGoBraces, epic-7.5, conservative-recall, false-positive]
retrievals: 0
status: active
type: clarifications skill 2026-06-22
---

# Should looksLikeNonGoBraces in syntaxguard.go be extended to

## Decision

No production code change needed. Epic 7.5 deliberately anchors looksLikeNonGoBraces on QUOTED keys only (jsonKeyLineRe: `^\s*"(?:[^"\\]|\\.)*"\s:`). Unquoted bare `ident:` is intentionally excluded because Go uses it in struct literals, map entries, labels, and case arms — adding detection for unquoted keys would risk misclassifying valid Go as non-Go. The residual false positive (unfenced YAML/TOML with unquoted keys + block braces flagged invalid_syntax) is the accepted narrow limitation under the 7.5 conservative-recall policy. The correct fix is a comment in syntaxguard_test.go noting this boundary, not a production change. See internal/verify/syntaxguard.go:59-66 (jsonKeyLineRe) and :249-251 (looksLikeNonGoBraces).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/syntaxguard.go
- internal/verify/syntaxguard_test.go
