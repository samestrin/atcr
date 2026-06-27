---
id: TD-0016
order: 16
section: '[2026-06-20] From Sprint: epic-5.0'
date: "2026-06-20"
group: U
status: deferred
severity: LOW
file: internal/report/render.go:317
category: CROSS_CUTTING
est_minutes: "15"
source: execute-epic-cumulative
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

The "File not found" warning format string is duplicated across internal/reconcile/emit.go (writeFindingsList) and internal/report/render.go (writePathWarning) in separate packages

## Fix

Extract a shared constant/helper only if a common low-level rendering package emerges; a cross-package dependency is not justified for one format string today (Won't-fix 2026-06-21: two independent format strings across packages with no shared dependency; the recorded fix confirms extraction is not justified at this scope)
