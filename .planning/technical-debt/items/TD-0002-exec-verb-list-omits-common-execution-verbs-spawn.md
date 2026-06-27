---
id: TD-0002
order: 2
section: '[2026-06-26] From Sprint: epic-11.2'
date: "2026-06-26"
group: "1"
status: open
severity: LOW
file: internal/tools/dispatch.go:123
category: EDGE_CASES
est_minutes: "15"
source: execute-epic-independent
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

exec-verb list omits common execution verbs (spawn/invoke/launch/system/cmd/fork/popen/subprocess) so an exec-named handler using one slips past the name lint; mitigated since external handlers cannot reach the unexported execBackend

## Fix

Expand the fragment list or add a comment stating the true boundary is the unexported execBackend (name guard is defense-in-depth only)
