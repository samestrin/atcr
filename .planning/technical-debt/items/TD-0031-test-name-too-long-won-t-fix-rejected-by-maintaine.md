---
id: TD-0031
order: 31
section: '[2026-06-16] From Sprint: 3.5_severity-rank-consolidation'
date: "2026-06-16"
group: "4"
status: deferred
severity: LOW
file: internal/reconcile/severity_consolidation_test.go:30
category: maintainancy
est_minutes: "2"
source: code-review
reviewers: Reviewer
confidence: MEDIUM
has_review_cols: true
---

## Problem

Test name too long (Won't fix: rejected by maintainer — breaks file's TestSubject_Behavior naming convention)

## Fix

Shorten to TestGrayZoneNormalizesMixedCase
