# Sprint 2.0: Tool-Using Reviewers

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 2.0 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

Extending the atcr reviewer pool from single-shot prompted calls into bounded multi-turn agents. Each reviewer can explore the repository through read-only, path-jailed tools (`read_file`, `grep`, `list_files`) exposed via OpenAI-compatible function calling, with the Go engine owning the entire tool harness. The payload becomes the starting point of a review, not the entire universe of context.

### Why This Matters

Single-shot reviewers can only reason about what is in the payload — they hallucinate context they cannot see, miss entire bug classes (the caller that passes nil, the invariant broken two packages away), and degrade badly on smaller models. This epic closes that gap by letting reviewers look things up during the review, producing findings backed by evidence actually read from the codebase.

### Key Deliverables

- Multi-turn agent loop in `internal/fanout/engine.go` driving `tool_calls` exchanges via `ChatCompleter`
- Read-only, path-jailed tool harness (`read_file`, `grep`, `list_files`) in `internal/tools/`
- Snapshot manager with live-worktree fast path and `git worktree` slow path
- Per-agent budget enforcement: `max_turns` (default 10), `tool_budget_bytes`, `timeout_secs`
- Graceful degradation for non-tool-capable models (`tools_degraded: true` in status.json)
- Transcript writer (`raw/<agent>/transcript.jsonl`) and live status.json counters
- Persona guidance for tool-enabled agents with evidence-citation rule
- Registry, payload-modes, and README documentation with cost guidance (3–10×)

### Success Criteria

- A tool-enabled agent completes a multi-turn review against a fixture repo, reads a file outside the payload, greps for callers, and produces findings citing that evidence
- All three budgets trip cleanly with partial-success semantics and status.json markers showing which budget tripped
- Path jail rejects absolute paths, `..`, symlink-escape, and `.git/` in all unit tests
- Non-tool-capable model with `tools: true` degrades to single-shot with `tools_degraded: true` recorded
- Mixed roster (tool-loop and single-shot agents) reconciles without special-casing
- No new third-party dependencies introduced

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** Strict 🔒 (Complexity 10/12 — Very Complex)

All user story implementation follows the STRICT + Adversarial TDD cycle:

1. **RED:** Write comprehensive failing tests covering all ACs and edge cases. Verify tests fail for the right reasons before writing any implementation.
2. **GREEN:** Implement minimal code to pass tests, one test at a time (T1 after each change). Verify all pass (T2). Commit.
3. **ADVERSARIAL:** Spawn a fresh subagent (no memory of implementation) to adversarially review changed files. Required fix threshold: CRITICAL/HIGH. MEDIUM/LOW deferred to tech debt.
4. **REFACTOR:** Apply CRITICAL/HIGH fixes from adversarial, improve code quality, maintain green (T1), validate full suite (T3), commit.

**Adversarial inline-fix bar:** CRITICAL/HIGH | **Deferred to tech debt:** MEDIUM/LOW

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [user-stories/](plan/user-stories/) | Feature requirements |
| [acceptance-criteria/](plan/acceptance-criteria/) | Validation requirements with DoD |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command |
|------|------|---------|
| T1: Focused | After each small change | `go test ./internal/<pkg>/... -run TestXxx` |
| T2: Module | After completing element | `go test ./internal/<pkg>/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

### DoD Verification Checklist
1. Tests (T3): All passing — `go test ./...`
2. Coverage: ≥80% — `go test -coverprofile=coverage.out ./...`
3. Lint: No errors — `golangci-lint run`
4. Vet: No issues — `go vet ./...`
5. Build: Succeeds — `go build ./...`

### DoD Report Template
```
Phase-{N} DoD Complete
Auto: {X}/5 | Story-Specific: {Y}/{Z}
Manual Review: [ ] Code reviewed
```

### Commit Process
Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

---

## Development Standards

### Implementation Standards

**Core Philosophy:** Maintainability and replaceability over cleverness.

- **Black Box Interfaces:** Every module must expose a clean, documented API; implementation details hidden.
- **Replaceable Components:** Any module rewritable from scratch using only its interface. All modules communicate through clean interfaces (`Dispatcher`, `Jail`, `Snapshot`, `ChatCompleter`).
- **Single Responsibility:** One module, one clear purpose (`internal/tools` owns tool concerns; `internal/fanout` owns loop/budget concerns).
- **Primitive-First Design:** Design around core data types: `ToolDef`, `ToolCall`, `ChatResponse`, `Message`, `Jail`, `Snapshot`, `AgentConfig`, `Result`, `AgentStatus`.

**Go Specifics:**
- **Panic Safety:** Goroutines and worker tasks must recover from panics.
- **Defer Cleanup:** Use `defer` to close file descriptors and HTTP response bodies immediately after creation.
- **Interface Segregation:** Return concrete types from constructors; accept interfaces as parameters.
- **Robust Error Handling:** Return `error` as the last parameter; never ignore errors; wrap with `fmt.Errorf("doing X: %w", err)`.
- **Context Propagation:** Accept `context.Context` as first parameter in all I/O-performing functions; respect cancellation.

### Coding Standards

- **Packages:** Lowercase, single-word (`tools`, `fanout`, `llmclient`, `registry`, `payload`).
- **Naming:** Exported = `PascalCase`; unexported = `camelCase`; interfaces end in `-er` for single-action behaviors (`ChatCompleter`, `Completer`).
- **Imports:** Group as stdlib → third-party → internal (`github.com/samestrin/atcr/`); use `goimports`.
- **Testing:** Table-driven tests for multiple scenarios; use `testify/assert` and `testify/require`; integration tests with `//go:build integration` tag.
- **Formatting:** `go fmt` before every commit; `golangci-lint run` must pass; `go vet ./...` must be clean.

### Git Strategy

- **Branch:** `feature/2.0_tool_using_reviewers` from `main`.
- **Commits:** Atomic, conventional format: `feat(tools): add read_file handler`, `test(fanout): add agent loop tests`.
- **Types:** `feat`, `fix`, `docs`, `refactor`, `test`, `chore`.

---

## Clarifications

### Phase 1 Clarifications (recorded 2026-06-13)

**Key Decisions:**
- **Spike code is throwaway.** Phase 1 spikes (1.1–1.3) run as disposable validation code under `.planning/.temp/` (gitignored), NOT in `internal/`. Only task 1.4 commits — the findings doc `clarifications/spike-findings.md`. Phase 2 authors the real, TDD-tested implementations in `internal/tools/` and `internal/llmclient/` fresh.
- **Wire-format validation via httptest mocks.** Phase 1 validates OpenAI/Anthropic/local dialect round-trip using httptest mock servers with representative response shapes — no live API calls, no network. Live-provider exercise (if needed) is deferred to Phase 3 integration tests.

**Scope Boundaries (Phase 1 only — gated):**
- IN: 3 spikes (wire format, path jail, git worktree lifecycle) + `spike-findings.md` + Phase 1 DoD + Phase 1 gate subagent review.
- OUT (deferred to later phases): all production implementation — tool harness, agent loop, budgets, degradation, transcript, persona/docs.

**Technical Approach — design discrepancies to document in spike-findings.md for Phase 2:**
- `invokeAgent` already exists in `internal/fanout/engine.go:228` (single-shot). The multi-turn loop must reconcile this name (new method or branch on `Agent.Tools`) — Phase 3 concern.
- `manifest.go` lives in `internal/payload/`, not `internal/fanout/` as some plan tasks state; `Manifest.Stages` already holds `["review"]`.
- Registry fields `Tools`/`MaxTurns`/`ToolBudgetBytes` are already reserved + validated (`internal/registry/config.go:65-67`); `supports_function_calling` is NOT yet present (new in Phase 4).
- status.json counters `Turns`/`ToolCalls`/`ToolBytes` already reserved (`internal/fanout/status.go:277-279`); `ToolsDegraded` is NOT yet present (new in Phase 4).
- `ChatCompleter` wiring into `Engine` (which currently holds a single `Completer`) is a Phase 3 design decision.

### Phase 3 Clarifications (recorded 2026-06-13)

**Key Decisions:**
- **max_turns semantics = Model A (confirmed).** A "turn" is one `Chat` call sent WITH tools. The turn counter increments at loop start; the budget is checked after the model responds. The model's returned `tool_calls` are executed only if `turns < MaxTurns` (room for another round-trip to feed results back). So `MaxTurns=N` ⇒ N Chat-with-tools calls, but the Nth turn's `tool_calls` are NOT executed; `max_turns` trips → one final `Chat` WITHOUT tools requests the answer (unbudgeted, not counted as a turn). Consequence: `MaxTurns=5` with tools-every-turn ⇒ `Result.Turns=5`, `Result.ToolCalls=4`; `MaxTurns=1` ⇒ `Result.Turns=1`, `Result.ToolCalls=0`. Reconciles AC 01-03 EC3 with AC 02-01 S1/EC1.
- **AgentStatus.ToolsDegraded added now (Phase 3).** AC 01-05 (Story 1) requires it; ACs are the binding contract over spike note C6's tentative Phase-4 tag. Phase 3 implements degrade path #1 (completer does not implement `ChatCompleter`, or no dispatcher injected → single-shot, `tools_degraded:true`). Phase 4 reuses the same field for degrade path #2 (`supports_function_calling` registry capability check) — no new field.

**Scope Boundaries (Phase 3 only — gated):**
- IN: llmclient wire format (`ToolDef`/`ToolCall`/`Message`/`ChatResponse`, `message`+`tool_calls`/`tool_call_id`) + `Chat`; fanout `ChatCompleter` + multi-turn loop + 3 budgets + loop hygiene + degrade path; `Result`/`AgentStatus` counter+tripped-budget+degraded plumbing; `review.go` `buildAgent`/`buildFallbackAgent` tool-field propagation + `PayloadContext.ToolsEnabled`; registry `applyDefaults(max_turns=10 when tools=true)` + `DefaultMaxTurns=10`; `internal/boundaries_test.go` allowlist fix; Phase 3 DoD + gate subagent.
- OUT (later phases): real snapshot→jail→dispatcher wiring into `ExecuteReview` (Phase 6 e2e — until then tool agents degrade safely); `supports_function_calling` capability degrade (Phase 4); `transcript.jsonl` + manifest review-stage recording (Phase 5).

