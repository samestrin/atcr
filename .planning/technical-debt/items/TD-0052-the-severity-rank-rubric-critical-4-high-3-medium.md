---
id: TD-0052
order: 52
section: '[2026-06-14] From Sprint: 2.2_code_review_fanout_hardening'
date: "2026-06-14"
group: U
status: deferred
severity: MEDIUM
file: internal/fanout/postprocess.go:14
category: maintainability
est_minutes: "120"
source: code-review
reviewers: claude
confidence: MEDIUM
has_review_cols: true
---

## Problem

The severity-rank rubric {CRITICAL:4,HIGH:3,MEDIUM:2,LOW:1} is independently redefined in fanout/postprocess.go:17, reconcile/merge.go, verify, report, plus a set-form copy reviewSeverities in registry/config.go. postprocess looks up severityRank[strings.ToUpper(...)] while reconcile looks up the raw value, so a future severity change or non-canonical casing silently desyncs fan-out truncation from reconcile merging. The postprocess copy was newly added by Epic 2.2. (disagreement: LOW vs MEDIUM) (Deferred: Epic Plan 3.5)

## Fix

Extract a single canonical severity package (or export from internal/stream) exposing the ordered rank map plus normalizeSeverity, and have registry/fanout/reconcile/verify/report consume it. Verify by deleting the local maps and confirming the suite passes with one source of truth.
