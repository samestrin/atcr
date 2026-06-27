---
id: TD-0043
order: 43
section: '[2026-06-16] From Sprint: 3.4_scorecard-diagnostics-writer'
date: "2026-06-16"
group: "1"
status: deferred
severity: LOW
file: internal/scorecard/store.go:194
category: performance
est_minutes: "2"
source: code-review
reviewers: otto
confidence: MEDIUM
has_review_cols: true
---

## Problem

Redundant call to diagWriter (Wontfix: FALSE POSITIVE — diagWriter is the required typed-nil guard for the nil-able opts.Writer interface, not redundant; removing it reintroduces the panic fixed by commit 476c6d1)

## Fix

Remove diagWriter call and use opts.Writer directly since ReadRecords already resolves it
