---
id: TD-0008
order: 8
section: '[2026-06-23] From Sprint: 8.0_reconciler_library'
date: "2026-06-23"
group: "2"
status: deferred
severity: HIGH
file: internal/reconcile/discover.go:25
category: correctness
est_minutes: "0"
source: execute-sprint
reviewers: execute-sprint
confidence: MEDIUM
has_review_cols: true
---

## Problem

`internal/reconcile` keeps its discovery `Source` (Name + `[]stream.Finding` + Skipped + SkippedFiles); the library now defines a public `Source` (Name + `[]Finding`)

## Fix

moved `Reconcile` takes the library `Source`; discovery output (`discover.Source`) is converted to `reconcile.Source` in the adapter/discovery layer
