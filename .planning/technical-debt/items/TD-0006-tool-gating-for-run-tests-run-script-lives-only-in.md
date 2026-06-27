---
id: TD-0006
order: 6
section: '[2026-06-26] From Sprint: 11.0_executing_reviewers'
date: "2026-06-26"
group: "3"
status: deferred
severity: MEDIUM
file: internal/tools/dispatch.go:146
category: security
est_minutes: "120"
source: code-review
reviewers: claude
confidence: MEDIUM
has_review_cols: true
---

## Problem

Tool gating for run_tests/run_script lives only in fanout.wireToolDefs (what the model is TOLD about); Dispatcher.Execute looks up d.handlers[name] with no check that the call was offered to the calling agent, and EnableExecution registers the exec handlers on the single dispatcher shared by the whole pool. The read-only guarantee for non-exec agents is therefore advisory, not structural — if any future caller enables exec non-uniformly across agents sharing one dispatcher, a non-exec agent could invoke run_script by simply naming it. No live exploit today: the sole exec caller, verify, sets exec uniformly for all skeptics. (Deferred: Epic Plan 11.1)

## Fix

Pass the agent's allowed tool set (or Exec flag) into Execute and reject any call whose name was not offered to this agent, or gate the exec handlers behind a per-call capability rather than a globally-registered handler. Verify with a test where a non-exec agent emits a run_script tool_call and asserts it is refused.
