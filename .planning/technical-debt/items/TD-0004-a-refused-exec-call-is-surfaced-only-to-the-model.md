---
id: TD-0004
order: 4
section: '[2026-06-26] From Sprint: epic-11.1'
date: "2026-06-26"
group: U
status: open
severity: LOW
file: internal/tools/dispatch.go:175
category: OBSERVABILITY
est_minutes: "15"
source: execute-epic-independent
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

A refused exec call is surfaced only to the model as a tool result with no dispatcher-side log or metric, so an operator cannot see that a non-exec agent attempted run_tests/run_script

## Fix

Emit a Warn/Debug log or counter at the refusal point naming the tool and that eligibility was absent
