## Metadata

**Plan Type:** feature
**Last Modified:** 2026-06-13
**Created:** 2026-06-13
**Status:** Draft - Awaiting User Stories
**Plan Number:** 2.0
**Epic/Initiative:** Epic 2.0
**Priority:** High
**Assigned Team:** Backend
**Dependencies:** Epic 1.0, Epic 1.1
**Stakeholders:** Sam

## Plan Overview

**Plan Goal:** Transform single-shot pool reviewers into bounded agents that can explore the repository through read-only, path-jailed tools (read_file, grep, list_files) via OpenAI-compatible function calling. The Go engine owns the entire tool harness — no external agent framework, no SSH, no containers.

**Target Users:** Developers running atcr reviews who need reviewers to look beyond the payload to verify suspicions, find callers, and cite real evidence.

**Framework/Technology:** Go 1.25, OpenAI-compatible function-calling wire format, git worktree for snapshots

## Planning Deliverables

### User Stories

- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 5 stories

### Acceptance Criteria

- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/2.0_tool_using_reviewers/`

## Feature Analysis Summary

The current fanout engine invokes each reviewer agent as a single-shot chat completion: send prompt, get response, done. This means reviewers can only reason about what is in the payload — they hallucinate context they cannot see, miss whole bug classes (the caller that passes nil, the invariant broken two packages away), and small models suffer most because they have weaker recall and no way to look things up.

Epic 2.0 introduces a multi-turn agent loop where `InvokeDirect` becomes a turn loop: send messages + tool definitions, if the response contains `tool_calls` execute them locally and append `role:"tool"` results, repeat until the model returns a final message or a budget trips. Three per-agent budgets (max_turns, tool_budget_bytes, timeout_secs) prevent runaway cost. A path jail and head snapshot sandbox ensure read-only, in-bounds file access.

## Technical Planning Notes

- **New package `internal/tools`**: Tool definitions (JSON Schema), dispatcher, path jail, snapshot manager, transcript writer. All built with Go stdlib — no new third-party dependencies.
- **Engine extension**: `internal/fanout/engine.go` `invokeAgent` branches on `Agent.Tools` to run the tool loop instead of single-shot. A new `ChatCompleter` interface provides the `Chat` method alongside the existing `Completer`. Type assertion in `invokeAgent` selects single-shot vs. loop.
- **llmclient wire format**: `internal/llmclient/client.go` adds `Chat(ctx, inv, messages, tools)` with `tools` array in request, `tool_calls` in response, `role:tool` messages. Backward compatible — tools omitted for non-tool agents.
- **Registry activation**: `Tools`, `MaxTurns`, `ToolBudgetBytes` fields already exist in `AgentConfig` (parsed+validated, inert in 1.x). Epic 2.0 wires the engine to read them and applies defaults (max_turns=10 when tools=true). Provider function-calling capability is declared in the registry; there is no dynamic probing in v1.
- **Snapshot manager**: When `head` == HEAD and worktree is clean, use live worktree (fast path). Otherwise `git worktree add` at head, remove after run. Path jail resolves all paths relative to snapshot root; rejects absolute paths, `..`, out-of-root symlinks, `.git/` access.
- **Loop hygiene**: Identical repeated tool call → inject a nudge message once, then halt the loop and request the final review. Malformed tool-call JSON → return a tool error message (one retry), then proceed to final answer. Tool execution error → returned to the model as the tool result, never fatal to the agent.
- **Read-only enforcement**: Structural, not permissive — there is no write tool, and the harness opens files read-only. No shell execution, no network access, no MCP loopback.
- **Fallback agent tool inheritance**: Fallback agents inherit the effective tools setting of the lane invocation. A fallback may also be a non-tool agent (degrade is per-agent, not per-slot).
- **Persona template extension**: `PayloadContext` gains a `ToolsEnabled bool` field. Persona templates use `{{if .ToolsEnabled}}` conditional sections for tool-specific guidance. Tools widen *evidence gathering*, not *scope* — findings still target the changed range unless explicitly out-of-scope-flagged.
- **Result struct extension**: `fanout.Result` gains `Turns int`, `ToolCalls int`, `ToolBytes int64` fields to propagate loop counters to the artifact/status layer. `AgentStatus` gains `ToolsDegraded bool` field for the degrade path.

## Toolset (v1 — deliberately minimal)

| Tool | Signature | Limits |
|------|-----------|--------|
| `read_file` | `(path, start_line?, end_line?)` | per-call byte cap; line-numbered output |
| `grep` | `(pattern, glob?)` | regex via Go stdlib; match cap with truncation marker |
| `list_files` | `(dir?)` | depth-capped listing |

No write tools. No shell. No network. Additions (e.g., `git_log`, `blame`) wait for field evidence.

## Implementation Strategy

Build bottom-up in dependency order:

1. **Tool harness** — definitions, dispatcher, path jail, per-call caps, unit tests with no LLM.
2. **Snapshot manager** — live worktree fast path, temporary git worktree, cleanup, manifest recording.
3. **Agent loop** — multi-turn invoke with tool_calls handling, budgets, loop hygiene, degrade path; httptest-scripted integration tests.
4. **Accounting + artifacts** — transcript.jsonl writer, status.json counters, manifest stages entry.
5. **Persona updates** — tool-enabled guidance sections, evidence-citation rule.
6. **Registry activation** — flip reserved fields to active with validation.
7. **Docs** — registry.md, payload-modes.md, README.

Each step is independently testable. The tool harness and snapshot manager have zero LLM dependency. The agent loop is tested with scripted httptest mock providers. The full integration is tested end-to-end with a fixture repo.

## Recommended Packages

No new third-party dependencies. The epic constraint explicitly requires stdlib-only for the tool harness. All three tools (read_file, grep, list_files) are straightforward with `os`, `regexp`, `path/filepath`, `io`.

## Out of Scope

- Write tools, shell execution, test running (Epic 5.0).
- Skeptic/judge roles (Epics 3.0/4.0) — though they will reuse this loop.
- Dynamic capability probing of providers; MCP-based tool exposure to pool agents.
- Submodule content access (documented limitation in v1).

## Clarifications

- **Q: Why not reuse openclaw's harness?** A: Openclaw was used in the source system for its harness, but the preference is to encapsulate the capability in atcr's own standalone system — no SSH/container infrastructure, one binary. (2026-06-10)
- **Q: Stage placement?** A: Executes immediately after the 1.x epics, before adversarial verification (3.0), because skeptics need this loop to refute meaningfully. (2026-06-10)

## User Story Themes

1. **Agent Loop Execution**: As a reviewer agent with tools enabled, I can make multiple tool calls across turns to explore the repository, so that I can verify suspicions and cite real evidence in my findings.
2. **Budget Enforcement**: As an operator, I can set per-agent budgets (max_turns, tool_budget_bytes, timeout_secs) so that tool-using agents cannot run away with unbounded cost.
3. **Path Jail & Snapshot Sandbox**: As a security constraint, all tool file access must be confined to the repository snapshot at the resolved head SHA, with no escape via absolute paths, `..`, symlinks, or `.git/` access.
4. **Graceful Degradation**: As an operator with a mixed roster, tool-enabled and non-tool agents must coexist in one review; a non-tool-capable model with `tools: true` must degrade to single-shot with `tools_degraded: true` in status.json.
5. **Transcript & Accounting**: As an operator debugging a review, I can replay the full tool-call session from `transcript.jsonl` and see turns/tool_calls/tool_bytes counters in status.json.

## Planning Success Criteria

- A tool-enabled agent completes a multi-turn review: reads a file outside the payload, greps for callers, and produces findings citing that evidence.
- All three budgets (turns, tool bytes, timeout) trip cleanly with partial-success semantics; status.json records which budget tripped.
- Loop hygiene rules enforced: repeated tool call nudges, malformed JSON retries once, tool execution errors returned non-fatally to the model.
- Path jail rejects absolute paths, `..`, and out-of-root symlinks; `.git/` is unreadable.
- Read-only enforcement is structural (no write tool exists, files opened read-only, no shell/network access).
- Non-tool-capable model with `tools: true` degrades to single-shot with `tools_degraded: true` in status.json.
- Fallback agents inherit the effective tools setting; a fallback may be a non-tool agent (degrade is per-agent).
- `transcript.jsonl` replays a full session; tool results above the truncation cap are marked.
- Worktree snapshot created and cleaned up when head != HEAD; live worktree used when clean and equal.
- Mixed roster (tool and non-tool agents) runs in one review; reconcile consumes both identically.
- Persona `{{if .ToolsEnabled}}` conditionals render tool-specific guidance; scope rule: tools widen evidence gathering, not scope.
- No new third-party dependencies.
- docs/registry.md documents `tools`, `max_turns`, `tool_budget_bytes` as active; docs/payload-modes.md explains payload-as-starting-point semantics for tool agents.

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Small models use tools badly (thrash, loop, ignore results) | Loop hygiene rules (nudge on repeat, halt on malformed JSON), conservative default budgets (max_turns=10), per-agent `tools` opt-in |
| Provider variance in function-calling dialects | Strict lowest-common-denominator wire format (OpenAI tools/tool_calls/role:tool); litellm normalizes most providers; degrade path for the rest |
| Token cost explosion | Hard per-agent budgets; counters surfaced in status.json and report panel; docs set expectations (3–10x calls per tool agent) |
| Worktree management edge cases (dirty trees, submodules) | Fast path only when clean and head==HEAD; explicit tests for all jail escape vectors; submodules unreadable in v1 (documented) |

## Next Steps

1. `/find-documentation @.planning/plans/active/2.0_tool_using_reviewers/`
2. `/create-documentation @.planning/plans/active/2.0_tool_using_reviewers/`
3. `/create-user-stories @.planning/plans/active/2.0_tool_using_reviewers/`
4. `/create-acceptance-criteria @.planning/plans/active/2.0_tool_using_reviewers/`
5. `/design-sprint @.planning/plans/active/2.0_tool_using_reviewers/`
6. `/create-sprint @.planning/plans/active/2.0_tool_using_reviewers/`
