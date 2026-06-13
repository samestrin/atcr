# User Story 5: Transcript & Accounting

**Plan:** [2.0: Tool-Using Reviewers](../plan.md)

## User Story

**As a** platform operator debugging a review produced by a tool-using agent
**I want** to replay the full tool-call session from `transcript.jsonl` and see live `turns`, `tool_calls`, and `tool_bytes` counters in `status.json`
**So that** I can understand what the agent saw, what it did, why it stopped, and diagnose prompt or evidence-gathering issues without rerunning the review

## Story Context

- **Background:** Epic 2.0 turns single-shot pool reviewers into multi-turn agents that explore the repository through `read_file`, `grep`, and `list_files` across several turns. When a finding looks wrong, a tool call looks excessive, or a budget trips unexpectedly, the operator has no way to reconstruct the session from the final review text alone — the reasoning is buried in the model's intermediate tool_calls and the truncated results it consumed. Without a per-turn transcript and live counters, operators are blind: they cannot distinguish "the agent never looked at the right file" from "the agent looked but the result was truncated above the cap" from "the agent was making progress and the byte budget tripped early."
- **Assumptions:**
  - The `raw/<agent>/` directory already exists per agent as part of the 1.x artifact layout; `transcript.jsonl` is added alongside any existing raw artifacts.
  - Operators are comfortable reading line-delimited JSON (one JSON object per line, one line per event) for replay and grep-ability.
  - The truncation cap for tool results recorded in the transcript is a fixed per-call value (aligned with the per-call byte cap already enforced by the tool harness); truncation is marked in-band on the recorded result, not as a separate event.
  - `status.json` already supports extensible fields per agent; the new counters are additive and backward-compatible with 1.x readers that ignore unknown fields.
  - `manifest.json` already has a `stages` section; Epic 2.0 adds a `"review"` entry listing agents that ran with tools enabled.
- **Constraints:**
  - Transcript writes must not block or fail the review on I/O error — log and continue; the transcript is best-effort observability, not part of the findings contract.
  - No new third-party dependencies — JSONL emission uses `encoding/json` and `os`.
  - Transcript format must be append-only per turn (no mid-file rewrites) so a crashed run still yields a usable partial transcript.
  - Sensitive content rules: transcript records tool results verbatim (truncated to cap), but the path jail already prevents `.git/` and out-of-root reads; no additional redaction layer is introduced in this story.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (Agent Loop Execution) — the loop must emit the events the transcript records; Story 2 (Budget Enforcement) — counters recorded in status.json include budget-tripped state |

## Success Criteria (SMART Format)

- **Specific:** Every tool-using agent run produces a `raw/<agent>/transcript.jsonl` file with one JSON object per line covering: outgoing `tool_calls` requests, incoming tool results (with truncation markers when above cap), and the final assistant message; `status.json` exposes `turns`, `tool_calls`, `tool_bytes` counters updated live; `manifest.json` `stages` includes a `"review"` entry listing agents that ran with `tools: true`.
- **Measurable:** 100% of integration-test agent runs (scripted httptest providers exercising multi-turn tool exchanges, budget trips, and the degrade path) produce a parseable `transcript.jsonl` that replays to the exact sequence of `Chat` requests and tool results observed by the engine, and `status.json` counters match the actual counts within the run.
- **Achievable:** Transcript emission is a writer that serializes events already in memory at each loop boundary; counters are the same values already tracked for budget enforcement (Story 2), just surfaced to the artifact layer.
- **Relevant:** Makes tool-using agents debuggable in production — the operator's primary complaint without this story is "I cannot tell what the agent saw." Directly addresses the plan's objective: "Enables debugging: what did the agent see? what did it do? why did it stop?"
- **Time-bound:** Delivered within the Epic 2.0 sprint sequence; required before any adversarial-verification stories (Epic 3.0) can rely on operators diagnosing skeptic-vs-reviewer disagreements.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [05-01](../acceptance-criteria/05-01-transcript-event-emission.md) | Transcript Event Emission | Unit + Integration |
| [05-02](../acceptance-criteria/05-02-transcript-durability-replay.md) | Transcript Durability and Replay | Unit + Integration |
| [05-03](../acceptance-criteria/05-03-live-status-counters.md) | Live Status Counters | Unit + Integration |
| [05-04](../acceptance-criteria/05-04-manifest-review-stage.md) | Manifest Review Stage Entry | Unit + Integration |

## Original Criteria Overview

1. `transcript.jsonl` is emitted per tool-using agent under `raw/<agent>/`, with one JSON object per line covering each turn's request `tool_calls`, each tool result (marked when truncated above the recorded cap), and the final assistant message.
2. The transcript is append-only per turn: a run interrupted mid-loop leaves a valid, replayable partial transcript.
3. `status.json` exposes `turns`, `tool_calls`, and `tool_bytes` counters per agent, updated live during the run and finalized on completion, degradation, or budget trip.
4. `manifest.json` `stages` includes a `"review"` entry that lists every agent that executed with `tools: true`, including agents that later degraded to single-shot.
5. Transcript I/O errors do not fail the review — errors are logged and the run continues; the transcript may be incomplete but the review result is unaffected.
6. A replay tool or test harness can reconstruct the exact `Chat` call sequence from the transcript and reproduce the engine's view of the session (modulo provider-side non-determinism).

