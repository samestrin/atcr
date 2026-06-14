# Phase 1 Spike Findings — Tool-Using Reviewers (Sprint 2.0)

**Date:** 2026-06-13
**Author:** /execute-sprint (Phase 1: Research & Spike)
**Spike code:** throwaway, run from `.planning/.temp/spike-2.0/` (gitignored). Not committed. Phase 2+ authors the real, TDD-tested implementations in `internal/tools/` and `internal/llmclient/`.

All three spikes pass green. Combined result: **38/38 checks passed** (wireformat 14, jail 15, worktree 9). No blockers found for Phase 2. Several design changes are required and are listed per-spike below.

---

## Spike 1.1 — OpenAI Function-Calling Wire Format

**Validated:** request serialization with a `tools` array, `tool_calls` deserialization, `role:"tool"` result-message construction, and a full 2-turn round-trip through an httptest mock provider (no network, no live API — per Phase 1 clarification Q2).

### Confirmed wire shapes

Request (turn includes tools + appended tool result on later turns):
```json
{
  "model": "gpt-4o",
  "messages": [
    {"role":"user","content":"Review engine.go"},
    {"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"engine.go\"}"}}]},
    {"role":"tool","tool_call_id":"call_1","content":"   1| package fanout"}
  ],
  "tools": [{"type":"function","function":{"name":"read_file","description":"...","parameters":{ /* JSON Schema */ }}}],
  "tool_choice": "auto"
}
```

Response (tool-call turn → final turn):
```json
{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[ ... ]}}]}
{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"FINDING: ..."}}]}
```

### Key findings → design changes for Phase 3 (`internal/llmclient/client.go`)

1. **`message.Content` must be a pointer (`*string`) or `json.RawMessage`.** OpenAI sends `content: null` on the tool-call turn. The current `message` struct (`client.go:109`) uses a value `string`, which would silently decode null to `""` — acceptable for decode, but for the *outgoing* assistant echo we must preserve null vs empty. Use `*string` with `omitempty`-safe handling. **The existing 1.0 single-shot path is unaffected** (it never sends tool messages).
2. **`tool_calls[].function.arguments` is a JSON-encoded *string* in the OpenAI/litellm contract**, e.g. `"{\"path\":\"x\"}"`. Decode it by unmarshaling the string, then unmarshaling its contents. **Defensive tolerance required:** some local/non-compliant providers emit `arguments` as a raw JSON *object*. The spike's `decodeArguments` tolerates both (peek first byte: `"` → string-encoded, `{` → raw object). Recommend Phase 2 dispatcher accept `json.RawMessage` and normalize.
3. **Anthropic-via-litellm normalizes to the OpenAI shape**, differing only by `content:""` (empty string) instead of `null`, and `id` prefix `toolu_`. No special-casing needed — the same decoder handles it.
4. **Degrade detection is structural, not capability-probed:** an incapable model that ignores the `tools` array simply returns `finish_reason:"stop"` with `content` and no `tool_calls`. The loop should treat "final message, no tool_calls" as terminal — this is also exactly how the degrade path surfaces at runtime. (Registry `supports_function_calling` is the *declared* capability used to skip the loop entirely — see 1.4 design notes.)
5. **New wire types needed:** `ToolDef`, `functionDef`, `ToolCall` (with `ID`, `Type`, `Function{Name, Arguments}`), and extended `message` (add `ToolCalls`, `ToolCallID`). Keep them in `internal/llmclient`. `ChatResponse` needs `FinishReason`.

---

## Spike 1.2 — Path Jail Prototype

**Validated:** `Resolve(rootCanon, rel)` rejects every escape vector and accepts every legitimate path; `O_NOFOLLOW` probed on darwin.

### Confirmed `Resolve` algorithm (order matters)

1. `filepath.IsAbs(rel)` → reject (absolute).
2. `clean := filepath.Clean(rel)`; reject if `clean == ".."` or `clean` starts with `".." + sep` (lexical escape).
3. Split `clean` on separator; reject if any component `== ".git"` (allows `.gitignore`, `.github`, `foo.git`).
4. `abs := filepath.Join(rootCanon, clean)`.
5. `resolved, err := filepath.EvalSymlinks(abs)`; on `os.IsNotExist` fall back to `abs` (already proven in-root); other errors reject.
6. Prefix check: accept iff `resolved == rootCanon` OR `strings.HasPrefix(resolved, rootCanon + sep)`.

### Key findings → design changes for Phase 2 (`internal/tools/jail.go`)

1. **CRITICAL: the jail root must itself be canonicalized via `EvalSymlinks` at construction.** macOS aliases `/var` → `/private/var` and `/tmp` → `/private/tmp`; the snapshot worktree lives under a temp dir, so a naive prefix check against the raw root rejects *legitimate* in-root files. `Jail.Root` must store the `EvalSymlinks`'d path. (This is the single most important Phase-2 carry-over.)
2. **`O_NOFOLLOW` (value `0x100`, defined on both darwin and linux) is available** and must be OR'd into the harness `os.OpenFile` flags (`os.O_RDONLY|syscall.O_NOFOLLOW`). It refuses to open a symlink (`ELOOP` / "too many levels of symbolic links") while opening regular files normally — closing the final-component swap in the EvalSymlinks→Open TOCTOU window.
3. **TOCTOU residual is bounded and documented:** `EvalSymlinks` resolves the whole chain; `O_NOFOLLOW` guards the final open. An attacker swapping a file for a symlink *after* resolve but *before* open is blocked at the final component by `O_NOFOLLOW`. Post-snapshot mutation of intermediate dirs is **out of scope** (the snapshot is taken at a clean head; see threat model in risks). Document this explicitly.
4. **`.git`-component matching, not substring matching:** split on separator and compare components. `foo.git/bar` and `.gitignore` must pass; only a literal `.git` directory component is blocked.

