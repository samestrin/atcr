# Technical Debt Tracking

This file is a staging area for small technical debt items discovered during development. Items are triaged and moved to individual sprint documents in `sprints/` as they are prioritized.

## Stats

| Severity | Open | Deferred | Resolved |
|----------|------|----------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 1 | 0 |
| MEDIUM | 0 | 21 | 0 |
| LOW | 0 | 20 | 0 |


## Directory Structure

```
technical-debt/
├── README.md                    # This file (staging area)
├── CLAUDE.md                    # AI assistant guidelines
└── sprints/
    ├── active/                  # Currently being addressed
    ├── pending/                 # Prioritized, not yet started
    └── completed/               # Resolved items
```

## How to Use

1. **Small items**: Add to this README under "Staging Area" below
2. **Larger items**: Create a new document in `sprints/pending/`
3. **During sprint planning**: Move items from pending to active
4. **After resolution**: Move items from active to completed



### [2026-06-21] From Sprint: epic-6.0
### [2026-06-20] From Sprint: epic-5.2
### [2026-06-20] From Sprint: epic-5.0
### [2026-06-20] From Sprint: 4.7.1_backup-swap-hardening
### [2026-06-19] From Sprint: 4.7_idempotency
### [2026-06-19] From Sprint: 4.5_circuit_breaker
### [2026-06-18] From Sprint: epic-4.3
### [2026-06-18] From Sprint: epic-4.2
### [2026-06-18] From Sprint: epic-4.1.2
### [2026-06-17] From Sprint: epic-4.1
### [2026-06-17] From Sprint: 4.0_structured_logging
### [2026-06-16] From Sprint: 3.5_severity-rank-consolidation
### [2026-06-16] From Sprint: epic-3.5
### [2026-06-16] From Sprint: 3.4_scorecard-diagnostics-writer
### [2026-06-15] From Sprint: 3.3_per-run_scorecard
### [2026-06-14] From Sprint: 3.2_disagreement_radar
### [2026-06-14] From Sprint: 2.2_code_review_fanout_hardening
### [2026-06-14] From Review: llmclient OpenAI-compatible tool handling

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|------------|
| U | [/] | LOW | internal/llmclient/chat.go:1 | No provider-conformance test matrix for the OpenAI-compatible surface. The client deliberately absorbs real wire divergence (string-encoded vs raw-object tool_call `arguments`, lenient finish_reason) but is exercised only against synthetic fixtures, so a regression against a specific provider's actual tool_call shape (OpenAI, litellm, Ollama, vLLM, Together) would not be caught. This is the robustness the official SDK is assumed to provide, achievable here without adopting it. | Add a recorded-fixture conformance suite: capture a real `tool_calls` response from each target provider and assert the parser (`ToolCallArguments`, `chatToolResponse` decode, finish_reason handling) yields identical engine-facing results. NOTE: scope is a few days, not a quick-win — consider promoting to a standalone test-remediation plan rather than resolving inline. | testing | 480 | review | claude | LOW |

### [2026-06-14] From Sprint: 3.0_adversarial_verification
### [2026-06-13] From Sprint: 2.0_tool_using_reviewers