## Technical Considerations

- **Implementation Notes:**
  - New package or subpackage `internal/tools/transcript` (or extend `internal/tools/`) owns the JSONL writer: `Open(path)`, `RecordToolCalls(turn, toolCalls)`, `RecordToolResults(turn, results)`, `RecordFinal(turn, message)`, `Close()`. Each method appends one JSON line with a stable schema: `{event, turn, ts, ...payload}`.
  - Truncation marker: when a tool result exceeds the per-call byte cap, the recorded content in the transcript is truncated to the cap and a `truncated: true` field (plus `original_bytes: int`) is set on that event's payload. The same truncated content is what the model received — the transcript is the operator's view of the model's view.
  - Writer is wrapped in a best-effort layer: if `Write` returns an error, log via the engine's structured logger and continue; do not propagate the error to the agent loop.
  - File opened with `O_CREATE|O_WRONLY|O_APPEND` so partial transcripts from crashed runs are well-formed JSONL up to the last successfully-written line.
  - `status.json` counters: `Turns int`, `ToolCalls int`, `ToolBytes int64` on `AgentStatus` (fields reserved in 1.x). Updated from the agent loop after each turn/tool execution using the same counters that drive budget enforcement (Story 2). Writer serializes the status file after each update so a live `atcr status` view sees progress.
  - `manifest.json` stages: a new `"review"` key under `stages` with a value that lists agent names that had `tools: true` effective for the run (including degraded agents). Written once at run finalization by the existing manifest builder, which already has the per-agent `Tools` and `ToolsDegraded` flags available.
  - Replay test helper: a `_test.go` function that reads `transcript.jsonl`, replays the event sequence against a recorded `Chat` trace, and asserts equivalence — used in integration tests to prove the transcript is a faithful record.
- **Integration Points:**
  - `internal/fanout/engine.go` — `invokeAgent` emits transcript events at each loop boundary (after `Chat` returns tool_calls, after each tool execution, after the final message).
  - `internal/fanout/result.go` — `AgentStatus` struct carries `Turns`, `ToolCalls`, `ToolBytes` (already reserved); transcript writer is invoked from the same code paths that update these counters.
  - `internal/artifacts/` (or equivalent status/manifest writer) — consumes the new `AgentStatus` fields for `status.json`; manifest builder reads effective `Tools` flag per agent for the `"review"` stage entry.
  - `internal/tools/` — transcript writer lives here alongside the tool harness, since it records tool-related events.
- **Data Requirements:**
  - `transcript.jsonl` event schema (one JSON object per line, fields per event type):
    - `{event: "tool_calls", turn: int, ts: RFC3339, tool_calls: [{id, name, arguments}]}`
    - `{event: "tool_result", turn: int, ts: RFC3339, tool_call_id: string, name: string, content: string, truncated: bool, original_bytes: int}`
    - `{event: "final", turn: int, ts: RFC3339, message: string}`
  - `status.json` per-agent schema gains: `turns: int`, `tool_calls: int`, `tool_bytes: int` (alongside the existing `tripped_budgets` from Story 2).
  - `manifest.json` `stages` gains: `review: {agents: [string], tools_enabled: [string], tools_degraded: [string]}` (or equivalent structured form).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Transcript file grows very large for long-running agents, bloating the `raw/` directory and slowing replay | Medium | Per-call byte cap already bounds individual tool results; add a per-transcript soft cap (e.g., 1 MB) with a `transcript_truncated: true` marker on the final event so the operator knows the record is incomplete |
| Transcript I/O stalls the agent loop on slow disks or network mounts | Low | Writes are small (one JSON line per event) and synchronous; wrap in a buffered writer with `Flush` per turn, not per event; I/O errors are non-fatal (log and continue) |
| Operator confusion between "truncated in transcript" and "truncated before being sent to the model" | Medium | Document clearly that the transcript records exactly what the model received; the `truncated` flag means the tool result was above cap and both the model and the transcript saw the truncated form |
| `manifest.json` `"review"` stage lists agents inconsistently between normal completion, degradation, and budget-tripped runs | Medium | Stage entry is derived from the effective `Tools` flag at invocation time, not the completion path; test all three completion paths explicitly |
| Replay test harness diverges from the actual engine's `Chat` call construction over time, giving false confidence in transcript fidelity | Low | Replay helper lives in the same package as the engine's loop and imports the same request-builder; any wire-format change (Story 1) updates both sides together |

---

**Created:** June 13, 2026
**Status:** AC Generated - Ready for Implementation