**Technical Approach:**
- **Production wiring deferred to Phase 6 (confirmed).** Phase 3 builds the loop with the dispatcher INJECTED via a new `Engine` `WithDispatcher` option; all 10 ACs are unit-tested with httptest mock providers + fake/real dispatcher. The live snapshot→jail→dispatcher wiring into `ExecuteReview` lands in Phase 6's fixture-repo e2e. A tool-enabled review with no injected dispatcher degrades to single-shot (`tools_degraded:true`).
- **Wire types live in `internal/llmclient`** (spike 1.1 #5); `llmclient` stays decoupled from `internal/tools`. The engine converts `tools.ToolDef` → `llmclient.ToolDef` once before the loop. `*llmclient.Client` implements both `Complete` and `Chat`; the engine selects the loop via type assertion `e.completer.(ChatCompleter)` when `Agent.Tools` is true.
- **Pre-existing red fixed here:** `internal/tools/` (added Phase 2) has no `internal/boundaries_test.go` allowlist entry, so `go test ./...` is currently red (Phase 2 only ran `go test ./internal/tools/...`). Phase 3 adds `"tools": {}` and `"tools"` to `fanout`'s allowed imports (the loop imports `tools`).

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Research & Spike (Day 1)

**Goal:** Validate OpenAI function-calling wire format with litellm, prototype path jail, validate `git worktree` lifecycle. Findings gate Phase 2 design.

### 1.1 [x] **Spike: OpenAI Wire Format Validation**
   1. Set up an httptest mock server returning a response with `tool_calls` in OpenAI format
   2. Send a request with `tools` array and verify the full round-trip: request serialization, `tool_calls` deserialization, `role:"tool"` message construction
   3. Test litellm-normalized responses from OpenAI, Anthropic, and a local model format
   4. Document which dialects require normalization and which degrade cleanly
   **Files:** `internal/llmclient/` (spike harness) | **Duration:** 2-3 hours

### 1.2 [x] **Spike: Path Jail Prototype**
   1. Prototype `Resolve(path)` using `filepath.Abs`, `filepath.Clean`, `filepath.EvalSymlinks`, prefix check against jail root
   2. Test all escape vectors: absolute paths, `..` traversal, symlink pointing outside root, path containing `.git/` component
   3. Evaluate `O_NOFOLLOW` availability on macOS/Linux for the open call
   4. Document rejection behaviors, any platform differences, and TOCTOU risk
   **Files:** `internal/tools/` (spike) | **Duration:** 2 hours

### 1.3 [x] **Spike: Git Worktree Lifecycle**
   1. Run `git worktree add <tmp-path> <sha>` programmatically via `exec.Command`
   2. Verify worktree is at the correct SHA and files are readable
   3. Test fast path: clean worktree with `head == HEAD` (use live worktree)
   4. Test slow path: dirty worktree or `head != HEAD` (use temporary worktree)
   5. Run `git worktree remove --force <path>` and verify cleanup
   **Files:** `internal/tools/` (spike) | **Duration:** 2 hours

### 1.4 [x] **Document Spike Findings & Risks**
   1. Write findings summary to `clarifications/spike-findings.md`
   2. Capture provider dialect risks and any required design changes
   3. Identify any blockers before Phase 2 foundation work
   4. Commit: `git commit -m "chore(spike): document wire format and path jail findings"`
   **Duration:** 1 hour

### 1.5 [x] **Phase 1 DoD**
   - [x] Wire format spike confirms `tool_calls` round-trip through mock provider
   - [x] Path jail prototype rejects all escape vectors: absolute, `..`, symlink-escape, `.git/`
   - [x] Git worktree lifecycle validated for both fast and slow paths
   - [x] Spike findings documented with risks and design decisions

   ```
   Phase-1 DoD Complete
   Auto: N/A | Story-Specific: 4/4
   Manual Review: [ ] Findings reviewed and accepted
   ```

### 1.6 [x] **Phase 1 - GATE: Integration & Exit Review (subagent)**
   **Scope:** Spike findings and design decisions from Phase 1

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's work — this is intentional.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - Spike findings file: absolute path to `clarifications/spike-findings.md`
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (wire format confirmed, jail prototype documented)?
       - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
       - PHASE-EXIT CONTRACT: Phase 2 (Foundation) can proceed without rework from findings?
       - REGRESSION: Earlier-phase behavior still intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (gate review, 2026-06-13):** All cross-cutting claims C1–C8 (line numbers, existing structures, `O_NOFOLLOW=0x100`, no new deps) independently VERIFIED correct against the live codebase. No CRITICAL/HIGH.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | sprint-plan.md:332 | Phase 2 task 2.5 still specs single-arg `Resolve(path)` with `filepath.Abs`; validated spike mandates two-arg `Resolve(rootCanon, rel)` with `filepath.Join` against a canonical root. RED tests could target wrong signature. | Phase 2 follows spike-findings.md, not stale 2.5 prose. Captured as TD-001. |
   | LOW | sprint-plan.md:332-333 | Canonical-root EvalSymlinks invariant lives only in spike-findings.md prose, absent from Phase 2 task spec. | Assert in Phase 2 jail RED tests. Captured as TD-002. |

   **Action taken:** No CRITICAL/HIGH → no pre-boundary fix needed. MEDIUM/LOW appended to `tech-debt-captured.md` (TD-001, TD-002). **Phase gate passed.**
   **Duration:** 15-30 min

---

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 2: Foundation (Days 2-3)

**Goal:** Build tool harness (definitions, dispatcher, handlers), path jail, and snapshot manager with full unit test coverage. No LLM or network required.

### 2.1 [x] **[Story 7: Tool Definitions & Dispatcher - RED](plan/user-stories/07-tool-definitions-dispatcher.md)**
   Write comprehensive failing tests, verify fail correctly
   - Test `ToolDef` JSON Schema serialization for `read_file`, `grep`, `list_files` (matches OpenAI function calling format)
   - Test dispatcher routing: each tool name routes to the correct handler
   - Test per-call byte caps: result truncated at cap with truncation marker appended
   - Test `read_file`: line-numbered output, optional `start_line`/`end_line` slicing, byte-cap truncation
   - Test `grep`: regex pattern matching, optional glob filter, match-cap truncation with marker
   - Test `list_files`: directory listing, depth cap enforcement
   - Test unknown tool name returns error result (never fatal)
   **Files:** `internal/tools/defs_test.go`, `internal/tools/dispatch_test.go`, `internal/tools/read_file_test.go`, `internal/tools/grep_test.go`, `internal/tools/list_files_test.go` | **Duration:** 3-4 hours
   **AC:** [07-01](plan/acceptance-criteria/07-01-read-file-tool.md), [07-02](plan/acceptance-criteria/07-02-grep-tool.md), [07-03](plan/acceptance-criteria/07-03-list-files-tool.md), [07-04](plan/acceptance-criteria/07-04-tool-dispatcher-byte-caps.md)

### 2.2 [x] **[Story 7: Tool Definitions & Dispatcher - GREEN](plan/user-stories/07-tool-definitions-dispatcher.md)**
   Minimal code to pass tests, one test at a time (T1), verify all pass (T2), COMMIT
   - `internal/tools/defs.go`: `ToolDef` structs with OpenAI JSON Schema, `AllDefs()` function
   - `internal/tools/dispatch.go`: `Dispatcher` struct with `Execute(toolName, args, jail)`, per-call byte cap, truncation marker
   - `internal/tools/read_file.go`: line-numbered reader, slicing, byte-cap truncation
   - `internal/tools/grep.go`: Go stdlib `regexp` search, optional glob filter, match-cap truncation
   - `internal/tools/list_files.go`: `os.ReadDir` with depth cap
   COMMIT: `git commit -m "feat(tools): add tool definitions, dispatcher, and handlers (green)"`
   **Files:** `internal/tools/defs.go`, `internal/tools/dispatch.go`, `internal/tools/read_file.go`, `internal/tools/grep.go`, `internal/tools/list_files.go` | **Duration:** 4-5 hours

### 2.2.A [x] **[Story 7: Tool Definitions & Dispatcher - ADVERSARIAL REVIEW (subagent)](plan/user-stories/07-tool-definitions-dispatcher.md)**
   **Changed Files:** `internal/tools/defs.go`, `internal/tools/dispatch.go`, `internal/tools/read_file.go`, `internal/tools/grep.go`, `internal/tools/list_files.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2 Tool Definitions & Dispatcher`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FILES FROM 2.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? (path traversal in grep glob? regex DoS via catastrophic backtracking?)
       - EDGE CASES: Null, empty, boundaries, concurrent access? (empty file, zero byte cap, concurrent Dispatch calls?)
       - ERROR HANDLING: Missing catches, swallowed errors? (file open errors, regex compile errors?)
       - PERFORMANCE: N+1, leaks, blocking ops? (unclosed file descriptors?)
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (2026-06-13):** No CRITICAL/HIGH. Confirmed: no sandbox escape (WalkDir/ReadDir do not traverse symlinked subdirs; glob matches basename only), RE2 has no catastrophic backtracking, write-tool guard sound, no FD leaks.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | grep.go scanFileForMatches | Scanner errors (`bufio.ErrTooLong` on >10MB line, mid-file IO error) swallowed → silent partial results | FIXED 2.3: check `sc.Err()`, append `[skipped: ...]` note |
   | MEDIUM | dispatch.go SetLimits/Execute | `SetLimits` mutates `d.limits` unsynchronized vs concurrent `Execute` reads (data race) | FIXED 2.3: documented as construction/tuning-only, not concurrent-safe (test-only mutator; production constructs once) |
   | LOW | dispatch.go truncate | Byte-slice cut can split a multi-byte UTF-8 rune → invalid UTF-8 in tool message | FIXED 2.3: `safeRuneCut` backs up to a rune boundary |
   | LOW | grep.go per-line cap | Same UTF-8 split on per-match-line truncation | FIXED 2.3: `safeRuneCut` |
   | LOW | open_other.go:9 | Non-unix build lacks `O_NOFOLLOW`, reopening read_file TOCTOU window (unix targets unaffected) | DEFERRED → TD-003 |

   **Action taken:** No CRITICAL/HIGH → no blocking pre-2.3 fix. Three MEDIUM/LOW correctness/robustness items fixed inline during 2.3 REFACTOR (cheap, no scope creep); one LOW platform residual deferred to `tech-debt-captured.md` (TD-003). **Adversarial review passed.**

### 2.3 [x] **[Story 7: Tool Definitions & Dispatcher - REFACTOR](plan/user-stories/07-tool-definitions-dispatcher.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any)
   2. Improve code quality: error messages, naming, interface clarity (T1)
   3. Validate all tests still pass (T3)
   4. COMMIT: `git commit -m "refactor(tools): address review + clean up dispatcher"`
   **Duration:** 1-2 hours

### 2.4 [x] **[Story 3: Path Jail & Snapshot Sandbox - RED](plan/user-stories/03-path-jail-sandbox.md)**
   Write comprehensive failing tests, verify fail correctly
   - Test `Jail.Resolve()`: absolute path rejected, `..` traversal rejected, symlink-escape rejected, `.git/` component rejected, valid paths accepted
   - Test paths with `.gitignore` and `.github/workflows/ci.yml` pass (false-positive check)
   - Test `foo.git/bar` passes (only `.git` directory component is blocked)
   - Test `Snapshot.For(head)`: returns live-worktree path when `head == HEAD` and worktree is clean
   - Test `Snapshot.For(head)`: creates temporary worktree when head differs or worktree is dirty
   - Test cleanup: temporary worktree removed after `Close()`
   - Test write-tool guard: `AllDefs()` contains no write tools (init-time invariant)
   **Files:** `internal/tools/jail_test.go`, `internal/tools/snapshot_test.go` | **Duration:** 2-3 hours
   **AC:** [03-01](plan/acceptance-criteria/03-01-path-jail-enforcement.md), [03-02](plan/acceptance-criteria/03-02-snapshot-lifecycle.md), [03-03](plan/acceptance-criteria/03-03-worktree-cleanup.md), [03-04](plan/acceptance-criteria/03-04-read-only-guard.md)

### 2.5 [x] **[Story 3: Path Jail & Snapshot Sandbox - GREEN](plan/user-stories/03-path-jail-sandbox.md)**
   Minimal code to pass tests, one test at a time (T1), verify all pass (T2), COMMIT
   - `internal/tools/jail.go`: `Jail` struct with `Root string`; `Resolve(path) (string, error)` using `filepath.Abs`, `filepath.Clean`, `filepath.EvalSymlinks`, prefix check; `.git` component matching
   - `internal/tools/snapshot.go`: `Snapshot` struct; `For(head string) (Snapshot, error)` — fast path when clean + `head==HEAD`, slow path with `git worktree add`; `Close()` removes temporary worktree
   COMMIT: `git commit -m "feat(tools): add path jail and snapshot manager (green)"`
   **Files:** `internal/tools/jail.go`, `internal/tools/snapshot.go` | **Duration:** 3-4 hours

### 2.5.A [x] **[Story 3: Path Jail & Snapshot Sandbox - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-path-jail-sandbox.md)**
   **Changed Files:** `internal/tools/jail.go`, `internal/tools/snapshot.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.5 Path Jail & Snapshot`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FILES FROM 2.5]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? (TOCTOU between EvalSymlinks and Open? absolute path bypass? symlink race? git worktree path injection?)
       - EDGE CASES: Null, empty, boundaries, concurrent access? (empty head SHA, submodules, worktree collision if path already exists?)
       - ERROR HANDLING: Missing catches, swallowed errors? (git command failures, cleanup failures on error paths?)
       - PERFORMANCE: N+1, leaks, blocking ops? (uncleaned worktrees on error paths?)
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (2026-06-13):** No CRITICAL/HIGH ranked. Confirmed sound: SHA arg-injection blocked (anchored regex + arg arrays), jail sibling-prefix (`/a/b` vs `/a/bc`) correctly avoided, `resolveExisting` catches escaping symlinks in existing intermediate components, absolute/`..` rejected pre-FS. Reviewer raised a case-insensitive-FS caveat on `.git` matching — escalated to a fix because macOS (atcr's primary platform) is case-insensitive by default.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH (macOS) | jail.go .git check | Case-sensitive `seg == ".git"` lets `.GIT/config` reach the real `.git` dir on case-insensitive filesystems (macOS/Windows default) → repo-internals exposure | FIXED 2.6: `strings.EqualFold(seg, ".git")` + RED tests for `.GIT`/`.Git` |
   | MEDIUM | snapshot.go cleanup guard | `HasPrefix(base, Clean(TempDir()))` has a sibling-prefix flaw (`/tmpfoo` vs `/tmp`) — last line of defense before RemoveAll | FIXED 2.6: append `os.PathSeparator` to the prefix |
   | MEDIUM | snapshot.go prune retry | Repo-wide `git worktree prune` on add-failure can prune a concurrent run's mid-registration worktree | DEFERRED → TD-005 (low likelihood: unique temp leaves) |
   | LOW | snapshot.go prune err | `worktree prune` error silently discarded | FIXED 2.6: logged to stderr |
   | LOW | snapshot.go leaf name | Leaf built from raw (possibly abbreviated) `head` not resolved full SHA | FIXED 2.6: use `resolvedHead` |
   | INFO | jail TOCTOU residual | Final-component swap already closed by `O_NOFOLLOW` in `openReadOnly`; intermediate-dir post-snapshot mutation is the documented out-of-scope threat (spike risk register) | No action — by design |
   | INFO | — | Manifest snapshot-field recording (AC 03-02 S5, 03-03 S4/S5) is engine-integration work | DEFERRED → TD-004 (Phase 3/5) |

   **Action taken:** No blocking CRITICAL/HIGH from the ranked table; the macOS `.git` case-insensitivity gap was escalated and fixed inline in 2.6 (real exposure on the primary dev platform). Cheap MEDIUM/LOW correctness items fixed inline; prune-concurrency (TD-005) and manifest-recording (TD-004) deferred. **Adversarial review passed.**

### 2.6 [x] **[Story 3: Path Jail & Snapshot Sandbox - REFACTOR](plan/user-stories/03-path-jail-sandbox.md)**
   1. Fix CRITICAL/HIGH issues from 2.5.A (if any)
   2. Improve code quality: error wrapping, cleanup on all error paths, clear invariant comments (T1)
   3. Validate all tests still pass (T3)
   4. COMMIT: `git commit -m "refactor(tools): address review + clean up jail and snapshot"`
   **Duration:** 1-2 hours

### 2.7 [x] **Phase 2 DoD**
   - [x] `go test ./internal/tools/...` — all passing
   - [x] `go test -coverprofile=coverage.out ./internal/tools/...` — ≥80% coverage (84.6%)
   - [x] `golangci-lint run ./internal/tools/...` — no errors (0 issues)
   - [x] `go vet ./internal/tools/...` — clean
   - [x] `go build ./...` — succeeds
   - [x] Story 7 ACs verified: `read_file`, `grep`, `list_files` handlers + dispatcher byte caps
   - [x] Story 3 ACs verified: all escape vectors rejected, snapshot lifecycle clean, no write tools

   ```
   Phase-2 DoD Complete
   Auto: 5/5 | Story-Specific: 8/8 (Stories 7 + 3)
   Manual Review: [ ] Code reviewed
   ```

### 2.8 [x] **Phase 2 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2 (`internal/tools/`)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 2 (absolute paths): all files in `internal/tools/`
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `Dispatcher.Execute` signature, `Jail.Resolve` signature, `Snapshot.For` signature — all consumable by Phase 3 agent loop?
       - CONFIG SURFACE: Per-call byte cap defaults documented?
       - INTEGRATION: No hidden coupling between dispatcher, jail, and snapshot?
       - PHASE-EXIT CONTRACT: Phase 3 (agent loop) can wire in Dispatcher and Jail without rework?
       - REGRESSION: Any existing `internal/tools/` code untouched?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate findings (2026-06-13):** Confirmed sound exit contracts (`Dispatcher.Execute`, `NewJail`/`Jail.Resolve`, `SnapshotManager.SnapshotFor`, `ToolDef.MarshalJSON`/`Tools()`, `Resolver` satisfied by `*Jail`); `DefaultLimits` + cap constants exported/documented; nothing outside `internal/tools/` touched. One HIGH integration trap found and FIXED before the boundary:
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | dispatch.go NewDispatcher / jail.go NewJail | Dispatcher took a separate `root` arg stored verbatim while `NewJail` canonicalized its root via EvalSymlinks → a Phase 3 caller passing the raw snapshot root to both would get garbage grep/list relative paths on macOS slow path | FIXED before boundary: `Resolver` interface gains `Root()`; `NewDispatcher(jail, limits)` derives root from `jail.Root()` — mismatch structurally impossible. Re-gate: NONE. |

   **Action taken:** HIGH fixed inline before the phase boundary (no stop); gate re-run returned NONE (single-source canonicalization invariant confirmed end-to-end). **Phase gate passed.** Phase 3 wiring contract: `SnapshotFor(head)` → `NewJail(root)` → `NewDispatcher(jail, DefaultLimits())` → `Execute`.
   **Duration:** 15-30 min

---

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 3: Core Items (Days 4-6)

**Goal:** Implement `ChatCompleter` interface, llmclient wire format, multi-turn agent loop, and budget enforcement with httptest-scripted integration tests.

### 3.1 [x] **[Story 1: Agent Loop Execution - RED](plan/user-stories/01-agent-loop-execution.md)**
   Write comprehensive failing tests, verify fail correctly
   - Test `ChatCompleter` interface: `Chat(ctx, inv, messages, tools)` signature accepted by fanout engine
   - Test llmclient wire format: `tools` array serialized in request body; `tool_calls` deserialized from response; `role:"tool"` messages accepted
   - Test `invokeAgent` multi-turn loop: sends messages → receives `tool_calls` → dispatches → appends `role:tool` results → repeats → stops on final message
   - Test loop hygiene: identical repeated tool call → nudge message injected once, then loop halts and requests final answer
   - Test malformed tool-call JSON → error returned as tool result (one retry), then proceeds to final answer
   - Test tool execution error → returned as tool result, not fatal
   - Test `Result` struct: `Turns`, `ToolCalls`, `ToolBytes` populated correctly after loop
   - Test backward compatibility: existing `Completer` (1.0 single-shot) path still works, returns default `Result` values
   **Files:** `internal/fanout/engine_test.go`, `internal/llmclient/client_test.go` | **Duration:** 3-4 hours
   **AC:** [01-01](plan/acceptance-criteria/01-01-chatcompleter-interface-wire-format.md), [01-02](plan/acceptance-criteria/01-02-multi-turn-agent-loop.md), [01-03](plan/acceptance-criteria/01-03-per-agent-budget-enforcement.md), [01-04](plan/acceptance-criteria/01-04-loop-hygiene.md), [01-05](plan/acceptance-criteria/01-05-degrade-path-fallback-inheritance.md), [01-06](plan/acceptance-criteria/01-06-result-accounting-compat.md)

### 3.2 [x] **[Story 1: Agent Loop Execution - GREEN](plan/user-stories/01-agent-loop-execution.md)**
   Minimal code to pass tests, one test at a time (T1), verify all pass (T2), COMMIT
   - `internal/llmclient/client.go`: Add `ChatCompleter` interface; extend request struct to include `tools []ToolDef` when non-nil; parse `tool_calls` from response; build `role:"tool"` messages
   - `internal/fanout/engine.go`: Add `invokeAgent(ctx, agent, messages, tools, snapshot)` — branches on `Agent.Tools`; drives multi-turn loop: `Chat` → check `tool_calls` → dispatch via `Dispatcher` → append results → check budgets → repeat until final message; loop hygiene (nudge, retry, error-as-result)
   - `internal/fanout/engine.go`: `Result` struct with `Turns int`, `ToolCalls int`, `ToolBytes int64`; populated from loop counters
   COMMIT: `git commit -m "feat(fanout): add multi-turn agent loop with tool dispatch (green)"`
   **Files:** `internal/llmclient/client.go`, `internal/fanout/engine.go` | **Duration:** 5-6 hours

### 3.2.A [x] **[Story 1: Agent Loop Execution - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-agent-loop-execution.md)**
   **Changed Files:** `internal/llmclient/client.go`, `internal/fanout/engine.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.2 Agent Loop Execution`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FILES FROM 3.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? (tool dispatch with user-controlled tool names? context injection via tool results into subsequent Chat messages?)
       - EDGE CASES: Null, empty, boundaries, concurrent access? (nil tools array, empty tool_calls slice, zero-turn budget, concurrent invoke calls?)
       - ERROR HANDLING: Missing catches, swallowed errors? (Chat error mid-loop, dispatcher panic recovery, context cancellation mid-tool-call?)
       - PERFORMANCE: N+1, leaks, blocking ops? (unbounded message accumulation across turns, goroutine leaks on context cancel?)
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (2026-06-13):** Files reviewed: `client.go`, `chat.go`, `engine.go`, `loop.go`. Reviewer confirmed sound: API key never leaks (errors echo only the redacted snippet/endpoint), `len(out.Content)` byte accounting reflects delivered bytes, FD-safe HTTP path, bounded response read. One real wire-protocol defect found (three ranked rows, same root cause) and FIXED inline in 3.3:
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | loop.go first-repeat | First-repeat nudge appended a `role:user` message and skipped the call WITHOUT a `role:tool` answer for its `tool_call_id`; the assistant `tool_calls` message was already appended → next request has a dangling tool_call_id (and an illegally interleaved user message) → OpenAI-compatible providers HTTP 400, breaking the loop. | FIXED 3.3: answer every skipped/repeated call with a `role:tool` nudge result (no interleaved user message); the per-call result both satisfies the wire contract and nudges. |
   | HIGH | loop.go nudged-reappear halt | Second-repeat halt `continue`d without answering the reappeared `tool_call_id`, so `requestFinalAnswer`'s Chat was malformed → 400 → partial findings discarded. | FIXED 3.3: append a `role:tool` answer for the reappeared call before halting. |
   | MEDIUM | loop.go max_turns trip | Model-A max_turns trip intentionally leaves the turn's tool_calls unexecuted; `requestFinalAnswer` then sent an assistant turn with unanswered tool_calls → malformed final request. | FIXED 3.3: `answerSkipped` appends a `role:tool` "skipped: turn budget reached" result for each unexecuted call before the final request. |
   | LOW | engine.go:202 | Fan-out lane goroutines have no `recover()`; a panic in an agent invocation crashes the process and the WaitGroup never drains (pre-existing 1.x; tool loop widens the surface). | DEFERRED → TD-006 (broader engine-concurrency change, no known panic path; dispatcher already recovers tool panics). |
   | LOW | chat.go ToolCallArguments / toolSig | Two malformed args with different raw encodings that decode to the same invalid inner string collide in `toolSig`. | No action — dedup on decoded args is intentional and correct (string-encoded and raw-object forms of the SAME call SHOULD dedup); both are rejected as malformed regardless. |

   **Action taken:** The HIGH/MEDIUM rows are one defect (dangling `tool_call_id` makes the conversation malformed for real providers; my fake `Chat` did not validate pairing, so unit tests missed it). FIXED inline in 3.3: every `tool_call` is now answered with a `role:tool` result on all skip paths, and the test fake (`scriptedChat`) now rejects a dangling `tool_call_id` exactly as a provider's HTTP 400 would — so the invariant is enforced across the whole loop suite. One LOW deferred to TD-006; one LOW is by-design. **Adversarial review passed (no CRITICAL; HIGH fixed before proceeding).**

### 3.3 [x] **[Story 1: Agent Loop Execution - REFACTOR](plan/user-stories/01-agent-loop-execution.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any)
   2. Improve code quality: extract loop sub-steps into clear helpers, naming, context propagation (T1)
   3. Validate all tests still pass (T3)
   4. COMMIT: `git commit -m "refactor(fanout): address review + clean up agent loop"`
   **Duration:** 1-2 hours

### 3.4 [x] **[Story 2: Budget Enforcement - RED](plan/user-stories/02-budget-enforcement.md)**
   Write comprehensive failing tests, verify fail correctly
   - Test turn budget: loop stops when `turns >= max_turns`; `AgentStatus.TrippedBudgets` records `"max_turns"`
   - Test tool byte budget: cumulative byte sum tracked per agent; trip deferred to end-of-turn (current tool result delivered in full); `"tool_budget_bytes"` recorded
   - Test timeout enforcement: `context.WithTimeout` cancels Chat call; partial result returned; `"timeout"` recorded
   - Test budgets enforce independently and in combination (earliest budget to trip records the reason)
   - Test partial-success semantics: partial findings are usable, not discarded when budget trips
   **Files:** `internal/fanout/engine_test.go` (budget sections), `internal/fanout/status_test.go` | **Duration:** 2-3 hours
   **AC:** [02-01](plan/acceptance-criteria/02-01-turn-budget-enforcement.md), [02-02](plan/acceptance-criteria/02-02-tool-byte-budget-enforcement.md), [02-03](plan/acceptance-criteria/02-03-timeout-enforcement.md), [02-04](plan/acceptance-criteria/02-04-budget-status-reporting-partial-success.md)

### 3.5 [x] **[Story 2: Budget Enforcement - GREEN](plan/user-stories/02-budget-enforcement.md)**
   **Note (2026-06-13):** Budget enforcement is inseparable from the loop — a loop with no `max_turns` cannot be bounded — so the GREEN budget code (`loop.go` max_turns/tool_budget_bytes/timeout checks, `status.go` `TrippedBudgets`, `registry` `applyDefaults`+`DefaultMaxTurns`, `artifacts.go` counter propagation) was authored together with the Story 1 loop in commit `8ff1a73`. Task 3.4 then added the dedicated budget RED tests (`engine_budget_test.go`, `status_tools_test.go`) which verify all of Story 2's ACs against that code. **Model-A corollary (documented):** under the confirmed max_turns semantics the literal "both `max_turns` and `tool_budget_bytes` trip on the same turn" case (AC 02-02 EC3 / AC 02-04 S2) is unreachable — the max_turns turn does not execute tools, so its bytes cannot also push over budget on that turn; budgets still trip independently across turns and `TrippedBudgets` de-dupes multiple entries. Captured as TD-007.
   Minimal code to pass tests, one test at a time (T1), verify all pass (T2), COMMIT
   - `internal/fanout/engine.go`: Budget check at top of each loop iteration: `turns >= agent.MaxTurns` trips `max_turns`; accumulate `toolBytes`; check `toolBytes > agent.ToolBudgetBytes` at end of each turn for `tool_budget_bytes`
   - `internal/fanout/engine.go`: Wrap loop with `context.WithTimeout(ctx, time.Duration(agent.TimeoutSecs)*time.Second)` for `timeout`
   - On budget trip: inject nudge → request final answer → collect partial findings
   - `internal/fanout/status.go`: `AgentStatus` struct with `TrippedBudgets []string` field; populated on trip
   COMMIT: `git commit -m "feat(fanout): add budget enforcement (max_turns, tool_bytes, timeout) (green)"`
   **Files:** `internal/fanout/engine.go`, `internal/fanout/status.go` | **Duration:** 3-4 hours

### 3.5.A [x] **[Story 2: Budget Enforcement - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-budget-enforcement.md)**
   **Changed Files:** `internal/fanout/engine.go` (budget sections), `internal/fanout/status.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.5 — this is intentional.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.5 Budget Enforcement`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FILES FROM 3.5]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? (budget counters manipulable via tool results? int64 overflow on `tool_bytes`?)
       - EDGE CASES: Null, empty, boundaries, concurrent access? (`max_turns=0`, `tool_budget_bytes=0`, `timeout_secs=0` — do they trip immediately?)
       - ERROR HANDLING: Missing catches, swallowed errors? (`context.DeadlineExceeded` propagation? budget trip on first turn?)
       - PERFORMANCE: N+1, leaks, blocking ops? (context not cancelled after loop exit?)
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (2026-06-13):** Files reviewed: `loop.go`, `engine.go`, `status.go`, `artifacts.go`, `registry/config.go`, `registry/precedence.go`. No CRITICAL/HIGH/MEDIUM. Reviewer independently verified: counters reach status.json on every halt path (normal/max_turns/byte/timeout/error/degrade); non-tool agents emit no tool fields (omitempty); strictly-greater byte trip with exactly-equal not tripping; deferred byte trip (full delivery then trip); timeout precedence over byte; DeadlineExceeded+Canceled→timeout; Model-A boundary; default applied once; no int64 overflow; counters not model-manipulable (REVIEWER stamped server-side); no context leak (`defer cancel()`).
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | loop.go requestFinalAnswer | On a byte/max_turns trip where the deadline lapsed microseconds before, `requestFinalAnswer` fired one doomed provider Chat (harmless — it returns a deadline error, timeout is recorded, counters survive) instead of short-circuiting. | FIXED 3.6: added an early `ctx.Err()` guard at the top of `requestFinalAnswer` that records timeout and finalizes without the wasted round-trip. |
   | LOW | loop.go dispatchTurn/run | When a loop-hygiene halt and a byte overage coincide on the same turn, the hygiene halt short-circuits before the byte-budget check, so `tool_budget_bytes` is not added to `TrippedBudgets` (ToolBytes is still reported accurately). | No action — by design: the hygiene halt is the proximate cause, mirroring the documented timeout-precedence ordering; the byte count is preserved on the result. |

   **Action taken:** No CRITICAL/HIGH/MEDIUM. One cheap LOW (doomed-final-Chat guard) fixed inline in 3.6; one LOW is intended precedence (documented). **Adversarial review passed.**

