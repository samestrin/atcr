# User Story 6: Payload Mode Selection

**Plan:** [1.0: atcr Core - Review Engine, Reconciler, and Skill](../plan.md)

## User Story

**As a** developer optimizing review quality per model capability
**I want** to configure different payload modes for different agents
**So that** small MoE models get expanded code blocks while frontier models get compact diffs

## Story Context

- **Background:** Different LLM models perform better with different input formats. Small MoE models produce better findings from real code (blocks mode) than from unified diffs. Frontier models handle diffs well and benefit from the token savings. Per-agent payload overrides allow fine-tuning.
- **Assumptions:** Developer understands the trade-offs between payload modes. Developer has a mix of model capabilities in their roster. Documentation clearly explains when to use each mode.
- **Constraints:** Default payload mode is blocks. Per-agent override in registry. Byte budgets with deterministic truncation. Truncation recorded in status.json — never silent. manifest.json records who saw what.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | M |
| **Dependencies** | Agent Configuration (US-02), Fan-out Engine |

## Success Criteria (SMART Format)

- **Specific:** Developer can configure default payload mode in .atcr/config.yaml and override per-agent in registry.yaml; manifest.json records each agent's payload mode
- **Measurable:** Each agent receives the correct payload format; manifest.json accurately reflects payload modes used; truncation recorded when byte budget exceeded
- **Achievable:** Uses git diff --function-context for blocks, unified diff for diff, full file content for files; byte budgets with deterministic truncation
- **Relevant:** Maximizes review quality by matching payload to model capability
- **Time-bound:** Implemented in task 4 (payload engine)

## Acceptance Criteria Overview

1. Three payload builders: diff (unified diff), blocks (--function-context expansion), files (full head content with changed regions marked)
2. Default payload mode: blocks (configurable in .atcr/config.yaml)
3. Per-agent override: payload field in registry.yaml agents section
4. manifest.json records default payload_mode and per-agent payload_mode(s)
5. Byte budget with deterministic truncation (drop whole files by size rank)
6. Truncation recorded in agent's status.json — never silent
7. blocks mode: git diff --function-context expands hunks to enclosing function/block
8. blocks fallback: when --function-context fails (no-brace languages, binary files), fall back to plain -U<n> context diff
9. files mode: full head-version content with changed regions marked
10. diff mode: standard unified diff (most compact)
11. Payload template vars available in persona prompts: {{.Payload}}, {{.PayloadMode}}, {{.FileCount}}, {{.BaseRef}}, {{.HeadRef}}
12. Per-payload scope rules in persona prompts (files mode surfaces pre-existing issues; reconcile flags findings outside changed ranges)
13. Documentation in docs/payload-modes.md explains when to use each mode

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/1.0_atcr_core/`_

## Technical Considerations

- **Implementation Notes:** 
  - Payload builder: internal/payload/builder.go — three builder functions
  - diff mode: git diff base..head (standard unified diff)
  - blocks mode: git diff --function-context base..head (expands hunks to enclosing function/block)
  - files mode: read full head-version content of changed files, mark changed regions
  - Byte budget: calculate total payload size, if over budget, drop whole files by size rank (smallest first)
  - Truncation recording: write to agent's status.json which files were dropped
  - Function-context fallback: when git diff --function-context fails (e.g., no braces in Python, binary files), fall back to plain -U<n> context diff per file
  - Template vars: text/template with {{.Payload}}, {{.PayloadMode}}, {{.FileCount}}, {{.BaseRef}}, {{.HeadRef}}, {{.AgentName}}

- **Integration Points:** 
  - Git (os/exec): git diff, git diff --function-context, git show (for files mode)
  - Fan-out engine: passes payload to LLM client per agent
  - Persona prompts: consume payload via template vars
  - manifest.json: records payload modes
  - status.json: records truncation

- **Data Requirements:** 
  - diff payload: unified diff text
  - blocks payload: expanded code blocks with real line numbers
  - files payload: full file content with changed regions marked (e.g., comments or markers)
  - status.json: {agent, status, payload_mode, files_dropped: [...], truncated: bool}
  - manifest.json: {payload_mode: "blocks", per_agent_payload: {agent1: "diff", agent2: "blocks"}}

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| blocks mode fails on languages without braces (Python, YAML) | Medium | Fallback to plain -U<n> context diff per file when --function-context fails |
| files mode token cost too high for large ranges | Medium | Byte budgets with truncation; documentation steers large ranges to diff mode |
| files mode produces out-of-change findings that pollute reconciliation | Medium | Per-payload scope rules in personas; reconcile annotates findings outside changed ranges |
| Truncation silently drops important files | High | Truncation always recorded in status.json; never silent |
| Function-context expansion produces huge payloads | Medium | Byte budget caps total size; truncation drops files if needed |

---

**Created:** June 10, 2026
**Status:** Draft - Awaiting Acceptance Criteria
