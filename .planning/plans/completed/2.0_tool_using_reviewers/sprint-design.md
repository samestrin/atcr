# Sprint Design: Tool-Using Reviewers

**Created:** June 13, 2026
**Plan:** [2.0: Tool-Using Reviewers](.planning/plans/active/2.0_tool_using_reviewers/)
**Plan Type:** ✨ Feature
**Status:** Design Complete

---

## Original User Request

> Turn the pool reviewers from single-shot prompted calls into bounded agents: each reviewer can explore the repository through read-only, path-jailed tools (`read_file`, `grep`, `list_files`) exposed via OpenAI-compatible function calling, with the Go engine owning the entire tool harness. The payload becomes the starting point of a review, not the universe.

**Referenced Resources:**
- [Epic 2.0: Tool-Using Reviewers](.planning/epics/active/2.0_tool_using_reviewers.md)
  - **Summary**: Epic plan transforming single-shot reviewers into bounded multi-turn agents with read-only tool access
  - **Key Points**: Multi-turn agent loop, path-jailed sandbox, three per-agent budgets (turns, bytes, timeout), graceful degradation for non-tool-capable models

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Tool-Using Agents
**Complexity:** 10/12 (VERY COMPLEX)
**Timeline:** 13 days
**Phases:** 6
**Pattern:** Research & Spike → Foundation → Core Items → Advanced → Integration → Testing → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
multi-turn agent loop Go
OpenAI function calling wire format
path jail symlink escape vectors
git worktree snapshot manager
tool dispatcher read-only enforcement
httptest mock provider multi-turn
transcript.jsonl JSONL replay
budget enforcement max_turns tool_budget_bytes
```

---

## Complexity Breakdown

- **Architecture:** 3/3 - New multi-turn agent loop transforms core invocation model from single-shot to bounded agent; new tool harness package with dispatcher, path jail, snapshot manager; integrates with existing fanout engine via ChatCompleter interface
- **Integration:** 2/3 - 3+ integrations: llmclient (wire format), fanout engine (loop), registry (config activation), artifacts (transcript/status/manifest), payload (persona context); all internal to Go binary but touches multiple subsystems
- **Story/Task & Test:** 3/3 - 7 user stories, 30 acceptance criteria; extensive testing: unit tests (no LLM), httptest mock providers, integration tests with fixture repos; 7 high-complexity ACs
- **Risk/Unknowns:** 2/3 - Provider function-calling dialect variance, small model thrashing behavior, worktree edge cases; mitigations exist (degrade path, loop hygiene, conservative defaults) but not fully proven in production

**Time Formula:** Base 5 days + (Complexity - 4) * 2 days + (Phases - 3) * 1 day
**Calculation:** 5 + (10 - 4) * 2 + (6 - 3) * 1 = 5 + 12 + 3 = 20 → clamped to 13 days (VERY COMPLEX range: 13+ days)

---

## Recommended Flags

**Adversarial:** true (complexity 10/12 >= 6, phases 6 >= 3)
**Gated:** true (complexity 10/12 >= 8, phases 6 >= 5, duration 13 > 5)
**Recommendation strength:** strong (complexity 10/12 >= 10)
**Suggested command:** `/create-sprint @.planning/plans/active/2.0_tool_using_reviewers/ --gated`

---

## Phase Structure

### Phase 1: Research & Spike (Day 1)
**Duration:** 1 day
**Focus:** Validate OpenAI function-calling wire format with litellm, probe provider dialect variance, spike on path jail implementation

**Items:**
- Spike on OpenAI `tools`/`tool_calls`/`role:tool` wire format via httptest mock
- Test litellm normalization of function-calling dialects (OpenAI, Anthropic, local models)
- Prototype path jail with `filepath.EvalSymlinks`, `O_NOFOLLOW`, `.git` component matching
- Validate `git worktree add` / `git worktree remove --force` lifecycle
- Document findings and risks before foundation phase

**Success Criteria:**
- Wire format spike confirms tool_calls round-trip through at least one provider
- Path jail prototype rejects absolute, `..`, symlink-escape, `.git/` vectors
- Git worktree lifecycle tested with clean and dirty worktrees

---

### Phase 2: Foundation (Days 2-3)
**Duration:** 2 days
**Focus:** Tool harness (definitions, dispatcher, path jail), snapshot manager, unit tests with no LLM

**Items:**
- **Tool definitions** (internal/tools/defs.go): `read_file`, `grep`, `list_files` as OpenAI JSON Schema
- **Path jail** (internal/tools/jail.go): Resolve method with full escape-vector rejection (absolute, `..`, symlinks, `.git/`)
- **Tool dispatcher** (internal/tools/dispatch.go): Routes tool calls to handlers, enforces per-call byte caps, returns truncated results
- **Tool handlers** (internal/tools/read_file.go, grep.go, list_files.go): Implement line-numbered reads, regex search, depth-capped listings
- **Snapshot manager** (internal/tools/snapshot.go): `SnapshotFor(head)` with live-worktree fast path and temporary `git worktree add` slow path
- **Unit tests** for all of the above (no LLM, no network, fixture-based)

**Success Criteria:**
- All path jail escape vectors rejected in unit tests
- Tool dispatcher enforces per-call byte caps with truncation markers
- Snapshot manager returns valid root for any head SHA, cleans up temporary worktree
- All tests pass without network access or LLM

---

### Phase 3: Core Items (Days 4-6)
**Duration:** 3 days
**Focus:** ChatCompleter interface, llmclient wire format, multi-turn agent loop, budget enforcement

**Items:**
- **ChatCompleter interface** (internal/fanout/engine.go): `Chat(ctx, inv, messages, tools)` alongside existing `Completer`
- **llmclient wire format** (internal/llmclient/client.go): `tools` array in request, `tool_calls` in response, `role:tool` messages
- **Agent loop** (internal/fanout/engine.go): `invokeAgent` branches on `Agent.Tools`, drives multi-turn loop, executes tool_calls via dispatcher, appends results, checks budgets
- **Budget enforcement**: `max_turns` (default 10), `tool_budget_bytes`, `timeout_secs` (via context.WithTimeout)
- **Loop hygiene**: repeated tool call nudge, malformed JSON retry, tool execution error as tool result
- **Result struct** (internal/fanout/engine.go): `Turns int`, `ToolCalls int`, `ToolBytes int64`
- **Integration tests** with httptest mock providers scripting multi-turn tool_calls exchanges

**Success Criteria:**
- Agent loop completes multi-turn review with tool calls and results
- All three budgets enforce independently and in combination
- Loop hygiene rules enforced (nudge, retry, error-as-result)
- Result struct populated with accurate counters
- All tests pass with scripted httptest providers

---

### Phase 4: Advanced (Days 7-8)
**Duration:** 2 days
**Focus:** Graceful degradation, fallback inheritance, mixed roster compatibility

**Items:**
- **Registry activation**: `supports_function_calling` field per model/provider in registry.yaml
- **Degrade path** (internal/fanout/engine.go): non-tool-capable model with `tools: true` executes single-shot, sets `tools_degraded: true`
- **AgentStatus** (internal/fanout/status.go): `ToolsDegraded bool` field
- **Fallback inheritance** (internal/fanout/review.go): fallback agents inherit effective tools setting from lane invocation
- **Mixed roster test**: tool-loop and single-shot agents coexist in one review, reconciler consumes both identically

**Success Criteria:**
- Non-tool-capable model with `tools: true` degrades to single-shot with `tools_degraded: true`
- Fallback agents inherit tools setting correctly
- Mixed roster review completes with both tool-loop and single-shot results
- Reconciler consumes both result shapes without special-casing

---

### Phase 5: Integration (Days 9-10)
**Duration:** 2 days
**Focus:** Transcript writer, status.json counters, manifest review stage, persona updates

**Items:**
- **Transcript writer** (internal/tools/transcript.go): JSONL emitter with `RecordToolCalls`, `RecordToolResults`, `RecordFinal` methods; append-only, best-effort I/O
- **Status counters** (internal/fanout/status.go): `turns`, `tool_calls`, `tool_bytes` updated live, written to status.json
- **Manifest review stage** (internal/fanout/manifest.go): `"review"` entry listing agents with `tools: true`
- **Persona updates**: `{{if .ToolsEnabled}}` conditional sections in persona templates; evidence-citation rule; scope guard
- **PayloadContext** (internal/payload/template.go): `ToolsEnabled bool` field set from `AgentConfig.Tools`

**Success Criteria:**
- Transcript.jsonl emitted per tool-using agent with complete event sequence
- Status.json counters match actual run values
- Manifest includes review stage with tool-enabled agents
- Persona templates render tool guidance when ToolsEnabled is true
- Evidence-citation rule stated in persona guidance

---

### Phase 6: Testing & Validation (Days 11-13)
**Duration:** 3 days
**Focus:** End-to-end integration tests, documentation, registry activation, final validation

**Items:**
- **End-to-end integration test**: fixture repo, tool-enabled agent reads file outside payload, greps for callers, produces findings citing evidence
- **Budget trip tests**: each budget trips cleanly, partial-success semantics hold, status.json records tripped budget
- **Path jail escape tests**: absolute, `..`, symlink-escape, `.git/` all rejected
- **Degrade path tests**: non-tool-capable model degrades, fallback inherits correctly
- **Transcript replay test**: reconstruct exact Chat call sequence from transcript.jsonl
- **Documentation**: docs/registry.md (active fields, defaults, validation), docs/payload-modes.md (payload-as-starting-point), README (3-10× cost guidance)
- **Registry activation**: flip reserved fields (`tools`, `max_turns`, `tool_budget_bytes`) to active with validation
- **Final validation**: all tests pass, all ACs covered, no regressions in 1.x single-shot path

**Success Criteria:**
- End-to-end test demonstrates multi-turn review with evidence citation
- All budget trips produce partial-success results with correct status.json markers
- Path jail rejects all escape vectors
- Degrade path works for non-tool-capable models and fallbacks
- Transcript replays faithfully
- Documentation complete and accurate
- Registry fields active and validated
- No regressions in existing tests

---

## Work Decomposition

### Story 1: Agent Loop Execution (6 ACs)
**Testable Elements:**
1. ChatCompleter interface and Chat method signature
2. Multi-turn loop execution with tool_calls handling
3. Per-agent budget enforcement (turns, bytes, timeout)
4. Loop hygiene (repeated call nudge, malformed JSON retry)
5. Degrade path for non-tool-capable models
6. Result struct accounting and backward compatibility

**AC Links:** 01-01 through 01-06

### Story 2: Budget Enforcement (4 ACs)
**Testable Elements:**
1. Turn budget enforcement with counter in status.json
2. Tool byte budget enforcement with deferred trip check
3. Timeout enforcement via context.WithTimeout
4. Budget status reporting and partial-success semantics

**AC Links:** 02-01 through 02-04

### Story 3: Path Jail & Snapshot Sandbox (4 ACs)
**Testable Elements:**
1. Path jail escape vector rejection (absolute, `..`, symlinks, `.git/`)
2. Snapshot manager lifecycle (live worktree fast path, temporary worktree slow path)
3. Worktree cleanup and manifest recording
4. Read-only enforcement and write tool guard

**AC Links:** 03-01 through 03-04

### Story 4: Graceful Degradation (4 ACs)
**Testable Elements:**
1. Single-shot degradation path for non-tool-capable models
2. Tool-capable agent loop path
3. Fallback degradation inheritance
4. Mixed roster reconciler compatibility

**AC Links:** 04-01 through 04-04

### Story 5: Transcript & Accounting (4 ACs)
**Testable Elements:**
1. Transcript event emission (tool_calls, tool_result, final)
2. Transcript durability and replay
3. Live status counters (turns, tool_calls, tool_bytes)
4. Manifest review stage entry

**AC Links:** 05-01 through 05-04

### Story 6: Persona Guidance & Documentation (4 ACs)
**Testable Elements:**
1. Tool-enabled persona guidance sections
2. Evidence-citation rule and scope guard
3. Registry documentation activation
4. Payload-modes semantics and README cost guidance

**AC Links:** 06-01 through 06-04

### Story 7: Tool Definitions & Dispatcher (4 ACs)
**Testable Elements:**
1. `read_file` line-numbered output, slicing, and byte-cap truncation
2. `grep` regex search, glob filter, and match-cap truncation
3. `list_files` depth-capped directory listing
4. Dispatcher routing and per-call byte caps

**AC Links:** 07-01 through 07-04

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** ./... (standard Go test pattern)
**Test File Placement Examples:**
- internal/tools/jail_test.go
- internal/tools/dispatch_test.go
- internal/tools/snapshot_test.go
- internal/tools/transcript_test.go
- internal/fanout/engine_test.go (agent loop, budgets, degrade path)
- internal/fanout/review_test.go (fallback inheritance)
- internal/llmclient/client_test.go (wire format)
- internal/payload/personas_render_test.go (ToolsEnabled rendering)

**Unit/Integration/E2E:**
- **Unit tests** (12 ACs): tool harness, tool handlers, path jail, snapshot manager, degrade path, transcript emission, status counters, manifest stage — all without LLM or network
- **Integration tests** (14 ACs): agent loop with httptest mock providers, budget enforcement, mixed roster, transcript replay, end-to-end with fixture repo
- **Documentation tests** (4 ACs): registry docs, payload-modes docs, README cost guidance, persona guidance
- **E2E tests** (0 ACs): not required for this epic

**Test Environment Status:**
- Framework: Go standard testing library with github.com/stretchr/testify/assert and require
- Execution: `go test ./...`
- Coverage Tools: `go test -coverprofile=coverage.out ./...` (baseline: 80%)

**Test Data Requirements:**
- Fixture repositories for path jail and snapshot tests
- httptest mock providers scripting multi-turn tool_calls exchanges
- Sample registry.yaml with `supports_function_calling` declarations
- Sample persona templates with `{{if .ToolsEnabled}}` sections

---

## Architecture

**Primitives:**
- **ToolDef**: OpenAI function-calling JSON Schema (name, description, parameters)
- **ToolCall**: ID, type, function name, arguments
- **ChatResponse**: Message (with potential ToolCalls), FinishReason
- **Message**: Role, Content, ToolCalls, ToolCallID
- **Jail**: Root path, Resolve method
- **Snapshot**: Root directory, cleanup function
- **AgentConfig**: Tools, MaxTurns, ToolBudgetBytes, TimeoutSecs
- **Result**: Turns, ToolCalls, ToolBytes
- **AgentStatus**: ToolsDegraded, Turns, ToolCalls, ToolBytes, TrippedBudgets

**Module Boundaries:**
- **internal/tools**: Tool definitions, dispatcher, path jail, snapshot manager, transcript writer — all tool-related concerns
- **internal/fanout**: Agent loop, budget enforcement, degrade path, result propagation — engine-layer concerns
- **internal/llmclient**: ChatCompleter interface, wire format (tools/tool_calls/role:tool) — provider communication
- **internal/registry**: Config parsing, validation, defaults, supports_function_calling declaration — configuration layer
- **internal/payload**: PayloadContext with ToolsEnabled, persona rendering — prompt layer
- **internal/artifacts**: status.json, manifest.json writers — observability layer

**External Dependencies:**
- Go stdlib only: os, regexp, path/filepath, io, context, time, encoding/json
- git worktree commands (already a dependency of atcr's test infrastructure)
- No new third-party dependencies (explicit epic constraint)

**Replaceability:**
- Tool dispatcher is replaceable: new tools can be added without changing the loop
- Snapshot manager is replaceable: alternative snapshot strategies (e.g., archive-based) can substitute
- ChatCompleter is replaceable: alternative wire formats or providers can implement the interface
- Path jail is replaceable: alternative sandboxing strategies (e.g., chroot, namespaces) can substitute
- All modules communicate through clean interfaces; implementation details are hidden

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| Path jail escape | Tool file access | Absolute paths, `..` traversal, symlink escape, `.git/` access | filepath.Abs/Clean/EvalSymlinks, O_NOFOLLOW, .git component matching, prefix check |
| Read-only enforcement | File opens, tool definitions | Write tool registration, file mutation | No write tool in registry, O_RDONLY in harness, init-time guard against write tool registration |
| Snapshot isolation | Worktree management, head SHA | Dirty worktree, submodule content, post-snapshot mutation | git status --porcelain check, fast-path only when clean, submodules unreadable in v1, documented threat model |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| Agent loop execution | Multi-turn Chat calls, tool dispatch | < 30s per review (typical) | Conservative max_turns=10, tool_budget_bytes cap, timeout_secs enforcement |
| Tool result byte tracking | Cumulative int64 sum per agent | < 1ms overhead per tool call | Running sum, end-of-turn trip check, no per-byte instrumentation |
| Transcript I/O | Append-only JSONL writes per turn | < 5ms per write | Buffered writer, flush per turn, non-fatal I/O errors |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Provider function-calling dialect | OpenAI, Anthropic, local models, litellm normalization | tool_calls round-trip correctly or degrade path activates |
| Small model thrashing | Repeated same call, ignored results, infinite loop | Loop hygiene: nudge once, halt and request final answer |
| Budget trip timing | Mid-tool-call, mid-Chat-call, end-of-turn | Defer to end-of-turn, current tool result delivered in full, partial-success semantics |
| Worktree dirty state | Staged, unstaged, untracked changes when head==HEAD | Fall through to temporary worktree path |
| Symlink race | Attacker swaps file for symlink between EvalSymlinks and Open | O_NOFOLLOW where supported, documented threat model (post-snapshot mutation out of scope) |
| Context cancellation | Timeout expires during Chat call | Catch error, record tripped budget, produce partial result |

### Defensive Measures Required

- **Input Validation:** All tool-call paths validated through jail.Resolve before filesystem I/O; empty/NUL-containing input rejected; absolute paths rejected
- **Error Handling:** Tool execution errors returned to model as tool result, never fatal to agent; transcript I/O errors logged and continued; budget trips produce partial-success results
- **Logging/Audit:** Transcript.jsonl records full event sequence; status.json records tripped budgets and counters; manifest.json records snapshot mode and worktree path
- **Rate Limiting:** max_turns=10 default caps worst-case looping; tool_budget_bytes caps cumulative tool-result bytes; timeout_secs caps wall-clock time
- **Graceful Degradation:** Non-tool-capable models degrade to single-shot; fallback agents inherit tools setting; degrade is per-agent, not per-slot; tools_degraded flag recorded in status.json

---

## Risks

**Technical:**
- Provider function-calling dialect variance → Strict lowest-common-denominator wire format; litellm normalizes most providers; degrade path for the rest
- Token cost explosion → Hard per-agent budgets; counters surfaced in status.json; docs set expectations (3-10× calls per tool agent)
- Small models use tools badly (thrash, loop, ignore results) → Loop hygiene rules, conservative default budgets, per-agent tools opt-in
- Worktree management edge cases (dirty trees, submodules) → Fast path only when clean and head==HEAD; explicit tests; submodules unreadable in v1 (documented)
- Symlink race between EvalSymlinks and Open → O_NOFOLLOW where supported; documented threat model (post-snapshot mutation out of scope)

**TDD-Specific:**
- httptest mock provider divergence from real provider behavior → Test against litellm-normalized responses; add integration test per declared tool-capable model
- Path jail false positives blocking legitimate files (.gitignore, .github/) → Unit test with .gitignore, .github/workflows/ci.yml, foo.git/bar to confirm they pass
- Transcript replay harness diverges from engine Chat call construction → Replay helper lives in same package, imports same request-builder
- Budget counters not propagated on degrade path → Counters written unconditionally in Result builder; test degrade path explicitly
- Manifest review stage lists agents inconsistently across completion paths → Stage entry derived from effective Tools flag at invocation time; test all three completion paths

---

**Next:** `/create-sprint @.planning/plans/active/2.0_tool_using_reviewers/ --gated`
