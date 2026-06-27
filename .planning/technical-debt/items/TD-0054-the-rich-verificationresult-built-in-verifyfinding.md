---
id: TD-0054
order: 54
section: '[2026-06-14] From Sprint: 3.0_adversarial_verification'
date: "2026-06-14"
group: "7"
status: deferred
severity: LOW
file: internal/verify/pipeline.go:238
category: correctness
est_minutes: "120"
source: execute-sprint
reviewers: execute-sprint, claude
confidence: HIGH
has_review_cols: true
---

## Problem

The rich VerificationResult built in verifyFinding never populates TrippedBudgets (invokeSkeptic folds tripped budgets only into free-text Notes), base.Model is hard-coded to skeptics[0].Config.Model even when another skeptic produced the winning verdict, and the skip-already-verified rebuild (pipeline.go:199) re-synthesizes records from the compact on-disk block losing Model/DurationMs/TrippedBudgets — so verification.json's structured audit fields degrade or misattribute on multi-skeptic and no-op re-runs (extends the model-attribution gap of TD-011). Also: Model is empty on the no_eligible_skeptic / tool_harness_unavailable early-return paths. [intent: deferred per sprint-plan TD-011]

## Fix

Thread the tripped-budget slice and the winning skeptic's model up from invokeSkeptic into base.TrippedBudgets/base.Model (join models when multiple voters agree, mirroring joinSkeptics), and carry Model/DurationMs/TrippedBudgets forward for skipped findings instead of synthesizing a lossy record.
