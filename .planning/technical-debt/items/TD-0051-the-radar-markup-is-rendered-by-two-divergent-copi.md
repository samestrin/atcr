---
id: TD-0051
order: 51
section: '[2026-06-14] From Sprint: 3.2_disagreement_radar'
date: "2026-06-14"
group: "5"
status: deferred
severity: MEDIUM
file: internal/report/disagree.go:47
category: correctness
est_minutes: "60"
source: code-review
reviewers: bruce, claude
confidence: HIGH
has_review_cols: true
---

## Problem

The radar markup is rendered by two divergent copies: writeRadarSection + formatScore in internal/report/disagree.go and a structurally different copy in internal/reconcile/disagree.go:389. They are intentionally not identical (report copy truncates via escTrunc + uses joinReviewers/reviewerOrUnknown; reconcile copy uses uncapped esc + joinOrNone), so a future markup change (new field, reordered bullets) must be made in both or the live `atcr report` radar and the archival reconciled/report.md silently drift. (Deferred: .planning/epics/active/7.2_radar-renderer-consolidation.md)

## Fix

Extract one shared item renderer parameterized by a truncate-vs-verbatim flag and heading prefix; have both call sites delegate. Add a test that diffs the rendered markup of both paths on the same DisagreementsFile asserting the only intended difference is truncation.
