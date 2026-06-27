---
id: TD-0017
order: 17
section: '[2026-06-20] From Sprint: epic-5.0'
date: "2026-06-20"
group: U
status: deferred
severity: MEDIUM
file: internal/reconcile/reconcile.go:26
category: EDGE_CASES
est_minutes: "60"
source: execute-epic-independent
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

Validation root is hardcoded to "." at every call site, so "atcr reconcile <path>" for a review of another repo, or running from a non-repo-root CWD, falsely flags every finding as "file not found"

## Fix

Thread the reviewed repo root explicitly or add a --repo flag, applied consistently with the verify stage which uses the same "." convention (Deferred 2026-06-21: the narrow Root: os.Getwd() variant is a no-op — filepath.Abs(".") already equals the CWD, so it would not fix the non-repo-root / other-repo case; the real fix is to plumb the reviewed-repo path explicitly via a --repo flag threaded through the reconcile and verify call sites, est 60)
