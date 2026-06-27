---
id: TD-0025
order: 25
section: '[2026-06-18] From Sprint: epic-4.3'
date: "2026-06-18"
group: "1"
status: deferred
severity: LOW
file: internal/validation/validation.go:86
category: OVER_ENGINEERING
est_minutes: "15"
source: execute-epic-independent
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

Severity and Enum validators are shipped but wired to nothing (ParseSeverity/ValidFormat remain the live paths); they exist only to satisfy AC5/AC7 and future use (Won't-fix: intentionally public for AC5/AC7 per epic 4.3 clarifications; deletion breaks ACs, wire-in out of scope)

## Fix

Revisit and delete if no caller adopts them within a release, or wire them in where duplication can be removed
