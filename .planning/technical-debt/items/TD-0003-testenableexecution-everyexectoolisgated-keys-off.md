---
id: TD-0003
order: 3
section: '[2026-06-26] From Sprint: epic-11.2'
date: "2026-06-26"
group: "1"
status: open
severity: LOW
file: internal/tools/exec_tools_test.go:49
category: UNDER_ENGINEERING
est_minutes: "30"
source: execute-epic-independent
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

TestEnableExecution_EveryExecToolIsGated keys off ExecutionTools() rather than handlers whose bodies reach runInSandbox, so a future in-package sandbox-reaching handler not added to ExecutionTools() escapes this invariant

## Fix

Assert any sandbox-reaching handler is registered only via registerExec, or document ExecutionTools() as the authoritative exec-tool registry the test relies on
