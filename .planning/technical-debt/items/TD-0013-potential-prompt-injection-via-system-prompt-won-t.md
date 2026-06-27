---
id: TD-0013
order: 13
section: '[2026-06-22] From Sprint: 7.0.1_executor_model_configuration'
date: "2026-06-22"
group: "7"
status: deferred
severity: LOW
file: internal/registry/config.go:524
category: security
est_minutes: "15"
source: code-review
reviewers: otto
confidence: MEDIUM
has_review_cols: true
---

## Problem

Potential prompt injection via system_prompt (Won't-fix: config.go:531–534 explicitly documents that control chars are intentionally NOT rejected in system_prompt; the --- delimiter added at executor.go:225 eliminates the CRLF metadata-forgery surface; otto's fix conflicts with the documented design decision)

## Fix

Add control character validation to SystemPrompt
