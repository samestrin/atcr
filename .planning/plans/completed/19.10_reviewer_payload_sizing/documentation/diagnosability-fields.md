# Diagnosability Fields (F8)

**Priority: IMPORTANT**

## Overview

Plan 19.10 extends `summary.json`/`AgentStatus` with per-agent fields that make payload sizing and degradation visible: effective input budget, resolved model window, reserved output tokens, chunk count, and degradation action taken.

> Source: [plan.md](../plan.md):Objectives:F8, [original-requirements.md](../original-requirements.md):Requirements:F8

## Key Concepts

- **Extend `AgentStatus` in `internal/fanout/status.go`.** The existing struct already follows an additive/omitempty discipline so old `status.json`/`summary.json` files remain byte-identical when new fields are unused. New fields must follow the same pattern (pointers or `omitempty`, never required fields).

  > Source: `internal/fanout/status.go:286-348`, codebase-discovery.json:architecture_notes:4

- **Suggested new fields.**

  | Field | Meaning |
  |-------|---------|
  | `effective_budget` | Effective input budget in bytes (after output reservation) |
  | `resolved_window` | Resolved context-window tokens for the model |
  | `reserved_output_tokens` | Output cap reserved (e.g., 8192) |
  | `chunk_count` | Number of chunks the diff was split into |
  | `degradation_action` | Action taken: `chunk`, `truncate`, `fallback`, `fail`, or empty |

  > Source: [original-requirements.md](../original-requirements.md):Requirements:F8

- **Assemble in `statusFor` / `writePool`.** `internal/fanout/artifacts.go:275` `statusFor` derives `AgentStatus` from a `Result`; `internal/fanout/artifacts.go:93-121` `writePool` assembles the per-agent statuses into `PoolSummary`. (`statusFor` lives in `artifacts.go`, not `status.go`; only the `AgentStatus` struct definition is in `internal/fanout/status.go:286`.)

  > Source: `internal/fanout/artifacts.go:275` (`statusFor`) / `internal/fanout/artifacts.go:93-121` (`writePool`)

- **Follow the non-silent degradation pattern.** Like `Truncation{Truncated, FilesDropped, AllDropped}`, the degradation action is always recorded explicitly, never inferred from logs alone.

  > Source: codebase-discovery.json:existing_patterns:Non-silent degradation record

## Implementation Guidance

- Add the new fields to `AgentStatus` with appropriate JSON tags and `omitempty`.

- Populate them in `statusFor` from the per-agent sizing result and any fallback/truncation state.

- Ensure `PoolSummary` carries the extended statuses unchanged (it embeds `[]AgentStatus`).

- Add AC8 coverage that `summary.json` records the new fields.

  > Source: [original-requirements.md](../original-requirements.md):Acceptance Criteria:AC8

## Related Documentation

- [Per-Agent Budget & Chunking](per-agent-budget-and-chunking.md) — produces the values to record (F2/F3)
- [on_overflow Policy](on-overflow-policy.md) — supplies the `degradation_action` value (F4)
- [Fallback Provenance](fallback-provenance.md) — supplies fallback substitution to record (F5)
- `internal/fanout/status.go` — `AgentStatus`
- `internal/fanout/artifacts.go` — `PoolSummary` / `writePool`
- `codebase-discovery.json` — discovery findings for F8