### 3.6 [x] **[Story 2: Budget Enforcement - REFACTOR](plan/user-stories/02-budget-enforcement.md)**
   1. Fix CRITICAL/HIGH issues from 3.5.A (if any)
   2. Improve budget check clarity: extract to `checkBudgets` helper, clear error messages (T1)
   3. Validate all tests still pass (T3)
   4. COMMIT: `git commit -m "refactor(fanout): address review + clean up budget enforcement"`
   **Duration:** 1 hour

### 3.7 [x] **Phase 3 DoD**
   - [x] `go test ./internal/fanout/... ./internal/llmclient/...` — all passing (full suite `go test ./...` green)
   - [x] `go test -coverprofile=coverage.out ./internal/fanout/... ./internal/llmclient/...` — ≥80% (fanout 86.7%, llmclient 91.0%, registry 87.6%)
   - [x] `golangci-lint run ./internal/fanout/... ./internal/llmclient/...` — no errors (0 issues, incl. registry/payload/internal)
   - [x] `go vet ./...` — clean
   - [x] `go build ./...` — succeeds
   - [x] Story 1 ACs verified: ChatCompleter + wire format, multi-turn loop, loop hygiene (nudge/halt/malformed), result accounting, degrade path, backward compat (single-shot unchanged)
   - [x] Story 2 ACs verified: all three budgets enforce independently, partial-success semantics, status.json `tripped_budgets`/counter markers

   ```
   Phase-3 DoD Complete
   Auto: 5/5 | Story-Specific: 10/10 (Stories 1 + 2)
   Manual Review: [ ] Code reviewed
   ```

