---
id: TD-0014
order: 14
section: '[2026-06-21] From Sprint: epic-6.0'
date: "2026-06-21"
group: "1"
status: deferred
severity: MEDIUM
file: internal/debate/debate.go:165
category: INTEGRATION
est_minutes: "60"
source: execute-epic-stage3
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

Gray-zone cluster merge/separate rulings are recorded in debate.json but not physically applied to findings.json; clusters still resolve via the existing adjudication path (Deferred: Epic Plan 6.1)

## Fix

Wire the judge cluster decision into the reconcile adjudication application so unattended runs auto-merge gray-zone clusters inline
