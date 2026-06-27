---
id: TD-0033
order: 33
section: '[2026-06-16] From Sprint: epic-3.5'
date: "2026-06-16"
group: U
status: deferred
severity: LOW
file: internal/stream/severity.go:20
category: OBSERVABILITY
est_minutes: "20"
source: execute-epic-independent
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

The canonical SeverityRank map carries a read-only-after-init invariant in a comment only, with no test guard preventing a future caller from mutating the shared map (which would race across concurrent fan-out agents). (Won't fix: structural guard — reconcile copy-on-init at merge.go:29-31 means no consumer writes stream.SeverityRank directly; grep confirms zero write sites across consumers. Snapshot test trips the over-simplification gate; Rank() accessor cascades to 14+ direct-lookup sites — both out of pure-consolidation scope.)

## Fix

Add a stream test that snapshots the map and asserts it is unchanged after consumers run, or wrap it behind a Rank(sev) accessor.