### 3.8 [x] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3 (`internal/fanout/engine.go`, `internal/fanout/status.go`, `internal/llmclient/client.go`)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): `internal/fanout/engine.go`, `internal/fanout/status.go`, `internal/llmclient/client.go`
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `invokeAgent` signature, `Result` struct, `AgentStatus.TrippedBudgets` — all consumable by Phase 4?
       - CONFIG SURFACE: `max_turns` (default 10), `tool_budget_bytes`, `timeout_secs` defaults documented?
       - INTEGRATION: Phase 2 `Dispatcher` and `Jail` integrated correctly into loop?
       - PHASE-EXIT CONTRACT: Phase 4 (degradation) can branch on `Agent.Tools` at `invokeAgent` entry without rework?
       - REGRESSION: 1.x single-shot `Completer` path untouched and all prior tests still pass?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate findings (2026-06-13):** All Phase 3 production files reviewed by a fresh hostile-integrator subagent. **No CRITICAL/HIGH/MEDIUM/LOW.** Independently verified: build/vet clean, full suites green, boundaries allowlist complete + acyclic + `fanout→tools` valid, no `go.mod`/`go.sum` delta (stdlib only). Exit contract confirmed consumable by Phase 4:
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | INFO | engine.go / status.go / loop.go | CONTRACT EXIT, CONFIG SURFACE (`max_turns`=10 / `tool_budget_bytes`=0-unlimited / `timeout_secs`, documented in docs/registry.md), INTEGRATION (toolDispatcher seam sound), REGRESSION (1.x single-shot untouched; `statusFor` gates tool fields behind `r.Tools` → byte-identical 1.x status.json; `send`/`attempt` refactor preserves `Complete` error semantics, 20+ tests incl. explicit regression guard) — all verified. Gate PASSES. |
   | INFO | engine.go invokeAgent branch | Phase 4 seam: add `supports_function_calling` as an additive `Agent` field and extend the `if a.Tools` branch; reuses `invokeDegraded` (`ToolsDegraded=true`) — no change to `invokeAgent`/`Result`/`AgentStatus` signatures or the loop. |
   | INFO | loop.go | Phase 5 note: accounting counters are exposed on `Result`→`AgentStatus` (no signature change), but the per-turn `TranscriptWriter` must be injected into `toolLoop` (the live `l.messages` stream is loop-internal) — additive observer plumbing into loop bodies. |
   | INFO | review.go ExecuteReview | By design (scope boundary): `ExecuteReview` builds the engine without `WithDispatcher`, so production tool agents degrade safely until Phase 6 wires `SnapshotFor(head)`→`NewJail(root)`→`NewDispatcher(jail, DefaultLimits())`→`WithDispatcher`. No Phase 3 rework. |

   **Action taken:** No CRITICAL/HIGH (nothing to fix before the boundary). Phase 4/5/6 wiring guidance captured above. **Phase gate passed.**
   **Duration:** 15-30 min

