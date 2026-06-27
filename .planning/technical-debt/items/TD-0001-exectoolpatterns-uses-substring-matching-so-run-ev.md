---
id: TD-0001
order: 1
section: '[2026-06-26] From Sprint: epic-11.2'
date: "2026-06-26"
group: "1"
status: open
severity: LOW
file: internal/tools/dispatch.go:123
category: REGRESSION_RISK
est_minutes: "30"
source: execute-epic-independent
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

execToolPatterns uses substring matching so "run"/"eval" reject legitimate read-only names (e.g. prune_* or *retrieval*); harmless today but constrains future read-only tool naming

## Fix

Match on _-split token boundaries instead of strings.Contains, or document the accepted false-positive set
