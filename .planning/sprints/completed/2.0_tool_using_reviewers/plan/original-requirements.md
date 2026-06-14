# Original Requirements

**Date:** 2026-06-13
**Arguments:** @.planning/epics/active/2.0_tool_using_reviewers.md
**Target:** .planning/epics/active/2.0_tool_using_reviewers.md

## Purpose

This file preserves the original input to `/init-plan` verbatim. It is the source of truth for what was requested, before any analysis, decomposition, or interpretation.

---

# Epic Plan 2.0: Tool-Using Reviewers

**Estimated Durations**: 3-4 weeks

## Objective

Turn the pool reviewers from single-shot prompted calls into bounded agents: each reviewer can explore the repository through read-only, path-jailed tools (`read_file`, `grep`, `list_files`) exposed via OpenAI-compatible function calling, with the Go engine owning the entire tool harness. The payload becomes the starting point of a review, not the universe.

## Context

In the source system, repository access for reviewers existed only in openclaw mode — the remote container provided a tool-calling harness and filesystem access (the LargeDiff workflow: "run git stat, pick 10 files, 15-tool-call limit"). That backend was dropped from atcr deliberately. This epic rebuilds the capability as a fully standalone, in-binary harness: no SSH, no containers, no external agent framework. The invoke loop in `fanout` grows from one request/response into a multi-turn loop; everything else in the 1.0 spine (findings contract, reconciler, artifacts, lanes, budgets) is reused unchanged.

## Problem Statement

Single-shot reviewers can only reason about what is in the payload:

1. They hallucinate context they cannot see — phantom APIs, wrong line numbers, invented callers.
2. Whole bug classes are unreachable: the caller that passes nil, the invariant broken two packages away, the config that no longer matches.
3. Small models suffer most — they have weaker recall and no way to compensate by looking things up.

## Proposed Solution

### Agent loop (engine)

- `InvokeDirect` becomes a turn loop: send messages + tool definitions → if the response contains `tool_calls`, execute them locally, append `role:"tool"` results, repeat → stop when the model returns a final message or a budget trips.
- Budgets (all per-agent, registry-driven, reserved in 1.1, activated here): `max_turns` (default 10), `tool_budget_bytes` (cumulative tool-result bytes), and the existing `timeout_secs` now covering the whole loop.
- Loop hygiene: identical repeated tool call → inject a nudge message once, then halt the loop and request the final review; malformed tool-call JSON → return a tool error message (one retry), then proceed to final answer; tool execution error → returned to the model as the tool result, never fatal to the agent.
- Models without function calling: `tools: true` on an incapable model degrades gracefully to the 1.0 single-shot path, recorded as `tools_degraded: true` in status.json. Capability is declared in the registry (no probing in v1 of this epic).
- Fallback agents inherit the effective tools setting of the lane invocation; a fallback may also be a non-tool agent (degrade is per-agent).

### Toolset (v1 of this epic — deliberately minimal)

| Tool | Signature | Limits |
|------|-----------|--------|
| `read_file` | `(path, start_line?, end_line?)` | per-call byte cap; line-numbered output |
| `grep` | `(pattern, glob?)` | regex via Go stdlib; match cap with truncation marker |
| `list_files` | `(dir?)` | depth-capped listing |

No write tools. No shell. No network. Additions (e.g., `git_log`, `blame`) wait for field evidence.

### Sandbox: path jail + head snapshot

- Tools operate on a snapshot of the repo at the resolved `head` SHA. When `head` == current `HEAD` and the worktree is clean, the live worktree is used; otherwise the engine creates a temporary `git worktree add` at `head` and removes it after the run (recorded in manifest.json).
- Path jail: all paths resolve relative to the snapshot root; reject absolute paths, `..` escapes, and symlinks that resolve outside the root. `.git/` is not readable.
- Read-only is enforced structurally: there is no write tool, and the harness opens files read-only.

### Artifacts and accounting

- `raw/<agent>/transcript.jsonl` — every turn: request tool_calls, tool results (truncated to a recorded cap), final message. Enables replay and prompt debugging.
- `status.json` gains live `turns`, `tool_calls`, `tool_bytes` counters (reserved in 1.1).
- `manifest.json` `stages` includes `"review"` with `tools: true` agents listed.

### Personas