---

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 4: Advanced (Days 7-8)

**Goal:** Graceful degradation for non-tool-capable models, fallback tool-setting inheritance, and mixed roster compatibility.

### 4.1 [x] **[Story 4: Graceful Degradation - RED](plan/user-stories/04-graceful-degradation.md)**
   Write comprehensive failing tests, verify fail correctly
   - Test degrade path: model with `supports_function_calling: false` and `tools: true` → executes single-shot, `AgentStatus.ToolsDegraded = true`
   - Test tool-capable path: model with `supports_function_calling: true` and `tools: true` → executes `invokeAgent` multi-turn loop
   - Test fallback inheritance: fallback agent inherits effective `Tools` setting from the lane invocation; fallback can also be a non-tool agent (degrade is per-agent)
   - Test mixed roster: tool-loop and single-shot agents in the same review run; reconciler receives both result shapes; no special-casing required
   - Test `supports_function_calling` field in registry.yaml parsed correctly per model/provider
   **Files:** `internal/fanout/engine_test.go` (degrade sections), `internal/fanout/review_test.go` | **Duration:** 2-3 hours
   **AC:** [04-01](plan/acceptance-criteria/04-01-single-shot-degradation-path.md), [04-02](plan/acceptance-criteria/04-02-tool-capable-agent-loop-path.md), [04-03](plan/acceptance-criteria/04-03-fallback-degradation-inheritance.md), [04-04](plan/acceptance-criteria/04-04-mixed-roster-reconciler-compatibility.md)

### 4.2 [x] **[Story 4: Graceful Degradation - GREEN](plan/user-stories/04-graceful-degradation.md)**
   Minimal code to pass tests, one test at a time (T1), verify all pass (T2), COMMIT
   - `internal/registry/`: Add `SupportsFC bool` per agent/provider entry; parse from `supports_function_calling` YAML field with default `false`
   - `internal/fanout/engine.go`: Branch at `invokeAgent` entry: if `Agent.Tools && !registry.SupportsFC(agent.Model)` → call single-shot `Complete`, set `result.ToolsDegraded = true`
   - `internal/fanout/status.go`: `AgentStatus.ToolsDegraded bool` field added
   - `internal/fanout/review.go`: Fallback agent inherits `Tools` setting from lane invocation; `ToolsDegraded` determined independently per fallback agent
   COMMIT: `git commit -m "feat(fanout): add graceful degradation and fallback inheritance (green)"`
   **Files:** `internal/fanout/engine.go`, `internal/fanout/review.go`, `internal/fanout/status.go`, `internal/registry/` | **Duration:** 3-4 hours

