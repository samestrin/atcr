---
id: TD-0049
order: 49
section: '[2026-06-14] From Sprint: 3.2_disagreement_radar'
date: "2026-06-14"
group: "4"
status: deferred
severity: MEDIUM
file: internal/reconcile/disagree.go:354
category: maintainability
est_minutes: "10"
source: code-review
reviewers: bruce
confidence: MEDIUM
has_review_cols: true
---

## Problem

Duplicated radar markdown rendering diverges (Deferred: Epic Plan 7.2)

## Fix

Extract shared writer or make reconcile use report package's escTrunc
