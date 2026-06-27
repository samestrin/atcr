---
id: TD-0041
order: 41
section: '[2026-06-16] From Sprint: 3.4_scorecard-diagnostics-writer'
date: "2026-06-16"
group: "1"
status: deferred
severity: MEDIUM
file: internal/scorecard/store.go:145
category: error-handling
est_minutes: "5"
source: code-review
reviewers: dax
confidence: MEDIUM
has_review_cols: true
---

## Problem

Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; decodeRecord writes to injectable w then returns (Record,bool) — no error cascade)

## Fix

Log or return the error from fmt.Fprintf
