---
id: TD-0005
order: 5
section: '[2026-06-26] From Sprint: epic-11.1'
date: "2026-06-26"
group: U
status: open
severity: LOW
file: internal/tools/exec_tools.go:66
category: SECURITY
est_minutes: "30"
source: execute-epic-independent
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

WithExecEligibility is exported package-wide so any package importing tools (not just fanout.loop and verify.evidence) can grant eligibility=true, widening the trust surface the structural gate aims to narrow

## Fix

Keep exported but document the closed set of authorized callers and add a test/lint asserting only fanout and verify reference it
