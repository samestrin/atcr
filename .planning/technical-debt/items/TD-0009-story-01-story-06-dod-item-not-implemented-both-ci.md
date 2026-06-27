---
id: TD-0009
order: 9
section: '[2026-06-23] From Sprint: 8.0_reconciler_library'
date: "2026-06-23"
group: U
status: deferred
severity: MEDIUM
file: unknown:0
category: docs
est_minutes: "15"
source: code-review
reviewers: claude
confidence: MEDIUM
has_review_cols: true
---

## Problem

[Story 01 / Story 06] DoD item not implemented: both CI jobs (root ci.yml + reconcile-module PR-time job) must be marked as REQUIRED status checks on the main branch-protection rule. The CI workflow deliverables they depend on are all present and verified; only the protection-rule toggle is unset. The two story-level [ ] boxes (AC 01-06, AC 06-02) are the same single external action. (intent_note: deferred per sprint-plan Final Phase / dod-completion-summary.md (external repo-admin action))

## Fix

Configure branch protection in GitHub repo Settings -> Branches: add the root CI job and the reconcile-module PR-time job as required status checks. External repo-admin UI action (post-merge), not a source-tree change; documented deferred in dod-completion-summary.md.