- Tool-enabled persona variant sections (`{{if .ToolsEnabled}}`): how to budget exploration (verify suspicions before reporting; prefer reading the enclosing file over guessing), and the rule that findings must cite evidence actually read.
- The payload-mode scope rules from 1.0 still apply; tools widen *evidence gathering*, not *scope* — findings still target the changed range unless explicitly out-of-scope-flagged.

## Success Criteria

### Functional
- [ ] A tool-enabled agent completes a multi-turn review against a fixture repo: reads a file outside the payload, greps for callers, and produces findings citing that evidence.
- [ ] All three budgets (turns, tool bytes, timeout) trip cleanly: the agent is asked for its final answer, status.json records which budget tripped, and partial-success semantics hold.
- [ ] Path jail rejects absolute paths, `..`, and out-of-root symlinks in tests; `.git/` is unreadable.
- [ ] Non-tool-capable model with `tools: true` degrades to single-shot with `tools_degraded: true`.
- [ ] `transcript.jsonl` replays a full session; tool results above the truncation cap are marked.
- [ ] Worktree snapshot created and cleaned up when head != HEAD; live worktree used when clean and equal.
- [ ] Mixed roster (tool and non-tool agents) runs in one review; reconcile consumes both identically.

### Quality
- [ ] Tool harness unit-tested without any LLM (direct dispatch tests) and integration-tested via httptest mock provider scripting multi-turn tool_calls exchanges.
- [ ] No new third-party dependencies.
- [ ] docs/registry.md documents `tools`, `max_turns`, `tool_budget_bytes` as active; docs/payload-modes.md explains payload-as-starting-point semantics for tool agents.

## Task Breakdown (dependency order)

1. **Tool harness:** tool definitions (JSON Schema), dispatcher, path jail, per-call caps; unit tests (no LLM).
2. **Snapshot manager:** live-worktree fast path, temporary `git worktree` at head, cleanup, manifest recording.
3. **Agent loop:** multi-turn invoke with tool_calls handling, budgets, loop hygiene, degrade path; httptest-scripted integration tests.
4. **Accounting + artifacts:** transcript.jsonl writer, status.json counters, manifest stages entry.
5. **Persona updates:** tool-enabled guidance sections; evidence-citation rule.
6. **Registry activation:** flip 1.1's reserved fields to active with validation (e.g., `max_turns` bounds).
7. **Docs:** registry.md, payload-modes.md, README — including the cost guidance (3–10× calls per tool agent).

## Technical Constraints

- The harness is entirely in-binary Go — no external agent framework, no MCP loopback, no shell execution.
- OpenAI function-calling wire format only (`tools` array, `tool_calls`, `role:"tool"` messages) — the lowest common denominator across OpenAI-compatible providers and litellm.
- Read-only, path-jailed, no network tools — a reviewer must never be able to mutate the repo or exfiltrate beyond the provider call itself.

## Out of Scope

- Write tools, shell execution, test running (Epic 5.0).
- Skeptic/judge roles (Epics 3.0/4.0) — though they will reuse this loop.
- Dynamic capability probing of providers; MCP-based tool exposure to pool agents.

## Dependencies

- Epic 1.0 complete (fan-out engine, payload engine, findings contract, personas).
- Epic 1.1 complete (reserved registry/status fields).

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Small models use tools badly (thrash, loop, ignore results) | Medium | Medium | Loop hygiene rules, conservative default budgets, per-agent `tools` opt-in — roster tuning stays in user control |
| Provider variance in function-calling dialects | Medium | Medium | Strict lowest-common-denominator wire format; litellm normalizes most providers; degrade path for the rest |
| Token cost explosion | Medium | High | Hard budgets per agent; counters surfaced in report panel table; docs set expectations (3–10×) |
| Worktree management edge cases (dirty trees, submodules) | Medium | Medium | Fast path only when clean and head==HEAD; explicit tests; submodules unreadable in v1 (documented) |

## Clarifications

- **Q: Why not reuse openclaw's harness, which already provided tool calling and filesystem access?** A: Openclaw was used in the source system precisely for that harness, but the preference is to skip it and encapsulate the capability in atcr's own standalone system — no SSH/container infrastructure, one binary. (2026-06-10)
- **Q: Stage placement?** A: Executes immediately after the 1.x epics, before adversarial verification (3.0), because skeptics need this loop to refute meaningfully. (2026-06-10)