### 4.2.A [x] **[Story 4: Graceful Degradation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-graceful-degradation.md)**
   **Changed Files:** `internal/fanout/engine.go` (degrade sections), `internal/fanout/review.go`, `internal/fanout/status.go`, `internal/registry/`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 4.2.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.2 Graceful Degradation`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FILES FROM 4.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? (registry field spoofing allowing tool use on incapable model? `ToolsDegraded` flag bypassed?)
       - EDGE CASES: Null, empty, boundaries, concurrent access? (model not in registry, fallback chain depth, `tools: true` but tool list empty?)
       - ERROR HANDLING: Missing catches, swallowed errors? (registry parse errors causing silent full-access grant?)
       - PERFORMANCE: N+1, leaks, blocking ops? (registry lookup performed on every loop turn?)
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (2026-06-13):** Fresh hostile-integrator subagent reviewed `engine.go`, `review.go`, `status.go`, `artifacts.go`, `loop.go`, `registry/config.go`. **No CRITICAL/HIGH/MEDIUM.** Independently verified: the tool loop is strictly nested `if a.Tools { if a.SupportsFC { loop } return degraded }` — a `tools:true` agent with `SupportsFC=false` cannot reach `invokeToolLoop`; `SupportsFC` defaults false (undeclared model degrades safely); registry is sole source (no runtime probe); fallback capability is per-agent (`buildFallbackAgent` sets `SupportsFC: ac.SupportsFC` from the fallback's OWN config while inheriting lane `Tools` from the primary); capability checked once per agent invocation (not per loop turn); model-not-in-registry fails fast at build; non-boolean YAML rejected by the strict decoder; 1.x byte-identical via `statusFor` gating all tool fields behind `r.Tools`.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | registry/config.go decode | A non-boolean `supports_function_calling` (e.g. `"yes"`, `1`) is rejected by the strict YAML decoder as a generic type error rather than a field-named message (same as the existing `tools`/`role` reserved fields). Safe (no silent grant), just less ergonomic. | DEFERRED → TD-008 (registry-wide ergonomics; applies to all bool fields, not degradation-specific). |

   **Action taken:** No CRITICAL/HIGH/MEDIUM → no blocking pre-4.3 fix. The two LOW rows were confirmations-of-correctness; the one actionable item (field-named decode-error hint) is a pre-existing registry-wide ergonomics nit applying to all bool fields, deferred to `tech-debt-captured.md` (TD-008) rather than fixed inline (out of Story 4 scope). **Adversarial review passed.**

### 4.3 [x] **[Story 4: Graceful Degradation - REFACTOR](plan/user-stories/04-graceful-degradation.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A (if any)
   2. Improve naming: `ToolsDegraded` flag surfacing, registry lookup clarity (T1)
   3. Validate all tests still pass (T3)
   4. COMMIT: `git commit -m "refactor(fanout): address review + clean up degradation path"`
   **Duration:** 1 hour

   **Completed (2026-06-13):** 4.2.A surfaced no CRITICAL/HIGH/MEDIUM (the two LOW rows were confirmations-of-correctness; one ergonomics nit deferred to TD-008), so there were no required fixes. The GREEN code was already clean — the capability gate is a single nested branch mirroring the existing harness-degrade structure, with thorough comments, and `SupportsFC`/`ToolsRequested` thread through the same buildAgent/statusFor seams as the 1.x tool fields. No code change was warranted; an empty/cosmetic refactor commit was intentionally NOT created (minimum-footprint). T3 full suite green (`go test ./...` — all packages pass).

### 4.4 [x] **Phase 4 DoD**
   - [x] `go test ./internal/fanout/... ./internal/registry/...` — all passing
   - [x] `go test -coverprofile=coverage.out ./...` — ≥80% (total 87.4%; fanout 86.7%, registry 87.6%)
   - [x] `golangci-lint run` — no errors (0 issues)
   - [x] `go vet ./...` — clean
   - [x] `go build ./...` — succeeds
   - [x] Story 4 ACs verified: degrade path (04-01), tool-capable path (04-02), fallback inheritance (04-03), mixed roster reconciler (04-04)

   ```
   Phase-4 DoD Complete
   Auto: 5/5 | Story-Specific: 4/4 (Story 4)
   Manual Review: [x] Code reviewed (4.2.A adversarial subagent — no CRITICAL/HIGH)
   ```

### 4.5 [x] **Phase 4 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4 (`internal/fanout/engine.go` degrade sections, `internal/fanout/review.go`, `internal/fanout/status.go`, `internal/registry/`)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `AgentStatus.ToolsDegraded` field consumable by Phase 5 transcript writer?
       - CONFIG SURFACE: `supports_function_calling` registry field documented with default (`false`)?
       - INTEGRATION: Fallback inheritance doesn't break existing `review.go` logic?
       - PHASE-EXIT CONTRACT: Phase 5 can read `ToolsDegraded` from `AgentStatus` without changes?
       - REGRESSION: Phase 2-3 multi-turn path and 1.x single-shot path both untouched?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate findings (2026-06-13):** Fresh hostile-integrator subagent reviewed all Phase 4 production files (`registry/config.go`, `fanout/engine.go`, `review.go`, `status.go`, `artifacts.go`, `loop.go`). **No CRITICAL/HIGH/MEDIUM.** Independently verified `go build`/`go vet`/`go test ./...` all clean. All five exit-contract checks PASS:
   - CONTRACT EXIT / PHASE-EXIT: `AgentStatus.ToolsDegraded`/`ToolsRequested` are plain serialized fields mapped in `statusFor`; Phase 5 reads them with no struct/`invokeAgent`-signature change.
   - CONFIG SURFACE: `supports_function_calling` present (config.go), value-bool default false, absent field = false with no load error (back-compat test confirms; project overlay re-declares own capability — no stale inheritance).
   - INTEGRATION: `buildFallbackAgent` inherits lane `Tools/MaxTurns/ToolBudgetBytes` from primary but uses the fallback's OWN `SupportsFC` (AC 04-03), test-covered.
   - REGRESSION: multi-turn loop and 1.x single-shot both intact; 1.x status.json byte-identical (tool fields gated by `r.Tools`; omit test proves it).

   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | status.go ToolsRequested | `omitempty` means Phase 5 cannot distinguish `false` from absent for `tools_requested`; safe as long as Phase 5 reads it only inside the `Tools==true`/non-nil-counters block (every live path that sets `Tools=true` also sets `ToolsRequested=true`). | No code change — by design; Phase 5 reads tool fields only when tool counters are present. |
   | LOW | engine.go invokeSingleShot | The single-shot path stamps `ToolsRequested = a.Tools` on every call (writes `false` for 1.x agents); `statusFor` gates emission behind `r.Tools`, so 1.x status.json stays byte-identical (proven by TestInvokeAgent_SingleShotStatusOmitsToolFields). | No change — correct and test-covered. |

   **Action taken:** No CRITICAL/HIGH/MEDIUM → nothing to fix before the boundary. Both LOW rows are confirmations-of-correctness the reviewer explicitly marked "no code change required" (not defects, not deferred work). **Phase gate passed.** Phase 5 wiring contract: read `ToolsDegraded`/`ToolsRequested` from `AgentStatus` (already serialized); inject the `TranscriptWriter` into `toolLoop` (the live `l.messages` stream is loop-internal) as additive observer plumbing.
   **Duration:** 15-30 min

---

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 5: Integration (Days 9-10)

**Goal:** Transcript writer, live status counters, manifest review stage, persona updates, and `PayloadContext.ToolsEnabled`.

### 5.1 [x] **[Story 5: Transcript & Accounting - RED](plan/user-stories/05-transcript-accounting.md)**
   Write comprehensive failing tests, verify fail correctly
   - Test `TranscriptWriter.RecordToolCalls(turn, toolCalls)`: appends JSON line to `raw/<agent>/transcript.jsonl`
   - Test `TranscriptWriter.RecordToolResults(turn, results)`: appends results, truncated if over cap with marker
   - Test `TranscriptWriter.RecordFinal(message)`: appends final message
   - Test transcript durability: I/O errors are logged and non-fatal (best-effort append-only)
   - Test transcript replay: reading back `transcript.jsonl` reconstructs the full Chat call sequence
   - Test `status.json` counters: `turns`, `tool_calls`, `tool_bytes` updated live and match actual run values
   - Test `manifest.json` review stage: `"review"` entry lists agents with `tools: true`
   **Files:** `internal/tools/transcript_test.go`, `internal/fanout/status_test.go`, `internal/fanout/manifest_test.go` | **Duration:** 2-3 hours
   **AC:** [05-01](plan/acceptance-criteria/05-01-transcript-event-emission.md), [05-02](plan/acceptance-criteria/05-02-transcript-durability-replay.md), [05-03](plan/acceptance-criteria/05-03-live-status-counters.md), [05-04](plan/acceptance-criteria/05-04-manifest-review-stage.md)

### 5.2 [x] **[Story 5: Transcript & Accounting - GREEN](plan/user-stories/05-transcript-accounting.md)**
   Minimal code to pass tests, one test at a time (T1), verify all pass (T2), COMMIT
   - `internal/tools/transcript.go`: `TranscriptWriter` with `RecordToolCalls`, `RecordToolResults`, `RecordFinal`; buffered JSONL writer; flush per turn; best-effort I/O (errors logged, never fatal)
   - `internal/fanout/status.go`: `turns`, `tool_calls`, `tool_bytes` counters incremented in loop; written to `status.json` at end of each turn
   - `internal/fanout/manifest.go`: Add `"review"` stage entry listing agents with `tools: true` at invocation time
   COMMIT: `git commit -m "feat(tools): add transcript writer and status/manifest accounting (green)"`
   **Files:** `internal/tools/transcript.go`, `internal/fanout/status.go`, `internal/fanout/manifest.go` | **Duration:** 3-4 hours

### 5.2.A [x] **[Story 5: Transcript & Accounting - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-transcript-accounting.md)**
   **Changed Files:** `internal/tools/transcript.go`, `internal/fanout/status.go`, `internal/fanout/manifest.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 5.2.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.2 Transcript & Accounting`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FILES FROM 5.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? (transcript path traversal injection? tool results containing sensitive data written to disk unredacted?)
       - EDGE CASES: Null, empty, boundaries, concurrent access? (concurrent writes to transcript from multiple goroutines, empty `tool_calls`, zero `tool_bytes` counter?)
       - ERROR HANDLING: Missing catches, swallowed errors? (file create fails, disk full, write fails mid-session — is transcript still closed cleanly?)
       - PERFORMANCE: N+1, leaks, blocking ops? (unbuffered write per event, file descriptor leak on error path?)
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (2026-06-13):** Files reviewed: `transcript.go`, `replay.go`, `manifest.go`, `loop.go`, `engine.go`, `review.go`. **No CRITICAL/HIGH.** Independently verified: model-controlled tool names/IDs are JSON-escaped (no JSONL line injection), agent names validated-unique + path-sanitized upstream (no `agent+".jsonl"` traversal), the FD is closed on every `run()` exit via `finalize()` (no leak), tool-result content is byte-capped before the loop (no unbounded disk write), `atomicWriteFile` cleans up its temp on every error path. Unredacted tool-result content on disk is by-design operator-owned observability (no worse than existing payload artifacts) — not flagged.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | loop.go toolCallRecords + transcript.go writeEvent | A `tool_calls` turn where ANY call has malformed/invalid-JSON arguments fails `json.Marshal` (invalid `RawMessage`), so the ENTIRE `tool_calls` event is dropped — including well-formed sibling calls — leaving orphan `tool_result` lines with no matching request. | FIXED 5.3: `toolCallRecords` pre-validates each `Arguments` with `json.Valid` and wraps malformed bytes as a JSON string, so the per-call record always marshals and the turn is fully recorded. |
   | LOW | review.go reviewStageFor | `Agents` and `ToolsEnabled` share the same backing slice (aliasing); a future mutation of one silently mutates the other. | FIXED 5.3: `Agents` gets its own copy via `append([]string(nil), enabled...)`. |
   | LOW | transcript.go writeEvent/flush | `bufio.Writer` latches its first error → a long run after the disk fills logs one warning per remaining event (log flooding). Behavior correct (swallowed, never fatal), just noisy. | FIXED 5.3: a `failed` flag logs once then silences subsequent writes; `Close()` still releases the FD regardless of `failed`. |
   | LOW | review.go ExecuteReview | Production `ExecuteReview` wires neither `WithDispatcher` nor `WithTranscript` → the transcript/dispatcher are dead in the real flow until Phase 6 e2e wires them. | DEFERRED — by design (scope boundary): Phase 6 (6.1) wires `SnapshotFor→NewJail→NewDispatcher→WithDispatcher` + the per-agent transcript factory. Tracked via TD-004 / Phase 6. |

   **Action taken:** No CRITICAL/HIGH → no blocking pre-5.3 fix. The MEDIUM (transcript faithfulness) and two cheap LOW (slice aliasing, log flooding) fixed inline in 5.3 REFACTOR; the production-wiring LOW is the documented Phase 6 scope boundary, deferred. **Adversarial review passed.**

### 5.3 [x] **[Story 5: Transcript & Accounting - REFACTOR](plan/user-stories/05-transcript-accounting.md)**
   1. Fix CRITICAL/HIGH issues from 5.2.A (if any)
   2. Improve I/O safety: ensure file handles closed on all paths, proper error logging (T1)
   3. Validate all tests still pass (T3)
   4. COMMIT: `git commit -m "refactor(tools): address review + clean up transcript writer"`
   **Duration:** 1 hour

### 5.4 [x] **[Story 6: Persona Guidance & Documentation - RED](plan/user-stories/06-persona-guidance-documentation.md)**
   Write comprehensive failing tests, verify fail correctly
   - Test `PayloadContext.ToolsEnabled bool`: field set from `AgentConfig.Tools` at render time
   - Test persona template rendering: `{{if .ToolsEnabled}}` sections present when `ToolsEnabled=true`, absent when `false`
   - Test evidence-citation rule present in tool-enabled persona output
   - Test scope guard present: tools widen evidence gathering, not scope
   - Test `docs/registry.md` documents `tools`, `max_turns`, `tool_budget_bytes` as active fields
   - Test `docs/payload-modes.md` has payload-as-starting-point semantics section
   **Files:** `internal/payload/personas_render_test.go` | **Duration:** 1-2 hours
   **AC:** [06-01](plan/acceptance-criteria/06-01-tool-enabled-persona-guidance.md), [06-02](plan/acceptance-criteria/06-02-evidence-citation-rule.md), [06-03](plan/acceptance-criteria/06-03-registry-documentation-activation.md), [06-04](plan/acceptance-criteria/06-04-payload-modes-readme-cost-guidance.md)

### 5.5 [x] **[Story 6: Persona Guidance & Documentation - GREEN](plan/user-stories/06-persona-guidance-documentation.md)**
   Minimal code to pass tests, one test at a time (T1), verify all pass (T2), COMMIT
   - `internal/payload/template.go`: Add `ToolsEnabled bool` to `PayloadContext`; populate from `AgentConfig.Tools`
   - Persona templates: add `{{if .ToolsEnabled}}` conditional sections with tool exploration guidance, evidence-citation rule ("findings must cite evidence actually read"), scope guard ("tools widen evidence gathering, not scope")
   - `docs/registry.md`: Document `tools`, `max_turns` (default 10, bounds), `tool_budget_bytes` as active fields with validation
   - `docs/payload-modes.md`: Add payload-as-starting-point semantics section for tool agents
   - `README.md`: Add 3–10× cost guidance for tool-enabled agents
   COMMIT: `git commit -m "feat(payload): add tool-enabled persona guidance and documentation (green)"`
   **Files:** `internal/payload/template.go`, persona templates, `docs/registry.md`, `docs/payload-modes.md`, `README.md` | **Duration:** 2-3 hours

### 5.5.A [x] **[Story 6: Persona Guidance & Documentation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-persona-guidance-documentation.md)**
   **Changed Files:** `internal/payload/template.go`, persona templates, `docs/registry.md`, `docs/payload-modes.md`, `README.md`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 5.5.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.5 Persona Guidance & Documentation`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FILES FROM 5.5]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? (`ToolsEnabled=false` still renders tool guidance sections? missing cost guidance could cause runaway spend?)
       - EDGE CASES: Null, empty, boundaries, concurrent access? (template parse errors on missing `ToolsEnabled` field, zero-length persona output?)
       - ERROR HANDLING: Missing catches, swallowed errors? (template render error swallowed silently?)
       - PERFORMANCE: N+1, leaks, blocking ops? (template re-parsed per render instead of cached?)
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (2026-06-13):** Fresh hostile reviewer checked all 7 personas + 3 docs against the implementation. **No CRITICAL/HIGH.** Independently verified: `ToolsEnabled=false` renders NO tool guidance (tests confirm); no persona instructs write/exfiltrate/jail-escape (read-only `read_file`/`grep`/`list_files`, "verify, not browse"); docs match `internal/registry/config.go` validation (`tools` default false, `max_turns` default 10 only when `tools:true`, `tool_budget_bytes >=0`/0=unlimited, `supports_function_calling` bool default false); payload-modes/README tool set accurate (read-only, path-jailed, no shell/network); README "typically 3-10×" present + hedged; all 7 files carry the identical block; `RenderPrompt` surfaces errors via `*RenderError` (no swallowing).
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | personas/*.md (tool block) | The `{{if}}`/`{{end}}` on their own lines leave dangling blank lines in both render states (cosmetic; tests pass). | FIXED 5.6: moved the block markers inline (`{{if .ToolsEnabled}}## Tool-Assisted Review` … `{{end}}## Severity Rubric`) so the `ToolsEnabled=false` render is byte-identical to 1.0 (strengthens AC 06-01 S3) and the true render has clean spacing. |
   | LOW | docs/registry.md max_turns row | Documented validation as only "must be `> 0`"; actual rule is `1..1000` (`MaxAgentTurns` hard cap at `precedence.go:15`). | FIXED 5.6: row now reads "must be within `1..1000`" with the runaway-loop backstop noted. |

   **Action taken:** No CRITICAL/HIGH → no blocking pre-5.6 fix. Both cheap LOW fixed inline in 5.6 REFACTOR (the whitespace fix directly serves AC 06-01 S3; the doc bound aligns docs with validation). **Adversarial review passed.**

### 5.6 [x] **[Story 6: Persona Guidance & Documentation - REFACTOR](plan/user-stories/06-persona-guidance-documentation.md)**
   1. Fix CRITICAL/HIGH issues from 5.5.A (if any)
   2. Improve documentation clarity: cost guidance phrasing, registry field descriptions (T1)
   3. Validate all tests still pass (T3)
   4. COMMIT: `git commit -m "refactor(payload): address review + clean up persona guidance"`
   **Duration:** 1 hour

### 5.7 [x] **Phase 5 DoD**
   - [x] `go test ./...` — all passing
   - [x] `go test -coverprofile=coverage.out ./...` — ≥80% (total 87.5%; tools 85.3%, fanout 87.4%, payload 89.7%; pre-existing `internal/mcp` 78.8% is untouched by Phase 5)
   - [x] `golangci-lint run` — no errors (0 issues)
   - [x] `go vet ./...` — clean
   - [x] `go build ./...` — succeeds
   - [x] Story 5 ACs verified: transcript events (05-01), durability + replay (05-02), live status counters (05-03 — write-once-at-completion per the binding AC), manifest review stage (05-04)
   - [x] Story 6 ACs verified: persona guidance (06-01), evidence-citation + scope guard (06-02), registry docs active (06-03), payload-modes + README cost guidance (06-04)

   ```
   Phase-5 DoD Complete
   Auto: 5/5 | Story-Specific: 8/8 (Stories 5 + 6)
   Manual Review: [x] Code reviewed (5.2.A + 5.5.A adversarial subagents — no CRITICAL/HIGH)
   ```

### 5.8 [x] **Phase 5 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 5 (`internal/tools/transcript.go`, `internal/fanout/status.go`, `internal/fanout/manifest.go`, `internal/payload/template.go`, persona templates, docs)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 5 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 5 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Transcript writer interface, status.json schema, manifest stage entry all consumable by Phase 6 end-to-end tests?
       - CONFIG SURFACE: `ToolsEnabled` field documented? Persona template conditional sections discoverable?
       - INTEGRATION: Status counters wired into loop? Transcript writer initialized and closed per agent invocation?
       - PHASE-EXIT CONTRACT: Phase 6 can exercise transcript replay and end-to-end test without additional wiring?
       - REGRESSION: Existing persona rendering for non-tool agents (`ToolsEnabled=false`) unaffected?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate findings (2026-06-13):** Fresh hostile-integrator subagent reviewed all Phase 5 production files + docs and ran `go build`/`go vet`/full `go test ./...` (all clean/green, uncached). **No CRITICAL/HIGH/MEDIUM/LOW.**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | NONE | tools/transcript.go, replay.go; fanout/loop.go, engine.go, review.go; payload/manifest.go; personas | All five exit-contract checks PASS. **CONTRACT EXIT:** `OpenTranscript`/`Record*`/`Close` nil-safe + best-effort (disabled no-op on open failure, latched `failed`, `Close` always releases FD); `ReplayTranscript` resilient (absent=empty, malformed/missing/unknown skipped+counted); status.json counters pointer+omitempty (1.x byte-identical, explicit zeros only for tool agents); `Manifest.Review` omitempty sibling of `Stages`, slices normalized to `[]`. **INTEGRATION:** counters wired (`Turns++`/`ToolCalls++`/`ToolBytes+=`), transcript opened once per `invokeToolLoop` and `Close`d on EVERY exit via `finalize` (no FD leak/double-close); `reviewStageFor` keys on `ToolsRequested` (preserved across degrade/trip/error), `Agents` non-aliasing copy. **REGRESSION:** `{{if .ToolsEnabled}}` false-render byte-identical to 1.0 (git diff confirms), all prior tests pass. |
   | INFO | engine.go invokeSlot attribution | Phase-6 wiring note (NOT a defect): the transcript factory receives `a.Name`, which for a *fallback* agent is the fallback's own name while status.json attribution rewrites to the primary (engine.go invokeSlot). Phase 6 owns the production factory + transcript-dir naming and must reconcile this keying (e.g. key the per-agent transcript dir on the slot/primary name) when wiring `WithTranscript` into `ExecuteReview`. Captured for Phase 6 task 6.1. |

   **Action taken:** No CRITICAL/HIGH/MEDIUM/LOW → nothing to fix before the boundary. The fallback-attribution keying note is captured for Phase 6 (6.1) production wiring. **Phase gate passed.** Phase 6 wiring contract: add `WithTranscript(func(agent) tools.OpenTranscript(<poolDir>/raw/agent/<dir>/transcript.jsonl, agent))` + `WithDispatcher(NewDispatcher(NewJail(SnapshotFor(head).Root()), DefaultLimits()))` into `ExecuteReview`'s `NewEngine`, keying the transcript path on the slot/primary agent dir.
   **Duration:** 15-30 min

---

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Final Phase: Testing & Validation (Days 11-13)

**Goal:** End-to-end integration tests, documentation completeness, registry activation, and regression verification.

### 6.1 [x] **End-to-End Integration Test (Fixture Repo)**
   **Completed (2026-06-13):** Wired the production tool harness into `ExecuteReview` — when any slot is tool-enabled it builds `SnapshotManager.SnapshotFor(head)` → `NewJail(root)` → `NewDispatcher(jail, DefaultLimits())` and a per-agent transcript factory under `raw/agent/<dir>/transcript.jsonl`, all best-effort (snapshot/jail failure logs and degrades). `PreparedReview` gained `Repo`/`Head`. E2E test (`engine_e2e_test.go`): a fixture repo where `head` changes `auth.go` (payload) while `helper.go` stays unchanged; an httptest mock scripts a 2-turn exchange (read_file `helper.go` + grep `func b` → final finding). Asserts: agent succeeds, finding produced, status.json counters (`turns>=2`, `tool_calls==2`, `tool_bytes>0`), transcript replays `[tool_calls, tool_result, tool_result, final]`, the read_file result carries `helper.go` content (file outside the payload), and manifest `review` lists the agent. Second test proves the degrade path through the real flow (incapable model → `tools_degraded`, 0 turns). Full suite green, lint 0 issues.
   1. Create a fixture repository with Go source files covering a realistic review scenario
   2. Write end-to-end test: tool-enabled agent reads a file outside the payload, greps for callers, produces findings citing that evidence
   3. Use httptest mock provider scripting multi-turn `tool_calls` exchanges with the fixture repo
   4. Assert: findings reference file paths actually read; `transcript.jsonl` replays faithfully; `status.json` counters non-zero
   **Files:** `internal/fanout/engine_e2e_test.go` (or `//go:build integration`) | **Duration:** 4-5 hours

### 6.2 [x] **Budget Trip & Scenario Tests**
   **Completed (2026-06-13):** Each listed scenario is covered. (1) Three budgets trip independently with `tripped_budgets` recorded — `engine_budget_test.go` (14 tests: max_turns trip/one/default, byte trip/not/exactly-met/oversize-then-trip/unlimited/tool-error-no-bytes, timeout first-Chat/mid-loop/during-tool/precedence) + `status_tools_test.go` serialization. (2) Partial-success — budget tests return the final partial content; NEW artifact-layer test `TestExecuteReview_BudgetTripRecordedInStatusAndPartialFindings` proves the on-disk `status.json` records `max_turns` AND the partial finding reaches `findings.txt` through `RunReview`. (3) Path jail escape vectors (absolute, `..`, symlink-escape, `.git/`) — `internal/tools/jail_test.go` (table-driven, Phase 2 DoD verified all vectors rejected). (4) Degrade path with `tools_degraded:true` — `engine_degrade_test.go` + `TestExecuteReview_ToolAgentDegradesWhenIncapable` (on-disk status.json). (5) Transcript replay reconstructs the exact sequence — `replay_test.go` + `TestExecuteReview_ToolAgentEndToEnd` (replays `[tool_calls, tool_result, tool_result, final]`). Only the artifact-layer budget-trip gap needed a new test; the rest was already comprehensively covered.
   1. Test all three budgets trip cleanly and independently; `status.json` records `tripped_budget` correctly for each
   2. Test partial-success semantics: partial findings returned and usable when budget trips
   3. Test path jail escape vectors (comprehensive): absolute, `..`, symlink-escape, `.git/` — all rejected
   4. Test non-tool-capable model degrade path with `tools_degraded: true` in status.json
   5. Test transcript replay harness reconstructs exact Chat call sequence from `transcript.jsonl`
   **Files:** existing test files + additional integration test cases | **Duration:** 3-4 hours

### 6.3 [x] **Documentation Completeness & Registry Activation**
   **Completed (2026-06-13):** (1-3) docs completeness verified by the Story 6 `TestDocs_*` tests — `docs/registry.md` documents `tools`/`max_turns` (default 10 when tools:true, `1..1000`)/`tool_budget_bytes` (`0 = unlimited`)/`supports_function_calling` as active with defaults+validation+backward-compat note; `docs/payload-modes.md` has the payload-as-starting-point tool-agent section + scope rule; `README.md` has the `3-10×` cost guidance and links `docs/registry.md`. (4) Registry validation activation: the fields were already parsed + validated (Phase 1) and acted upon by the engine (Phase 3/4 — `applyDefaults(max_turns=10 when tools:true)`, `SupportsFC` capability gate); flipped the stale `reserved/inert in 1.x` doc comments in `internal/registry/config.go` to reflect that `tools`/`max_turns`/`tool_budget_bytes`/`supports_function_calling` are **active in 2.0** (Role remains reserved for Stage 3/4). No behavior change — the activation landed in earlier phases; this aligns the code comments with the validation and docs.
   1. Verify `docs/registry.md`: `tools`, `max_turns`, `tool_budget_bytes` documented as active with defaults, bounds, validation
   2. Verify `docs/payload-modes.md`: payload-as-starting-point semantics section complete
   3. Verify `README.md`: 3–10× cost guidance present, tool-using reviewer workflow documented
   4. Flip registry validation: activate `tools`, `max_turns`, `tool_budget_bytes` fields from reserved to active in registry validation code
   COMMIT: `git commit -m "docs: complete registry, payload-modes, README documentation + activate registry fields"`
   **Duration:** 2-3 hours

### 6.4 [x] **Final Regression Check**
   **Completed (2026-06-13):** (1) Full `go test ./...` green — no 1.x regressions (the engine wiring is gated behind `anyToolAgent`, so non-tool reviews take the unchanged path; `TestRunReview_*` all pass). (2) Mixed roster verified end-to-end: `TestExecuteReview_MixedRosterReconcilesBoth` runs one tool-loop agent + one 1.x single-shot agent in one review — the pool consumes both result shapes, the tool agent emits counters + a transcript, and the non-tool agent's status.json is byte-clean of tool fields. (3) No new third-party dependencies: `git diff main -- go.mod go.sum` is empty (only stdlib + internal packages added). Coverage total 87.5% (≥80%); lint 0 issues; vet/build clean.
   1. Confirm all 1.x single-shot paths still pass: `go test ./...` with no regressions against prior test suite
   2. Verify mixed roster: tool and non-tool agents in one review; reconciler consumes both result shapes identically
   3. Confirm no new third-party dependencies: inspect `go.mod` for any additions
   4. Final commit: `git commit -m "test(fanout): final regression + mixed roster verification"`
   **Duration:** 1-2 hours

---

### Validation Checklist
- [x] All tests passing (T3): `go test ./...`
- [x] Coverage meets threshold: `go test -coverprofile=coverage.out ./...` ≥80% (total 87.5%)
- [x] Lint/format clean: `golangci-lint run` (0 issues) + `go vet ./...`
- [x] Build succeeds: `go build ./...`
- [x] No new third-party dependencies: `git diff main -- go.mod go.sum` empty
- [x] All sprint success criteria met (see Sprint Overview)
- [x] Drift check against [original-requirements.md](plan/original-requirements.md): confirm all deliverables present

### Drift Analysis
Compare delivered implementation against [original-requirements.md](plan/original-requirements.md):
- [x] Multi-turn agent loop with `tool_calls` handling ✓ (Phase 3 `internal/fanout/loop.go`; e2e in 6.1)
- [x] `read_file`, `grep`, `list_files` tools with stated signatures and limits ✓ (Phase 2 `internal/tools/`)
- [x] Path jail: absolute, `..`, symlink-escape, `.git/` all rejected ✓ (Phase 2 `jail_test.go`)
- [x] Snapshot manager: live-worktree fast path, `git worktree` slow path, cleanup ✓ (Phase 2 `snapshot.go`; wired into `ExecuteReview` in 6.1)
- [x] `max_turns` (default 10), `tool_budget_bytes`, `timeout_secs` budgets ✓ (Phase 3 `engine_budget_test.go`; artifact-layer trip in 6.2)
- [x] `tools_degraded: true` in status.json for non-tool-capable models ✓ (Phase 4; e2e `TestExecuteReview_ToolAgentDegradesWhenIncapable`)
- [x] `transcript.jsonl` per tool-using agent with complete event sequence ✓ (Phase 5 `internal/tools/transcript.go` + replay; e2e replays the full sequence)
- [x] No new third-party dependencies ✓ (`go.mod`/`go.sum` unchanged from main)
- [x] `docs/registry.md`, `docs/payload-modes.md`, `README.md` updated ✓ (Story 6; `TestDocs_*`)
