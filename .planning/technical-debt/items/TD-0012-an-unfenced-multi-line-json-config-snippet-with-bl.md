---
id: TD-0012
order: 12
section: '[2026-06-22] From Sprint: epic-7.1'
date: "2026-06-22"
group: "1"
status: deferred
severity: MEDIUM
file: internal/verify/syntaxguard.go:130
category: EDGE_CASES
est_minutes: "30"
source: execute-epic-independent
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

An unfenced multi-line JSON/config snippet with block braces can still satisfy looksLikeGoCode and be parsed as Go, producing a spurious invalid_syntax flag on non-Go content (residual after heuristic hardening).

## Fix

Detect obviously non-Go brace content (JSON object / key:value lines) before treating block braces as a Go signal; deferred as a separate design refinement (unfenced non-Go fixes are rare; fenced non-Go is already handled). [Deferred 2026-06-22 to Epic Plan 7.5 syntax-guard-refinements per clarification]
