---
id: TD-0026
order: 26
section: '[2026-06-18] From Sprint: epic-4.2'
date: "2026-06-18"
group: U
status: deferred
severity: LOW
file: internal/registry/attribution.go:55
category: INTEGRATION
est_minutes: "5"
source: execute-epic-independent
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

attribute() recurses and reconstructs the join even for a single-error errors.Join (which still satisfies Unwrap() []error), a small avoidable cost on the load-time validation path

## Fix

Optionally short-circuit when len(children)==1 if profiling ever flags it (WON'T-FIX 2026-06-18: trigger unmet — error-path only, never hit on normal load; no perf AC in epic 4.2)