---

## Spike 1.3 — Git Worktree Lifecycle

**Validated:** `git worktree add --detach <path> <sha>`, correct SHA checkout, file readability, `git worktree remove --force`, full cleanup, and the fast/slow path decision.

### Key findings → design changes for Phase 2 (`internal/tools/snapshot.go`)

1. **Use `--detach`:** `git worktree add --detach <path> <sha>` checks out the SHA in detached-HEAD mode (no branch created/consumed). Confirmed the worktree HEAD equals the requested SHA.
2. **The worktree path must NOT pre-exist** — git creates it. Allocate a unique non-existent leaf under a temp base (`os.MkdirTemp` for the base, then join a leaf git will create).
3. **Fast-path decision:** use the live worktree (repo root) iff `git status --porcelain` is empty AND `head == current HEAD`. Otherwise create a temporary worktree at `head`. (The spike correctly chose the slow path because the tree was dirty from the in-flight sprint-plan edit.)
4. **`.git` in a *linked* worktree is a FILE** (a `gitdir:` pointer), not a directory. The jail's `.git`-component block still applies and is even safer — there is no `.git` tree to descend. **Canonicalize the worktree root with `EvalSymlinks` too** (temp lives under `/var` → `/private/var`) before handing it to the `Jail`.
5. **Cleanup contract:** `git worktree remove --force <path>` removes the worktree and its registration; `worktree list` no longer references it; the path is gone. Record the snapshot mode + worktree path in `manifest.json` (see cross-cutting notes). Cleanup must run on **all** error paths (`defer`), and a failed `remove` must not be fatal to the review (log + continue) — but should be recorded.

---

## Cross-Cutting Design Notes (carry into Phase 2+)

These reconcile the plan's stated file paths/names against the actual 1.0/1.1 codebase. None are blockers; all are Phase 2–6 concerns flagged here so the gate review and later phases proceed without rework.

| # | Plan assumption | Reality (verified) | Action |
|---|-----------------|--------------------|--------|
| C1 | New `invokeAgent(ctx, agent, messages, tools, snapshot)` | `invokeAgent(ctx, a Agent)` already exists at `internal/fanout/engine.go:228` (single-shot) | Phase 3: branch on `Agent.Tools` — add a `invokeToolAgent` helper or extend `invokeAgent` to dispatch; keep the 1.0 single-shot path intact. |
| C2 | `internal/fanout/manifest.go` | `manifest.go` lives in `internal/payload/`; `Manifest.Stages` already = `["review"]` (`review.go:234`) | Phase 5: extend `payload.Manifest` (add tool-agent listing to the review stage) in `internal/payload/manifest.go`, not `internal/fanout`. |
| C3 | Registry fields `tools`/`max_turns`/`tool_budget_bytes` to be added | Already reserved + validated at `internal/registry/config.go:65-67`, bounds enforced (`MaxAgentTurns`, `>=0`) | Phase 6 "activation" is mostly *documentation* + wiring into the loop; the parse/validate layer exists. |
| C4 | status.json counters `turns`/`tool_calls`/`tool_bytes` to be added | Already reserved as `*int`/`*int64` with `omitempty` at `internal/fanout/status.go:277-279` | Phase 5: populate them from loop counters; no struct change for these three. |
| C5 | `supports_function_calling` registry field | NOT present yet | Phase 4: add `SupportsFC bool` (yaml `supports_function_calling`, default `false`) to the provider/agent config + validation. |
| C6 | `AgentStatus.ToolsDegraded` | NOT present yet | Phase 4: add `ToolsDegraded bool` (`omitempty`) to `AgentStatus`. |
| C7 | `ChatCompleter` wiring | `Engine` holds a single `Completer` (`Complete(ctx, inv)(string,error)`) | Phase 3: have `*llmclient.Client` implement both `Complete` and `Chat`; engine accepts a `ChatCompleter` (engine can type-assert or take both). Keep `Completer` for the single-shot/degrade path. |
| C8 | `Agent.Tools/MaxTurns/ToolBudgetBytes` on fanout Agent | fanout `Agent` (`engine.go:24`) has none; they live on `registry.AgentConfig` | Phase 3: thread tool config from `registry.AgentConfig` into the resolved fanout `Agent` in `buildAgent`/`buildFallbackAgent` (`review.go:420,473`). |

---

## Risk Register Update

| Risk | Status after spike | Residual mitigation |
|------|--------------------|---------------------|
| Path-jail escape (TOCTOU) | **Mitigated.** EvalSymlinks + canonical root + `O_NOFOLLOW` cover all tested vectors | Document post-snapshot-mutation as out-of-scope; snapshot at clean head |
| macOS symlinked temp root false-rejects | **Found & solved.** Canonicalize root (and worktree root) with EvalSymlinks | Make it a constructor invariant + unit test |
| Provider function-calling dialect variance | **Mitigated.** OpenAI / Anthropic-via-litellm / local raw-object all decode via tolerant `arguments` parser | Add a dialect test matrix in Phase 3 |
| Worktree leak on error | Manageable | `defer remove --force` on all paths; non-fatal but recorded in manifest |
| No new third-party deps | **Confirmed.** All spikes use Go stdlib only (`net/http/httptest`, `path/filepath`, `os/exec`, `syscall`, `encoding/json`) | Keep stdlib-only constraint through Phase 2+ |

## Blockers before Phase 2

**None.** All spike success criteria met. Phase 2 (Foundation) can proceed; carry the C1–C8 design notes and the canonical-root invariant into the RED tests.
