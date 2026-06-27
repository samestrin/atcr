---
id: TD-0050
order: 50
section: '[2026-06-14] From Sprint: 3.2_disagreement_radar'
date: "2026-06-14"
group: U
status: deferred
severity: LOW
file: internal/reconcile/disagree.go:413
category: maintainability
est_minutes: "15"
source: code-review
reviewers: otto
confidence: MEDIUM
has_review_cols: true
---

## Problem

Redundant implementation of writeRadarSection (Deferred: .planning/epics/active/7.2_radar-renderer-consolidation.md)

## Fix

Remove duplicate function from internal/reconcile and use internal/report
