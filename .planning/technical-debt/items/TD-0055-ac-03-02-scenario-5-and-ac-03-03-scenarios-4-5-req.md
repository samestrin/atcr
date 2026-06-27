---
id: TD-0055
order: 55
section: '[2026-06-13] From Sprint: 2.0_tool_using_reviewers'
date: "2026-06-13"
group: "5"
status: deferred
severity: LOW
file: internal/tools/snapshot.go
category: testing
est_minutes: "0"
source: execute-sprint
reviewers: execute-sprint
confidence: MEDIUM
has_review_cols: true
---

## Problem

AC 03-02 Scenario 5 and AC 03-03 Scenarios 4-5 require `manifest.json` `stages.review` to record `snapshot_mode` (live/worktree), `head_sha`, and `snapshot_worktree_path`. (intent_note: deferred per sprint-plan §2.5.A (manifest review-stage recording is Phase 5 work); Deferred to Epic Plan 2.1)

## Fix

when wiring `SnapshotFor` into the agent loop, record `snapshot_mode`/`head_sha`/`snapshot_worktree_path` into `internal/payload/manifest.go` review stage and add the manifest assertion tests from AC 03-02/03-03.
