---
id: TD-0007
order: 7
section: '[2026-06-25] From Sprint: epic-11.0'
date: "2026-06-25"
group: U
status: deferred
severity: LOW
file: internal/tools/exec_tools.go:69
category: SECURITY
est_minutes: "30"
source: execute-epic-stage3
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

Execution tools are gated per-agent only at the definition level (wireToolDefs); the shared per-run dispatcher will execute a run_tests/run_script call from any agent once EnableExecution is wired. The sandbox isolates every run identically so this is not a containment gap, but a non-designated agent could still incur execution cost. (Deferred: .planning/epics/active/11.1_dispatcher-structural-gating.md — exec_tools.go:69 is a data struct, not a gating point; the offering-layer gate is already structural, and a runtime per-call guard is the multi-file change scoped to Epic 11.1)

## Fix

Thread agent exec-eligibility into the dispatcher (or add a per-call guard) so only designated agents execute, for precise cost attribution.
