---
id: TD-0053
order: 53
section: '[2026-06-14] From Review: llmclient OpenAI-compatible tool handling'
date: "2026-06-14"
group: U
status: deferred
severity: LOW
file: internal/llmclient/chat.go:1
category: testing
est_minutes: "480"
source: review
reviewers: claude
confidence: LOW
has_review_cols: true
---

## Problem

No provider-conformance test matrix for the OpenAI-compatible surface. The client deliberately absorbs real wire divergence (string-encoded vs raw-object tool_call `arguments`, lenient finish_reason) but is exercised only against synthetic fixtures, so a regression against a specific provider's actual tool_call shape (OpenAI, litellm, Ollama, vLLM, Together) would not be caught. This is the robustness the official SDK is assumed to provide, achievable here without adopting it.

## Fix

Add a recorded-fixture conformance suite: capture a real `tool_calls` response from each target provider and assert the parser (`ToolCallArguments`, `chatToolResponse` decode, finish_reason handling) yields identical engine-facing results. NOTE: scope is a few days, not a quick-win — consider promoting to a standalone test-remediation plan rather than resolving inline.
