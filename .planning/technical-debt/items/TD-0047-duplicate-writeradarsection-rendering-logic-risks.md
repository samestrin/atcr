---
id: TD-0047
order: 47
section: '[2026-06-14] From Sprint: 3.2_disagreement_radar'
date: "2026-06-14"
group: U
status: deferred
severity: LOW
file: internal/reconcile/disagree.go:280
category: security
est_minutes: "20"
source: code-review
reviewers: greta
confidence: MEDIUM
has_review_cols: true
---

## Problem

Duplicate writeRadarSection rendering logic risks divergent escaping across packages (Deferred: .planning/epics/active/7.2_radar-renderer-consolidation.md)

## Fix

Consolidate radar rendering into report package; reconcile should call report.writeRadarSection
