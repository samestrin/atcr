---
id: TD-0015
order: 15
section: '[2026-06-20] From Sprint: epic-5.2'
date: "2026-06-20"
group: U
status: deferred
severity: LOW
file: internal/cache/store.go:115
category: PERFORMANCE
est_minutes: "30"
source: execute-epic-cumulative
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

Eviction does a full ReadDir+Stat of the cache dir on every Put even when under the cap; O(n) per write scales poorly if the cache accumulates thousands of entries (Won't-fix 2026-06-21: scan runs serially under the store mutex and LLM calls dominate latency; no Epic 5.2 perf criterion requires O(1) Put — added state not justified at LOW)

## Fix

Maintain a running total-size counter and skip the directory scan when it is under the cap, or evict only on a periodic/threshold basis
