# Sprint 3.3: Scorecard Pipeline

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 3.3 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Clarifications

### Phase 2 Clarifications (recorded 2026-06-15)

**Key Decisions:**

1. **Per-reviewer usage sourcing — FULL WIRING (option a).** Phase 2 adds `TokensIn`, `TokensOut`, and `Model` to `fanout.Result` (engine.go), accumulates per-turn usage in the tool loop (loop.go), persists model + tokens through `AgentStatus` (status.go + artifacts.go), and the scorecard emitter reads each reviewer's `status.json` to source model/tokens/latency. Rejected "degraded now, defer wiring" — it would ship the exact failure the hard prerequisite was designed to prevent (cost/token columns always empty). TD-002 from the Phase 1 gate explicitly handed this to Phase 2.
2. **Cost computed at emit time, not persisted.** `status.json` persists `model` + `tokens_in` + `tokens_out` only. `cost_usd` is computed in the emitter via `llmclient.ComputeCostUSD(model, tokensIn, tokensOut)` so a future rate-table correction (TD-003) retroactively re-prices historical JSONL records. Token counts are stable raw measurements; cost is derived and follows the current rate table at read time. The JSONL record still carries `cost_usd` as a computed field.

**Scope Boundaries:**

- Approved Phase 2 edits OUTSIDE the originally declared file list: `internal/fanout/engine.go`, `internal/fanout/loop.go`, `internal/fanout/status.go`, `internal/fanout/artifacts.go`. All changes are additive (new fields + accumulation + wiring).
- `internal/fanout/review.go` is explicitly EXCLUDED — no changes needed there; the scorecard hook lives in `cmd/atcr/reconcile.go`, not the review execution path.
- Single-shot path: the engine's `Completer.Complete()` returns content only; the engine must obtain usage on the single-shot path (e.g. via `CompleteWithUsage`) to populate `Result.TokensIn/TokensOut/Model` — handled within the approved engine.go edits.

**Technical Approach:**

- Reviewer→model and reviewer→usage are recovered at reconcile time by reading per-agent `sources/pool/raw/agent/<agent>/status.json` (the only on-disk per-reviewer record). reconcile is a separate process from `atcr review`, so in-memory `fanout.Result` is unavailable — persistence to `status.json` is mandatory.
- `latency_ms` ← `AgentStatus.DurationMS`; finding-derived metrics (raised/corroborated/solo/corroboration_rate) ← `findings.json` `Reviewers`; verification fields ← `reconciled/verification.json`.

### TD-005 Decision (recorded 2026-06-15)

**MCP parity: `atcr_reconcile` MUST emit local scorecards.** The store is not intentionally CLI-only.

**Rationale:**
- `original-requirements.md` says "automatically at the end of `atcr reconcile`" — no CLI-only qualifier
- The codebase has an explicit parity contract: `handleVerify`'s comment in `handlers.go` states "MCP and CLI emit identical artifacts for the same input"
- The shared gate-threshold resolver (`registry.ResolveGateThreshold`) is the existing pattern for cross-entry-point parity
- Monitoring-foundation goal fails if the dominant agentic (MCP) path produces no records

**Fix path (before Phase 5 docs):** Extract `emitScorecard(reviewDir string, res reconcile.Result)` into a shared helper; call it from both `cmd/atcr/reconcile.go` and `internal/mcp/handlers.go:handleReconcile` after `RunReconcile` succeeds — mirrors the gate-threshold resolver pattern so the two entry points cannot diverge again.

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

A per-run scorecard pipeline that emits normalized per-reviewer eval records alongside each `atcr reconcile` run, accumulates those records in a local monthly JSONL store (`~/.config/atcr/scorecard/YYYY-MM.jsonl`), and exposes two CLI commands: `atcr scorecard` (view a single run) and `atcr leaderboard` (aggregated view across runs with filtering and public export). The pipeline first resolves a hard prerequisite: decoding `usage` from provider responses in `internal/llmclient` so that cost and token fields are populated.

### Why This Matters

Every reconcile run currently discards quality signal — corroboration rates, costs, latencies — making it impossible to answer "is our review quality improving?" or "which model finds the most real bugs at what cost?". This feature persists that signal locally and provides the data prerequisite for Epic 10.0's public Model-Eval Leaderboard.

### Key Deliverables

- llmclient `usage` decoding: `tokens_in`, `tokens_out`, `cost_usd` populated from provider responses
- `internal/scorecard/` package: `scorecard.go`, `store.go`, `aggregate.go`, `export.go`
- `atcr scorecard [id-or-path]` CLI command
- `atcr leaderboard [--since, --model, --persona, --export]` CLI command
- `--no-scorecard` flag on `atcr reconcile`
- `docs/scorecard.md` — schema, storage, CLI usage, privacy model

### Success Criteria

- `atcr reconcile` writes per-reviewer JSONL records after each run; verification fields conditionally included when `verification.json` is present
- `atcr scorecard` displays a formatted table for any run by run_id or directory path
- `atcr leaderboard` ranks reviewers by corroboration rate; filters by since/model/persona; exports anonymized public JSON
- `--no-scorecard` suppresses all writes with no side effects on exit code or output
- All 21 acceptance criteria pass; coverage ≥ 80%; lint and vet clean

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

Complexity 9/12 (COMPLEX) → **Moderate 🔄** TDD with **Adversarial 🎯** reviews enabled for all stories.

| Phase | Focus | Stories | TDD Mode |
|-------|-------|---------|----------|
| 1 | Hard Prerequisite (llmclient usage parsing) | — | Moderate + Adversarial |
| 2 | Core Emitter | Story 1 | Moderate + Adversarial |
| 3 | CLI Commands | Stories 2 & 3 | Moderate + Adversarial |
| 4 | Export + Suppression | Stories 4 & 5 | Moderate + Adversarial |
| 5 | Documentation + Integration Tests | Story 6 | Moderate + Adversarial |
| Final | Validation | — | Checklist |

**Gated Mode:** `/execute-sprint` stops at each Phase-Boundary Gate (N.LAST). Review findings, fix any CRITICAL/HIGH issues, then resume.

**Adversarial Reviews:** Fresh subagent spawned per GREEN phase. Subagent has no context of the implementation — intentional bias guard. CRITICAL/HIGH findings fixed inline in REFACTOR; MEDIUM/LOW deferred to `tech-debt-captured.md`.

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
| T1: Focused | After each small change | `go test ./internal/scorecard/... -run TestXxx` |
| T2: Module | After completing story element | `go test ./internal/scorecard/... ./cmd/atcr/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

### DoD Verification Checklist

1. Tests (T3): All passing
2. Coverage: ≥ 80% (`go test -coverprofile=coverage.out ./...`)
3. Lint: No errors (`golangci-lint run`)
4. Vet: Clean (`go vet ./...`)
5. Build: Succeeds (`go build ./...`)

### DoD Report Template

```
Story-{N} DoD Complete
Auto: {X}/5 | Story-Specific: {Y}/{Z} ACs
Manual Review: [ ] Code reviewed
```

### Commit Process

Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).

```
git add [specific files] && git commit -m "<type>(<scope>): <message>"
```

Commit types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`

---

## Development Standards

### Architecture Principles

- **Black Box Interfaces:** `internal/scorecard/` exposes `Emit()`, `ReadRecords()`, `Aggregate()`, `Export()` — callers never know JSONL implementation details.
- **Single Responsibility:** Each file in `internal/scorecard/` has one job: emit, store, aggregate, or export.
- **Replaceable Components:** Storage backend (JSONL) can be replaced without changing callers.
- **Primitive-First Design:** `ScorecardRecord` is the core primitive flowing through the system.

### Coding Standards (Go)

- Packages lowercase, single-word: `scorecard`
- Exported types: `PascalCase` (`ScorecardRecord`, `FilterOpts`, `EmitOpts`)
- Error handling: return `error` as last param; wrap with `fmt.Errorf("context: %w", err)`
- Context: pass `context.Context` as first param in long-running I/O functions
- Imports: stdlib → third-party → internal (`github.com/samestrin/atcr/`)
- Formatting: `goimports` before every commit
- File permissions on scorecard JSONL: `0600` (user read/write only); directory `0700`
- Interface names end in "-er" for single-action behavior (e.g., `Storer`)

### Git Strategy

Branch: `feature/3.3_per_run_scorecard` (create from `main` before first commit)

```bash
git checkout -b feature/3.3_per_run_scorecard
```

Commit format: `type(scope): description`

- `feat(scorecard): implement Emit() with JSONL storage`
- `feat(leaderboard): add --since filter`
- `refactor(scorecard): address adversarial review findings`

---

## External Resources

No external documentation sources identified. Refer to:
- [plan/sprint-design.md](plan/sprint-design.md) — architecture decisions and risk analysis
- [plan/original-requirements.md](plan/original-requirements.md) — full epic definition

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Hard Prerequisite — llmclient Usage Parsing

**Focus:** Resolve the hard prerequisite blocking cost/token fields. Without this, `cost_usd`, `tokens_in`, and `tokens_out` will always be empty — not a degradation case, a blocker for the scorecard emitter.

**Files:** `internal/llmclient/client.go`, `internal/llmclient/chat.go`, `internal/llmclient/client_test.go`

**Note:** This phase resolves a dependency, not a user-facing story. Refer to [sprint-design Phase 1](plan/sprint-design.md) for context.

---

### 1.1 [x] **llmclient Usage Parsing — RED**

1. Read `internal/llmclient/client.go` (`chatResponse`) and `internal/llmclient/chat.go` (`chatToolResponse`) to understand current structure
2. Identify where provider `usage` block is dropped/ignored
3. Write failing tests in `internal/llmclient/client_test.go`:
   - `TestComplete_TokensFromUsage` — assert `Complete()` returns non-zero `tokens_in` and `tokens_out` when provider response includes `usage`
   - `TestChat_TokensFromUsage` — assert `Chat()` returns non-zero `tokens_in` and `tokens_out` when provider response includes `usage`
   - `TestComputeCostUSD_KnownModel` — assert `cost_usd` computed correctly from per-model rate table for a known model
   - `TestComputeCostUSD_UnknownModel` — assert zero (not panic) for unknown model
4. Verify all new tests fail (RED confirmed)

**Files:** `internal/llmclient/client_test.go` | **Duration:** 2-3 hours

---

### 1.2 [x] **llmclient Usage Parsing — GREEN**

1. Add `UsageData` struct with `PromptTokens int`, `CompletionTokens int` fields
2. Decode `usage` in `chatResponse` in `client.go` — unmarshal from provider response JSON
3. Decode `usage` in `chatToolResponse` in `chat.go`
4. Surface `tokens_in`, `tokens_out` via `Complete()` and `Chat()` return values (extend response struct)
5. Create `internal/llmclient/rates.go` — per-model rate table; `ComputeCostUSD(model string, tokensIn, tokensOut int) float64`
6. Thread usage values up: callers of `Complete()`/`Chat()` access usage from response struct
7. Run T1 after each change; confirm all tests pass (T2)
8. `git commit -m "feat(llmclient): decode usage from provider responses (green)"`

**Files:** `internal/llmclient/client.go`, `internal/llmclient/chat.go`, `internal/llmclient/rates.go` | **Duration:** 3-4 hours

---

### 1.2.A [x] **llmclient Usage Parsing — ADVERSARIAL REVIEW (subagent)**

**Changed Files:** `internal/llmclient/client.go`, `internal/llmclient/chat.go`, `internal/llmclient/rates.go`, `internal/llmclient/client_test.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 1.2 llmclient usage parsing`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FILES FROM 1.2]
  - Checklist (pass verbatim):
    - SECURITY: Auth bypass, injection, data exposure?
    - EDGE CASES: Null, empty, boundaries, concurrent access? Provider omits usage entirely?
    - ERROR HANDLING: Missing catches, swallowed errors? What if usage fields are negative?
    - PERFORMANCE: N+1, leaks, blocking ops?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| HIGH (escalated from MEDIUM) | client.go:122-127, chat.go:113 | `UsageData` fields are `int`. A provider emitting `prompt_tokens`/`completion_tokens` as a JSON float (some gateways do) makes `json.Unmarshal` fail the ENTIRE response, turning a successful 200 into a hard parse error and discarding the assistant content. Regression introduced by this change (usage was previously not decoded, so a float block was harmlessly ignored). | Give `UsageData` a custom `UnmarshalJSON` that tolerates float/malformed usage and degrades to zero rather than failing the parent decode. Fix in 1.3. |
| MEDIUM (deferred → TD-001) | rates.go:19 | Rate-table keys are bare model ids; real ids carry suffixes/prefixes (`claude-opus-4-8[1m]`, `anthropic/claude-...`). Variants miss the map and silently yield $0 cost, hiding a misconfigured model. | Normalize model key before lookup (strip `[...]` suffix / provider prefix). Deferred. |
| LOW (addressed in 1.3) | client_test.go | No tests for partial usage block, negative counts into `ComputeCostUSD`, or empty-model lookup. | Add table cases in REFACTOR. |

**Action Required:**
- CRITICAL/HIGH found → List issues for 1.3, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

---

### 1.3 [x] **llmclient Usage Parsing — REFACTOR**

1. Fix CRITICAL/HIGH issues from 1.2.A (if any)
2. Review rate table structure — consider provider-keyed map for extensibility; handle providers that omit usage (zero values, not panics)
3. Ensure `UsageData` zero-value is safe to pass to `ComputeCostUSD`
4. Run T1; validate all tests still pass (T3)
5. `git commit -m "refactor(llmclient): address review + clean up usage parsing"`

**Duration:** 1-2 hours

---

### 1.4 [x] **Phase 1 DoD Verification**

```
Phase-1 Prereq DoD
Auto: 5/5 | Story-Specific: 0/0 ACs (prerequisite phase)
Manual Review: [x] Code reviewed (adversarial 1.2.A + REFACTOR 1.3)
```

- [x] T3: `go test ./internal/llmclient/...` — all passing
- [x] Coverage ≥ 80% for `internal/llmclient/` (92.2%)
- [x] `golangci-lint run ./internal/llmclient/...` — no errors (0 issues)
- [x] `go vet ./internal/llmclient/...` — clean
- [x] Build: `go build ./...` — succeeds
- [x] Manual: `Complete()`/`CompleteWithUsage()` and `Chat()` return correct `tokens_in`/`tokens_out` from fixtures; `ComputeCostUSD` correct for known model, zero for unknown
- [x] Hard prerequisite resolved: cost and token fields will populate in scorecard emitter (Phase 2)

---

### 1.5 [x] **Phase 1 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 1 (integration-level, not TDD cadence)

**Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 1 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 1 (absolute paths): [LIST]
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
    - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
    - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
    - PHASE-EXIT CONTRACT: Phase 2 scorecard emitter can consume `tokens_in`/`tokens_out`/`cost_usd` without rework?
    - REGRESSION: Earlier-phase behavior still intact (Complete()/Chat() return values unchanged for existing callers)?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| MEDIUM (→ TD-002) | chat.go:85-93; fanout/loop.go | `ChatResponse.Usage` is per-turn; tool agents call `Chat()` N times. Phase 2 must accumulate per-agent usage in the fanout loop/`Result` or it undercounts multi-turn token/cost. | Deferred to Phase 2 (fanout integration). |
| MEDIUM (→ TD-003) | rates.go:17-27 | Rate table hardcoded, no override path; stale rate yields confidently-wrong cost; model id not surfaced for audit. | Phase 5 docs note cost approximate; config override follow-up. |
| LOW (fixed inline) | client.go UnmarshalJSON | Negative counts only clamped in `ComputeCostUSD`, not at data boundary. | Added `clampNonNegative` at decode so all consumers see non-negative counts. |
| LOW (fixed inline) | rates.go / UsageData | Three vocabularies (PromptTokens/CompletionTokens vs tokensIn/tokensOut vs tokens_in/tokens_out) invite transposition. | Added `UsageData.CostUSD(model)` so field→arg mapping is written once. |

**Verdict:** No CRITICAL/HIGH. Two LOW items fixed inline; two MEDIUM items captured as TD-002/TD-003. **Phase gate passed.**

**Action Required:**
- CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
- MEDIUM/LOW found → Append to `tech-debt-captured.md`
- None found → Note "Phase gate passed" and proceed to phase stop
**Duration:** 15-30 min

---

## Phase 2: Core Emitter

**Focus:** Story 1 — Auto-emit scorecard records at end of `atcr reconcile`. One JSONL record per reviewer plus one aggregate record per run. Verification fields conditionally included when `verification.json` is present.

**AC Links:** [01-01](plan/acceptance-criteria/01-01-jsonl-file-creation.md), [01-02](plan/acceptance-criteria/01-02-schema-validation.md), [01-03](plan/acceptance-criteria/01-03-verification-conditional-fields.md), [01-04](plan/acceptance-criteria/01-04-no-scorecard-flag.md), [01-05](plan/acceptance-criteria/01-05-aggregate-record.md)

---

### 2.1 [x] **[Auto-emit Scorecard — RED](plan/user-stories/01-auto-emit-scorecard.md)**

**Mode:** Moderate | **AC:** 01-01, 01-02, 01-03, 01-04, 01-05

1. Analyze all 5 ACs; identify testable units in `internal/scorecard/`
2. Write tests in `internal/scorecard/scorecard_test.go`:
   - `TestEmit_CreatesJSONLFile` — assert file created at `~/.config/atcr/scorecard/YYYY-MM.jsonl` after `Emit()`
   - `TestEmit_SchemaValidation` — assert all required fields present: schema_version, run_id, reviewer, model, role, findings_raised, findings_corroborated, findings_solo, corroboration_rate, cost_usd, tokens_in, tokens_out, latency_ms
   - `TestEmit_ConditionalFields_WithVerification` — assert findings_verified, findings_refuted, survived_skeptic_rate present when verification data provided
   - `TestEmit_ConditionalFields_NoVerification` — assert conditional fields absent when no verification data
   - `TestEmit_NoScorecardFlag` — assert zero records written when `EmitOpts.NoScorecard = true`
   - `TestEmit_AggregateRecord` — assert one aggregate record appended per run alongside per-reviewer records
3. Write tests in `internal/scorecard/store_test.go`:
   - `TestStore_AppendAndRead` — assert records written are readable back via `ReadRecords()`
   - `TestStore_FilePermissions` — assert JSONL file created with `0600` permissions
   - `TestStore_ConcurrentAppend_SameMonthFile` — spawn N concurrent goroutines appending records to the same month file; assert every line is intact and JSON-parseable (no interleaved/torn lines) and total line count == total records written (covers sprint-design "Concurrent reconcile runs" atomic-append risk)
4. Verify all tests fail (RED confirmed)

**Files:** `internal/scorecard/scorecard_test.go`, `internal/scorecard/store_test.go` | **Duration:** 2-3 hours

---

### 2.2 [x] **[Auto-emit Scorecard — GREEN](plan/user-stories/01-auto-emit-scorecard.md)**

1. Create `internal/scorecard/` package
2. Implement `internal/scorecard/scorecard.go`:
   - `ScorecardRecord` struct with all schema fields; conditional fields tagged `json:",omitempty"`
   - `EmitOpts` struct: `NoScorecard bool`
   - `Emit(runID string, findings []Finding, summary Summary, verificationPath string, opts EmitOpts) error`:
     - If `opts.NoScorecard`: return nil immediately (no file I/O, no directory creation)
     - Compute per-reviewer metrics from `findings`: raised, corroborated, solo, corroboration_rate
     - Populate cost_usd, tokens_in, tokens_out from llmclient usage (threaded in via Summary or direct)
     - When `verificationPath` non-empty and file exists: fold in findings_verified, findings_refuted, survived_skeptic_rate
     - Build one `ScorecardRecord` per reviewer; build one aggregate record for the run
     - Call `store.Append()` for each record; log errors, never return them (best-effort)
3. Implement `internal/scorecard/store.go`:
   - `Append(record ScorecardRecord) error` — derive month path from `record.RunID` timestamp; open with `O_APPEND|O_CREATE|O_WRONLY`, `0600`; write JSON line + newline via `bufio.Writer`; flush. **Concurrency contract:** marshal each record to a single `[]byte` (line + `\n`) and emit it in one `Write` per record — each record is < `PIPE_BUF`, so `O_APPEND` makes concurrent same-file writes atomic and non-interleaved; do NOT batch multiple records through one shared `bufio.Writer`/flush (that can merge writes past `PIPE_BUF` and tear lines under concurrency)
   - `ReadRecords(path string) ([]ScorecardRecord, error)` — stream-parse JSONL line-by-line; log+skip malformed lines; return valid records
   - Directory auto-created with `0700` on first write
4. Integrate `Emit()` call into `cmd/atcr/reconcile.go` after `RunReconcile()` succeeds
5. Run T1 after each change; run T2 after package complete
6. `git commit -m "feat(scorecard): implement Emit() and store with JSONL storage (green)"`

**Files:** `internal/scorecard/scorecard.go`, `internal/scorecard/store.go`, `cmd/atcr/reconcile.go` | **Duration:** 3-4 hours

---

### 2.2.A [x] **[Auto-emit Scorecard — ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-auto-emit-scorecard.md)**

**Changed Files:** `internal/scorecard/scorecard.go`, `internal/scorecard/store.go`, `internal/scorecard/scorecard_test.go`, `internal/scorecard/store_test.go`, `cmd/atcr/reconcile.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 2.2 scorecard emitter`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 2.2]
  - Checklist (pass verbatim):
    - SECURITY: File permissions on JSONL? PII logged in error messages? Directory permissions?
    - EDGE CASES: Null/empty findings slice? Division by zero in corroboration_rate? Concurrent reconcile runs hitting same file?
    - ERROR HANDLING: Is best-effort logging correct? Does early-return for NoScorecard skip all I/O?
    - PERFORMANCE: bufio flush atomicity? Does Append hold file handle open longer than needed?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| MEDIUM | store.go:17-23 | Atomicity rationale cites PIPE_BUF (governs pipes/FIFOs, not regular files) and claims records are "well under PIPE_BUF" (darwin PIPE_BUF=512B, a full record can approach it). Behavior is correct on Linux/macOS (single-`write()` `O_APPEND` atomic offset update for regular files), but the stated invariant is unsound and the concurrency test only asserts line count, not un-torn lines. | Correct rationale to cite single-`write()` `O_APPEND` regular-file atomicity; strengthen the concurrency test to parse every line + assert a unique sentinel per record. Portability caveat → TD-004. |
| LOW | reconcile.go:124-128 | `emitScorecard` inserts `reviewers[rev]` for every reviewer string incl. a possible empty `""` (blank reviewer column) → junk record with name `""`. | Skip/trim empty reviewer names before inserting and counting. |
| LOW | scorecard.go:116-120 + reconcile.go:139 | Double-logging on `resolveDir` failure: `Emit` logs `scorecard: write failed` and returns the err; caller logs again. Two stderr lines per fault. | Don't return the resolveDir error as aggregate `firstErr` (it is already logged in `Emit`). |
| LOW | store.go:42-43 | `Append` re-opens the month file per record (N open/close cycles for one run's batch). Required for cross-process atomicity; minor in-process cost. | Deliberate trade-off; documented. No change. |

**Action Required:**
- CRITICAL/HIGH found → List issues for 2.3, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

**Verdict:** No CRITICAL/HIGH. The MEDIUM is a doc/test-quality issue (no runtime bug on Linux/macOS today). Cheap correctness items (empty-reviewer skip, double-log, comment correction, concurrency-test strengthening) fixed inline in 2.3; the forward-looking platform-portability caveat captured as TD-004.

---

### 2.3 [x] **[Auto-emit Scorecard — REFACTOR](plan/user-stories/01-auto-emit-scorecard.md)**

1. Fix CRITICAL/HIGH issues from 2.2.A (if any)
2. Ensure `corroboration_rate` handles zero denominator (findings_raised == 0 → rate = 0.0)
3. Add `schema_version: 1` as a package-level constant; all records emit this value
4. Review `ScorecardRecord` JSON field ordering for consistency with spec schema
5. Run T1; validate all tests still pass (T3)
6. `git commit -m "refactor(scorecard): address review + clean up emitter"`

**Duration:** 1-2 hours

---

### 2.4 [x] **Phase 2 DoD Verification**

```
Story-1 DoD Complete
Auto: 5/5 | Story-Specific: 5/5 ACs
Manual Review: [x] Code reviewed (adversarial 2.2.A + REFACTOR 2.3)
```

- [x] T3: `go test ./internal/scorecard/... ./cmd/atcr/...` — all passing
- [x] Coverage ≥ 80% for `internal/scorecard/` (84.7%)
- [x] `golangci-lint run` — no errors (0 issues)
- [x] `go vet ./...` — clean
- [x] Build: `go build ./...` — succeeds
- [x] AC 01-01: JSONL file created at `~/.config/atcr/scorecard/YYYY-MM.jsonl` ✓ (TestEmit_CreatesJSONLFile, TestStore_*)
- [x] AC 01-02: All required schema fields present in emitted records ✓ (TestEmit_SchemaValidation)
- [x] AC 01-03: Verification fields conditional on verification.json presence ✓ (TestEmit_ConditionalFields_*)
- [x] AC 01-04: NoScorecard flag prevents all writes (Emit-level gate tested; full CLI flag in Phase 4) ✓ (TestEmit_NoScorecardFlag)
- [x] AC 01-05: Aggregate record appended per run alongside per-reviewer records ✓ (TestEmit_AggregateRecord)

---

### 2.5 [x] **Phase 2 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 2 (integration-level, not TDD cadence)

**Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 2 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 2 (absolute paths): [LIST]
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: All phase-exit contracts honored (Emit signature, ScorecardRecord struct, store API)?
    - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
    - INTEGRATION: reconcile.go integration correct? scorecard call is post-RunReconcile?
    - PHASE-EXIT CONTRACT: Phase 3 (store.ReadRecords, store.FindByRunID) can be added without reworking Phase 2 code?
    - REGRESSION: `atcr reconcile` still works correctly without scorecard errors surfacing?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| MEDIUM (→ TD-005, RESOLVED 2026-06-15) | internal/mcp/handlers.go (atcr_reconcile) | MCP `atcr_reconcile` ran `RunReconcile` but did not emit a scorecard, while the CLI did → MCP-driven runs silently omitted from the store. | RESOLVED: user decision = MCP must emit (monitoring foundation, not CLI-only). Extracted `scorecard.EmitForReconcile(reviewDir, res)` shared bridge; both CLI and MCP `handleReconcile` call it post-RunReconcile. Bridge unit-tested; MCP test HOME-isolated. |
| LOW (no action) | internal/scorecard/scorecard.go:42-60 | Aggregate record serializes empty `reviewer`/`model`/`role`. Phase 3 readers must key on `record_type` to avoid treating the aggregate as a reviewer row — already the established pattern (`findReviewer` filters by `RecordTypeReviewer`). | No change; pattern established. |
| LOW (→ TD-006) | internal/fanout/loop.go:128,284 | Tool-loop token accumulation (`addUsage`) is unit-tested, and persistence-through-`status.json` is covered for the single-shot path, but no single e2e test asserts a tool-enabled agent's summed usage lands in `status.json`. Currently harmless (statusFor is path-agnostic). | Add a tool-loop e2e usage assertion (extend engine_e2e_test.go). Deferred as test-coverage debt. |

**Verdict:** No CRITICAL/HIGH. Build + full suite + vet green. Contract-exit, config back-compat (status.json byte-identical for zero-usage runs), reconcile integration (strictly post-RunReconcile, best-effort, no exit-code/stdout impact), Phase 3 reusability (monthFromRunID reusable for FindByRunID), and regression (single-shot + tool-loop behavior preserved) all confirmed. **Phase gate passed.** MEDIUM/LOW captured as TD-005/TD-006.

**Action Required:**
- CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
- MEDIUM/LOW found → Append to `tech-debt-captured.md`
- None found → Note "Phase gate passed" and proceed to phase stop
**Duration:** 15-30 min

---

## Phase 3: CLI Commands

**Focus:** Stories 2 & 3 — `atcr scorecard` and `atcr leaderboard` CLI commands with JSONL read, aggregation, ranking, and filtering.

**AC Links (Story 2):** [02-01](plan/acceptance-criteria/02-01-scorecard-command-resolution.md), [02-02](plan/acceptance-criteria/02-02-scorecard-table-rendering.md), [02-03](plan/acceptance-criteria/02-03-scorecard-error-handling.md)
**AC Links (Story 3):** [03-01](plan/acceptance-criteria/03-01-leaderboard-table.md), [03-02](plan/acceptance-criteria/03-02-since-filter.md), [03-03](plan/acceptance-criteria/03-03-model-persona-filters.md), [03-05](plan/acceptance-criteria/03-05-graceful-empty-handling.md)

---

### 3.1 [x] **[View Single-Run Scorecard — RED](plan/user-stories/02-view-single-run-scorecard.md)**

**Mode:** Moderate | **AC:** 02-01, 02-02, 02-03

1. Analyze 3 ACs; identify testable units
2. Write tests in `cmd/atcr/scorecard_test.go`:
   - `TestScorecardCmd_ResolveByRunID` — assert `atcr scorecard <run_id>` resolves and displays correct records
   - `TestScorecardCmd_ResolveByPath` — assert `atcr scorecard <path>` reads records from directory path
   - `TestScorecardCmd_TableRendering` — assert all expected columns present in tabwriter output (reviewer, model, role, raised, corroborated, solo, rate, cost, latency)
   - `TestScorecardCmd_VerificationColumns` — assert conditional verification columns shown only when data present
   - `TestScorecardCmd_NoRecordsFound` — assert "No records found" message, exit code 0
   - `TestScorecardCmd_CorruptedJSONL` — assert malformed lines skipped with warning, command continues
3. Write tests in `internal/scorecard/store_test.go` (extend):
   - `TestStore_FindByRunID` — assert correct records returned for a given run_id
   - `TestStore_FindByRunID_InvalidFormat` — assert clear error for unrecognized run_id format
4. Verify all new tests fail (RED confirmed)

**Files:** `cmd/atcr/scorecard_test.go`, `internal/scorecard/store_test.go` | **Duration:** 2-3 hours

---

### 3.2 [x] **[View Single-Run Scorecard — GREEN](plan/user-stories/02-view-single-run-scorecard.md)**

1. Add `FindByRunID(runID string) ([]ScorecardRecord, error)` to `internal/scorecard/store.go`:
   - Derive month from run_id timestamp prefix (`2026-06-14T10:00:00Z-abc123` → `2026-06.jsonl`)
   - Validate run_id format before parsing; return error for unknown formats
   - Stream-parse only the relevant month file
2. Create `cmd/atcr/scorecard.go` — `atcr scorecard [id-or-path]` command:
   - Dispatch: run_id (starts with timestamp prefix) vs. directory/file path
   - Render via `text/tabwriter`: reviewer | model | role | raised | corroborated | solo | rate | cost_usd | latency_ms
   - Show conditional verification columns (findings_verified, findings_refuted, survived_skeptic_rate) only when any record has them
   - Missing records: "No records found for run <id>" — exit 0
   - Corrupted lines: log warning, continue processing
3. Register `scorecard` command in `cmd/atcr/main.go`
4. Run T1 after each change; confirm all tests pass (T2)
5. `git commit -m "feat(scorecard): implement atcr scorecard command (green)"`

**Files:** `cmd/atcr/scorecard.go`, `internal/scorecard/store.go`, `cmd/atcr/main.go` | **Duration:** 3-4 hours

---

### 3.2.A [x] **[View Single-Run Scorecard — ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-view-single-run-scorecard.md)**

**Changed Files:** `cmd/atcr/scorecard.go`, `internal/scorecard/store.go`, `cmd/atcr/scorecard_test.go`, `internal/scorecard/store_test.go`, `cmd/atcr/main.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 3.2 scorecard command`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 3.2]
  - Checklist (pass verbatim):
    - SECURITY: Path traversal via id-or-path argument? Does path validation prevent escaping scorecard dir?
    - EDGE CASES: run_id with no matching month file? run_id from future date? Empty JSONL file?
    - ERROR HANDLING: What if run_id month derivation returns invalid date? tabwriter flush error?
    - PERFORMANCE: Does stream-parse hold file open after error? Large month file read?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings table:**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| HIGH (fixed in 3.3) | store.go FindByRunID | Returned the first month file with any match and never merged across months; a run split across a month boundary (the exact clock-skew case the fallback exists for) silently dropped the records in the non-winning months. | Rewrote FindByRunID to union matches across boundary-adjacent months; added TestStore_FindByRunID_UnionAcrossMonths. Also resolves the LOW perf concern (boundary-gated, no mid-month rescan). |
| MEDIUM (fixed in 3.3) | paths.go IsRunID | A bare 7-char month prefix ("2026-06") passed IsRunID (only runID[:7] validated) and resolved to exit-1 "no records" instead of exit-2 "bad argument". | Tightened IsRunID to also require the RFC3339 'T' time component; added TestScorecardCmd_BareMonthPrefixIsUsageError. |
| MEDIUM (fixed in 3.3) | paths.go monthRe | `^\d{4}-\d{2}$` accepted impossible months (2026-99/2026-00). | Constrained monthRe to 01-12; added TestStore_FindByRunID_RejectsInvalidMonth. |
| MEDIUM (deferred → TD-007) | store.go / scorecard.go | Diagnostics go to process-global os.Stderr, not cmd.ErrOrStderr(), so a redirected error stream can't capture them. | Package-wide convention (matches Emit/ReadRecords); threading a writer is cross-cutting. Deferred. |
| LOW (fixed in 3.3) | scorecard.go renderScorecard | Final stdout `w.Write` error was discarded (broken pipe → exit 0). | renderScorecard now returns the write error; runScorecard propagates it. |
| LOW (no action) | scorecard.go COST/LATENCY columns | Unbounded numeric fields from JSONL could widen columns. | Reviewer/Model (the injection vector) already sanitized via sanitizeCell; numbers are trusted-local. No change. |

**Verdict:** No CRITICAL. One HIGH (cross-month data loss) fixed in 3.3 with a regression test; two cheap MEDIUM correctness items (loose run_id / invalid-month acceptance) and one LOW (swallowed write error) fixed inline; one MEDIUM (stderr writer) captured as TD-007; one LOW no-action. All tests green, lint 0 issues.

**Action Required:**
- CRITICAL/HIGH found → List issues for 3.3, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

---

### 3.3 [x] **[View Single-Run Scorecard — REFACTOR](plan/user-stories/02-view-single-run-scorecard.md)**

1. Fix CRITICAL/HIGH issues from 3.2.A (if any)
2. Validate path argument at parse time (no traversal; reject anything not under `~/.config/atcr/scorecard/` or an explicit JSONL path)
3. Validate run_id format before month derivation; reject with clear error for unknown formats
4. Improve tabwriter column alignment and header labels for readability
5. Run T1; validate all tests still pass (T3)
6. `git commit -m "refactor(scorecard): address review + clean up command"`

**Duration:** 1-2 hours

---

### 3.4 [x] **[View Aggregated Leaderboard — RED](plan/user-stories/03-view-aggregated-leaderboard.md)**

**Mode:** Moderate | **AC:** 03-01, 03-02, 03-03, 03-05

1. Analyze 4 ACs; identify testable units in `internal/scorecard/aggregate.go`
2. Write tests in `internal/scorecard/aggregate_test.go`:
   - `TestAggregate_RankedTable` — assert output sorted by corroboration_rate descending
   - `TestAggregate_SinceFilter_Days` — assert `--since 7d` excludes records older than 7 days
   - `TestAggregate_SinceFilter_MonthBoundary` — assert `--since 30d` spans month JSONL files correctly
   - `TestAggregate_SinceFilter_Weeks` — assert `--since 2w` (14-day window)
   - `TestAggregate_ModelFilter` — assert `--model X` excludes non-matching records
   - `TestAggregate_PersonaFilter` — assert `--persona X` excludes non-matching records
   - `TestAggregate_ComposedFilters` — assert `--model X --persona Y` composes correctly
   - `TestAggregate_EmptyStore` — assert "No records found" message, exit 0
   - `TestAggregate_NoFilterMatch` — assert "No records match filters" message, exit 1
3. Write tests in `cmd/atcr/leaderboard_test.go`:
   - `TestLeaderboardCmd_TableDisplay` — assert tabwriter output with all expected columns
   - `TestLeaderboardCmd_SinceFlag` — assert `--since` flag wired correctly to filter
   - `TestLeaderboardCmd_ModelFlag` — assert `--model` flag wired correctly
4. Verify all new tests fail (RED confirmed)

**Files:** `internal/scorecard/aggregate_test.go`, `cmd/atcr/leaderboard_test.go` | **Duration:** 2-3 hours

---

### 3.5 [x] **[View Aggregated Leaderboard — GREEN](plan/user-stories/03-view-aggregated-leaderboard.md)**

1. Implement `internal/scorecard/aggregate.go`:
   - `FilterOpts` struct: `Since string`, `Model string`, `Persona string`
   - `ParseSince(s string) (time.Time, error)` — parse `Nd` (days), `Nw` (weeks), `Nm` (months); reject unknown formats
   - `ApplyFilters(records []ScorecardRecord, opts FilterOpts) []ScorecardRecord` — composable time + model + persona filters
   - `AggregatedRow` struct — model, reviewer/persona, role, runs, avg_corroboration_rate, total_cost_usd, avg_latency_ms, cost_per_corroborated
   - `Aggregate(records []ScorecardRecord) []AggregatedRow` — group by (model, reviewer, role); compute averages; sort by corroboration_rate desc
   - Stream relevant month files based on `--since` date range (default last 30 days)
2. Create `cmd/atcr/leaderboard.go` — `atcr leaderboard` command:
   - Flags: `--since` (default "30d"), `--model`, `--persona`, `--export` (placeholder for Phase 4)
   - Load and filter records; aggregate; render ranked table via `text/tabwriter`
   - Empty store: "No records found" (exit 0)
   - No filter match: "No records match filters. Try widening --since or removing filters." (exit 1)
3. Register `leaderboard` command in `cmd/atcr/main.go`
4. Run T1 after each change; confirm all tests pass (T2)
5. `git commit -m "feat(leaderboard): implement atcr leaderboard command (green)"`

**Files:** `internal/scorecard/aggregate.go`, `cmd/atcr/leaderboard.go`, `cmd/atcr/main.go` | **Duration:** 3-4 hours

---

### 3.5.A [x] **[View Aggregated Leaderboard — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-view-aggregated-leaderboard.md)**

**Changed Files:** `internal/scorecard/aggregate.go`, `cmd/atcr/leaderboard.go`, `internal/scorecard/aggregate_test.go`, `cmd/atcr/leaderboard_test.go`, `cmd/atcr/main.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 3.5 leaderboard aggregation`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 3.5]
  - Checklist (pass verbatim):
    - SECURITY: Auth bypass, injection, data exposure?
    - EDGE CASES: Zero corroboration_rate in ranking (division by zero)? --since spanning into months with no files? All records have same rate (stable sort)?
    - ERROR HANDLING: ParseSince rejects unknown suffixes with clear error? What if month file vanishes mid-read?
    - PERFORMANCE: Does aggregation load entire JSONL into memory? Streaming for 10,000 records?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings table:**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| MEDIUM (fixed in 3.6) | aggregate.go runIDTime | Timestamp end was located via the first 'Z', so a run_id with a numeric offset (`+00:00`) returned ok=false and — because the default 30d window is always active — was silently excluded from every view. | Replaced the 'Z'-scan with an RFC3339 prefix regex tolerating both Z and offset forms; added TestApplyFilters_ParsesOffsetTimestamp. |
| MEDIUM (fixed in 3.6) | leaderboard.go no-match path | The always-active default `--since 30d` could hide all historical data behind the generic "no records match filters" message, indistinguishable from a bad --model/--persona. | No-match message now names the active window ("window: last 30d") and suggests a wider --since; added TestLeaderboardCmd_AllRecordsOlderThanDefaultWindow. |
| LOW (fixed in 3.6) | scorecard.go sanitizeCell | Stripped C0+DEL (blocking ESC/ANSI) but not C1 controls (0x80-0x9f) or U+2028/U+2029 line separators that can fracture table rows. | Extended sanitizeCell to also strip C1 and U+2028/U+2029. |
| LOW (fixed in 3.6) | aggregate.go Aggregate sort | Tie-break compared the stored float rate with `!=`; two groups equal-by-value but summed in different orders could differ sub-ULP and bypass the (reviewer, model) tie-break. | Rate comparison now uses exact int64 cross-multiplication; added TestAggregate_RankStableOnEqualRate. |
| LOW (no action) | aggregate.go ParseSince | Accepts `+7d` (harmless) and rejects uppercase units via the default case. | Documented grammar is lowercase Nd/Nw/Nm; behavior left as-is. |

**Verdict:** No CRITICAL/HIGH. Two MEDIUM (offset-timestamp silent drop, ambiguous no-match window) and two LOW (sanitization gap, float tie-break determinism) all fixed inline in 3.6 with regression tests; one LOW no-action. All tests green, lint 0 issues.

**Action Required:**
- CRITICAL/HIGH found → List issues for 3.6, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

---

### 3.6 [x] **[View Aggregated Leaderboard — REFACTOR](plan/user-stories/03-view-aggregated-leaderboard.md)**

1. Fix CRITICAL/HIGH issues from 3.5.A (if any)
2. Validate `--since` format at flag parse time; reject with clear error for unknown suffixes
3. Ensure single-pass JSONL streaming (no load-entire-file-into-memory pattern)
4. Confirm sort is stable (equal corroboration_rate → secondary sort by model name for determinism)
5. Run T1; validate all tests still pass (T3)
6. `git commit -m "refactor(leaderboard): address review + clean up aggregation"`

**Duration:** 1-2 hours

---

### 3.7 [x] **Phase 3 DoD Verification**

```
Stories-2-3 DoD Complete
Auto: 5/5 | Story-Specific: 7/7 ACs
Manual Review: [x] Code reviewed (adversarial 3.2.A + 3.5.A, REFACTOR 3.3 + 3.6)
```

- [x] T3: `go test ./internal/scorecard/... ./cmd/atcr/...` — all passing
- [x] Coverage ≥ 80% (scorecard 89.4%, cmd/atcr 82.9%; aggregate.go 97.3%, ParseSince 100%)
- [x] `golangci-lint run` — no errors (0 issues)
- [x] `go vet ./...` — clean
- [x] Build: `go build ./...` — succeeds
- [x] AC 02-01: `atcr scorecard` resolves by run_id and directory path ✓ (TestScorecardCmd_ResolveByRunID, _ResolveByPath)
- [x] AC 02-02: Table renders all columns; conditional verification columns shown only when present ✓ (TestScorecardCmd_TableRendering, _VerificationColumns, _NoVerificationColumnsWhenAbsent)
- [x] AC 02-03: Error handling for no records and corrupted JSONL lines ✓ (TestScorecardCmd_NoRecordsFound, _CorruptedJSONL, _NoArgs, _InvalidRunID)
- [x] AC 03-01: Leaderboard ranked by corroboration_rate descending ✓ (TestAggregate_RankedTable, _RankStableOnEqualRate)
- [x] AC 03-02: `--since` filter applies time window correctly ✓ (TestParseSince, TestApplyFilters_Since*)
- [x] AC 03-03: `--model` and `--persona` filters composable ✓ (TestApplyFilters_Model, _Persona, _Composed)
- [x] AC 03-05: Graceful handling of empty store (exit 0) and no-match filters (exit 1) ✓ (TestLeaderboardCmd_EmptyStoreExit0, _NoFilterMatchExit1, _AllRecordsOlderThanDefaultWindow)

> Note: sprint-plan distinguishes empty store (exit 0) from filters-match-nothing (exit 1); this intentionally refines the AC 03-02/03-03/03-05 prose (which described exit 0 for no-match) per the executable DoD contract.

---

### 3.8 [x] **Phase 3 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 3 (integration-level, not TDD cadence)

**Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 3 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 3 (absolute paths): [LIST]
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: All phase-exit contracts honored (store, aggregate API signatures)?
    - CONFIG SURFACE: New flags documented, defaulted, back-compat?
    - INTEGRATION: Both commands registered in main.go? Store/aggregate layering clean?
    - PHASE-EXIT CONTRACT: Phase 4 (export) can reuse `aggregate.ApplyFilters()` and `store.ReadRecords()` without rework?
    - REGRESSION: Phase 1-2 behavior intact (scorecard emitter, reconcile integration)?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings table:**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| LOW (fixed inline) | scorecard.go:55-59 | FindByRunID error was wrapped as usageError (exit 2, "invalid run_id"), but the run_id is already validated upstream, so the only error here is a real store read failure (permissions) — mislabeled as usage. | Now returns a plain "failed to read scorecard store" error (exit 1); comment corrected. |
| LOW (no action) | leaderboard.go Long/flags | Phase-3 leaderboard help/flags do not mention `--export`/`--no-scorecard` — correct (those are Phase 4). Noted as a Phase-4 touchpoint. | Phase 4 extends Long + flags here. |
| LOW (no action) | aggregate.go ApplyFilters | ApplyFilters drops aggregate records (per-reviewer leaderboard semantics, intentional). If Phase-4 export ever needs aggregate rows, add a RecordType-preserving variant rather than relaxing ApplyFilters. | None now; Phase-4 note. |

**Verdict:** **PASS** — no CRITICAL/HIGH. Reviewer verified build/vet/lint/full-suite green, both commands registered, monthRe tightening does not regress the emit path (run_id is always UTC `YYYY-MM…Z`), Phase 1-2 emitter/MCP bridge/`--no-scorecard` intact, and Phase 4 can reuse `ReadAll`/`ReadRecords`/`ApplyFilters` without rework. One LOW fixed inline; two LOW are Phase-4 forward notes (no debt). **Phase gate passed.**

**Action Required:**
- CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
- MEDIUM/LOW found → Append to `tech-debt-captured.md`
- None found → Note "Phase gate passed" and proceed to phase stop
**Duration:** 15-30 min

---

## Phase 4: Export + Suppression

**Focus:** Stories 4 & 5 — `atcr leaderboard --export` (versioned public submission JSON with anonymization) and `--no-scorecard` suppression flag on `atcr reconcile`.

**AC Links (Story 4):** [03-04](plan/acceptance-criteria/03-04-export-json.md), [04-01](plan/acceptance-criteria/04-01-export-command-public-schema.md), [04-02](plan/acceptance-criteria/04-02-anonymization-pii-stripping.md), [04-03](plan/acceptance-criteria/04-03-metric-preservation-metadata.md), [04-04](plan/acceptance-criteria/04-04-determinism-filtering-errors.md)
**AC Links (Story 5):** [05-01](plan/acceptance-criteria/05-01-cli-flag-registration.md), [05-02](plan/acceptance-criteria/05-02-suppression-gate.md), [05-03](plan/acceptance-criteria/05-03-no-side-effects.md)

---

### 4.1 [x] **[Export Public Leaderboard — RED](plan/user-stories/04-export-public-leaderboard.md)**

**Mode:** Moderate | **AC:** 03-04, 04-01, 04-02, 04-03, 04-04

1. Analyze 5 ACs; identify testable units in `internal/scorecard/export.go`
2. Write tests in `internal/scorecard/export_test.go`:
   - `TestExport_ValidJSON` — assert `Export()` produces valid JSON conforming to public schema v1
   - `TestExport_AnonymizationStripsRunID` — assert run_id absent from output
   - `TestExport_AnonymizationStripsPathLike` — assert no path-like strings (`/home/`, `/Users/`, `C:\\`)
   - `TestExport_AnonymizationStripsAPIKeys` — assert no API key patterns (`sk-`, `Bearer `)
   - `TestExport_MetricsPreserved` — assert findings_raised, corroboration_rate, cost_usd, tokens_in, tokens_out all present and correct
   - `TestExport_ModelPersonaRolePreserved` — assert model, reviewer/persona, role preserved
   - `TestExport_Determinism` — run `Export()` twice on same input; assert byte-identical output
   - `TestExport_FiltersApplied` — assert `--since`/`--model`/`--persona` applied before anonymization
   - `TestExport_NoMatchError` — assert error with exit 1 when no records match filters
3. Extend `cmd/atcr/leaderboard_test.go`:
   - `TestLeaderboardCmd_ExportFlag` — assert `--export` produces JSON (not table) to stdout
   - `TestLeaderboardCmd_OutputFlag` — assert `--output <path>` writes JSON to specified file
4. Verify all new tests fail (RED confirmed)

**Files:** `internal/scorecard/export_test.go`, `cmd/atcr/leaderboard_test.go` | **Duration:** 2-3 hours

---

### 4.2 [x] **[Export Public Leaderboard — GREEN](plan/user-stories/04-export-public-leaderboard.md)**

1. Implement `internal/scorecard/export.go`:
   - `PublicRecord` struct — allowlist only: schema_version, model, role, runs, findings_raised, findings_corroborated, corroboration_rate, cost_usd, tokens_in, tokens_out, latency_ms (NO run_id, no reviewer name, no path-like fields)
   - `AnonymizeRecord(r ScorecardRecord) PublicRecord` — copy only allowlisted fields; set schema_version = 1
   - `Export(records []ScorecardRecord, opts FilterOpts) ([]byte, error)`:
     1. Apply filters via `aggregate.ApplyFilters()`
     2. Anonymize: build `[]PublicRecord`
     3. Aggregate by (model, role) — sum runs, average metrics
     4. Sort: model asc, role asc (stable, deterministic)
     5. Marshal JSON with explicit struct field order (no random maps)
     6. Return bytes
2. Add `--export` flag to `cmd/atcr/leaderboard.go`: when set, call `export.Export()` and write to stdout or `--output <path>`
3. Create `--output <path>` flag: write JSON to file with `0600` permissions
4. Run T1 after each change; confirm all tests pass (T2)
5. `git commit -m "feat(leaderboard): implement --export with anonymization (green)"`

**Files:** `internal/scorecard/export.go`, `cmd/atcr/leaderboard.go` | **Duration:** 3-4 hours

---

### 4.2.A [x] **[Export Public Leaderboard — ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-export-public-leaderboard.md)**

**Changed Files:** `internal/scorecard/export.go`, `cmd/atcr/leaderboard.go`, `internal/scorecard/export_test.go`, `cmd/atcr/leaderboard_test.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 4.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 4.2 export anonymization`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 4.2]
  - Checklist (pass verbatim):
    - SECURITY: PII leakage in PublicRecord? Is the allowlist truly exhaustive? Could model/role fields contain path-like strings from provider? Could error messages leak paths in JSON output?
    - EDGE CASES: Empty filtered result set? model or role is empty string? Aggregation with single record?
    - ERROR HANDLING: --output file creation fails (permissions, dir missing)? No records after filtering?
    - PERFORMANCE: Large export (10,000 records)? Does sort stability hold for large sets?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings table:**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| HIGH (fixed in 4.3) | export.go scrubPath | Path scrub anchored `(^|\s)` only — a path glued to a non-space byte (`host=/etc/passwd`, `cfg:/Users/sam/.ssh/id_rsa`) leaked into public JSON. | Rewrote scrub to strip any `/`-run NOT preceded by an alphanumeric (so absolute paths and `=/...`/`:/...` forms are stripped while provider-prefixed `anthropic/claude-3` is preserved); `~`-runs stripped anywhere. Added negative fixtures (TestExport_AnonymizationStripsPathLike extended + glued-path case). |
| HIGH (fixed in 4.3) | export.go scrubField | Emails never scrubbed; a reviewer/model carrying `user@host` leaked PII (and would fail export_test.go's own `@` invariant on real data). | Added email pattern to the scrub set. |
| MEDIUM (fixed in 4.3) | export.go scrubKey | Secret-prefix denylist narrow (missed AWS `AKIA`, `glpat-`, `xoxp-`/`xapp-`, `github_pat_`/`gho_`/`ghu_`/`ghs_`/`ghr_`, and `key=`/`token=`/`Authorization:` assignment forms). | Broadened the key denylist and added credential-assignment forms; documented that the allowlist (not the denylist) is the primary guarantee. |
| MEDIUM (fixed in 4.3) | export.go AnonymizeRecord | Negative metric in a corrupt-but-parseable source record flowed into sums and `ratio()`, yielding negative/>1 public rates. | Clamp ingested counts to ≥0 in AnonymizeRecord (defense-in-depth for the public format). |
| LOW (fixed in 4.3) | export_test.go | PII tests only fed space-separated happy-path fixtures — the scrub gaps were untested. | Added negative fixtures (glued path, email, AWS/GitLab key prefixes) asserting absence in output. |
| LOW (no action) | export.go SurvivedSkepticRate | A no-verification group emits 0.0, indistinguishable from a verified-0% group. | By design per AC 04-03 Scenario 8 (zero, not omitted/null, when absent). Documented. |
| LOW (no action) | leaderboard.go writeExportFile | `os.Stat` directory pre-check is TOCTOU. | Pre-stat is only for a friendlier message; the rename/create fails safely regardless. No correctness impact. |

**Verdict:** No CRITICAL. Two HIGH (mid-token path leak, unscrubbed emails) and three MEDIUM/LOW hardening items fixed inline in 4.3 with negative-fixture regression tests; two LOW are by-design/no-impact. All cheap — nothing deferred to TD.

**Action Required:**
- CRITICAL/HIGH found → List issues for 4.3, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

---

### 4.3 [x] **[Export Public Leaderboard — REFACTOR](plan/user-stories/04-export-public-leaderboard.md)**

1. Fix CRITICAL/HIGH issues from 4.2.A (if any)
2. Review `PublicRecord` allowlist — add sanitization for model/role fields (strip any path-like substring)
3. Ensure `--output` file created with `0600` permissions; parent directory must exist
4. Verify tests assert no paths/hostnames/API key patterns — add assertions if missing
5. Run T1; validate all tests still pass (T3)
6. `git commit -m "refactor(export): address review + tighten anonymization allowlist"`

**Duration:** 1-2 hours

---

### 4.4 [x] **[Suppress Emission — RED](plan/user-stories/05-suppress-emission.md)**

**Mode:** Moderate | **AC:** 05-01, 05-02, 05-03

1. Analyze 3 ACs; identify testable units
2. Write tests in `cmd/atcr/reconcile_test.go`:
   - `TestReconcileCmd_NoScorecardFlagInHelp` — assert `--no-scorecard` appears in `atcr reconcile --help` output
   - `TestReconcileCmd_NoScorecardSuppressesWrite` — assert zero JSONL records written when `--no-scorecard` passed
   - `TestReconcileCmd_NoScorecardExitCode` — assert reconcile exit code unchanged with `--no-scorecard`
   - `TestReconcileCmd_NoScorecardNoSideEffects` — assert reconcile stdout/stderr unaffected by `--no-scorecard`
   - `TestReconcileCmd_DefaultWritesScorecard` — regression: assert reconcile WITHOUT `--no-scorecard` still writes records
3. Verify all tests fail (RED confirmed)

**Files:** `cmd/atcr/reconcile_test.go` | **Duration:** 1-2 hours

---

### 4.5 [x] **[Suppress Emission — GREEN](plan/user-stories/05-suppress-emission.md)**

1. Add `--no-scorecard` bool flag to `cmd/atcr/reconcile.go` via cobra: `cmd.Flags().Bool("no-scorecard", false, "suppress scorecard emission for this run")`
2. Pass flag value to `scorecard.Emit()` via `EmitOpts{NoScorecard: noScorecard}`
3. Verify `internal/scorecard/scorecard.go` `Emit()` already checks `opts.NoScorecard` as FIRST condition (from Phase 2); confirm no directory creation or file I/O occurs when true
4. Run T1 after each change; confirm all tests pass (T2)
5. `git commit -m "feat(reconcile): add --no-scorecard suppression flag (green)"`

**Files:** `cmd/atcr/reconcile.go`, `internal/scorecard/scorecard.go` | **Duration:** 1-2 hours

---

### 4.5.A [x] **[Suppress Emission — ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-suppress-emission.md)**

**Changed Files:** `cmd/atcr/reconcile.go`, `internal/scorecard/scorecard.go`, `cmd/atcr/reconcile_test.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 4.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 4.5 no-scorecard suppression`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 4.5]
  - Checklist (pass verbatim):
    - SECURITY: Does early return leave any partial state (open file handle, half-written record)?
    - EDGE CASES: Is suppression gate truly the FIRST check in Emit()? Any code path that creates the directory before the gate check?
    - ERROR HANDLING: Does the flag default to false (emit by default)? Correct cobra flag registration?
    - PERFORMANCE: N+1, leaks, blocking ops?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings table:**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| MEDIUM (fixed in 4.6) | internal/scorecard/reconcile.go:23 | The "zero I/O when suppressed" intent was defeated at the bridge: `EmitForReconcile` read `fanout.ReadPoolSummary(reviewDir)` and assembled findings BEFORE `Emit`'s `NoScorecard` gate ran (`Emit` is the bridge's last call). The store gate still prevented all scorecard-store writes, but the suppressed path still did the upstream pool-summary read. | Added an `opts.NoScorecard` early-return at the TOP of `EmitForReconcile`, before any read, so a suppressed run does truly zero I/O. `Emit`'s own first-line gate is retained as defense-in-depth for direct callers. |
| LOW (no action) | internal/scorecard/scorecard.go:111 | `Emit` returns `error` but the bridge discards it (best-effort, logs internally). | Not dead — `scorecard_test.go` exercises the return (nil on suppression, error on write failure). Deliberate best-effort contract; left as-is. |

**Verdict:** No CRITICAL/HIGH. One MEDIUM (bridge-level pool-summary read on the suppressed path) fixed inline in 4.6; one LOW is a deliberate, test-exercised contract (no action). Emit's gate confirmed first-statement; flag defaults to false; MCP path still emits; exit-code semantics unchanged. Nothing deferred to TD.

**Action Required:**
- CRITICAL/HIGH found → List issues for 4.6, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

---

### 4.6 [x] **[Suppress Emission — REFACTOR](plan/user-stories/05-suppress-emission.md)**

1. Fix CRITICAL/HIGH issues from 4.5.A (if any)
2. Confirm suppression gate is the absolute first check in `Emit()` — add comment noting this is intentional
3. Verify `EmitOpts` struct is documented and minimal
4. Run T1; validate all tests still pass (T3)
5. `git commit -m "refactor(reconcile): address review + clean up suppression gate"`

**Duration:** 30-60 min

---

### 4.7 [x] **Phase 4 DoD Verification**

```
Stories-4-5 DoD Complete
Auto: 5/5 | Story-Specific: 8/8 ACs
Manual Review: [x] Code reviewed (adversarial 4.2.A + 4.5.A, REFACTOR 4.3 + 4.6)
```

- [x] T3: `go test ./...` — all passing
- [x] Coverage ≥ 80% (module total 88.3%; scorecard+cmd/atcr 85.2%; export.go 95.7%, AnonymizeRecord/scrubField 100%)
- [x] `golangci-lint run` — no errors (0 issues)
- [x] `go vet ./...` — clean
- [x] Build: `go build ./...` — succeeds
- [x] AC 03-04: `atcr leaderboard --export` produces JSON output ✓ (TestLeaderboardCmd_ExportFlag, TestExport_ValidJSON)
- [x] AC 04-01: Public schema v1 conformance (schema_version, exported_at, filters, records); --output to file/stdout; dir-target error ✓ (TestExport_ValidJSON, TestLeaderboardCmd_OutputFlag, _OutputToDirectoryExit1)
- [x] AC 04-02: Anonymization strips run_id, path-like (incl. glued/Windows), email, and API-key patterns ✓ (TestExport_Anonymization*, TestExport_PreservesProviderPrefixedModel)
- [x] AC 04-03: All 11 metrics + model/persona/role preserved; verification fields zero-not-null; runs included ✓ (TestExport_MetricsPreserved, _VerificationZeroWhenAbsent, TestAnonymizeRecord_StripsRunIDPreservesMetrics)
- [x] AC 04-04: Deterministic (byte-identical); sorted (model, reviewer, role); filters before anonymization; exit 1 + exact message on no match ✓ (TestExport_Determinism, _SortedByModelReviewerRole, _FiltersApplied, _NoMatchError, TestLeaderboardCmd_Export{Empty,NoFilterMatch}Exit1)
- [x] AC 05-01: `--no-scorecard` appears in `atcr reconcile --help` ✓ (TestReconcileCmd_NoScorecardFlagInHelp)
- [x] AC 05-02: Zero records written when `--no-scorecard` passed; store dir not created ✓ (TestReconcileCmd_NoScorecardSuppressesWrite, TestEmitForReconcile_NoScorecardSuppresses)
- [x] AC 05-03: No side effects on exit code or stdout/stderr with `--no-scorecard` ✓ (TestReconcileCmd_NoScorecardExitCodeUnchanged, _NoScorecardNoSideEffects, _DefaultWritesScorecard regression)

---

### 4.8 [x] **Phase 4 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 4 (integration-level, not TDD cadence)

**Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 4 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 4 (absolute paths): [LIST]
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: Export API contract clean (inputs, outputs, error types)?
    - CONFIG SURFACE: `--export`, `--output`, `--no-scorecard` flags documented, defaulted, back-compat?
    - INTEGRATION: Export correctly reuses aggregate.ApplyFilters()? No scorecard data leaked through export?
    - PHASE-EXIT CONTRACT: Phase 5 integration tests can exercise the full pipeline end-to-end?
    - REGRESSION: Phases 1-3 unaffected (emit still fires by default; leaderboard table still works without --export)?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings table:**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| LOW (fixed inline) | leaderboard.go runLeaderboard | `--output` without `--export` silently rendered the table and ignored `--output`, leaving the user's expected file unwritten with no signal. | `--output` without `--export` is now a usage error (exit 2, "--output requires --export"); added TestLeaderboardCmd_OutputWithoutExportIsUsageError. |
| LOW (fixed inline) | export.go:203 | `ExportFilters(opts)` struct conversion (required by staticcheck S1016) invisibly couples the two structs' field layout. | Added a comment documenting that the two structs mirror field-for-field and a divergence breaks compilation by design (the forcing function). |
| LOW (no action) | export.go survived_skeptic_rate | A no-verification group emits 0 for verification metrics, indistinguishable from a verified-0% group. | By design per AC 04-03 Scenario 8 (always-present, zero-included). Reviewer concurred no code change required for Phase 4 correctness. |

**Verdict:** **PASS** — no CRITICAL/HIGH. Reviewer confirmed: Export reuses ApplyFilters with filters-before-anonymization; ErrNoExportRecords handled consistently (stderr + exit 1, never non-JSON on stdout); PublicRecord allowlist + scrubField backstop prevent internal-field leak; (model, reviewer, role) sort deterministic; EmitForReconcile signature change preserves CLI↔MCP parity (both emit; only CLI suppresses, double-gated); Phases 1-3 regression-free (default emit fires, table path intact, scorecard cmd untouched, exit codes unchanged); Phase 5 can exercise emit→store→read→aggregate→export end-to-end without rework. Two cheap LOW items fixed inline; one LOW is by-design. Nothing deferred to TD. **Phase gate passed.**

**Action Required:**
- CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
- MEDIUM/LOW found → Append to `tech-debt-captured.md`
- None found → Note "Phase gate passed" and proceed to phase stop
**Duration:** 15-30 min

---

## Phase 5: Documentation + Integration Testing

**Focus:** Story 6 — Create `docs/scorecard.md` and run end-to-end integration tests for the full reconcile → emit → query pipeline.

**AC Links:** [06-01](plan/acceptance-criteria/06-01-scorecard-documentation.md)

---

### 5.1 [ ] **[Document Scorecard — RED](plan/user-stories/06-document-scorecard.md)**

**Mode:** Moderate | **AC:** 06-01

1. Write integration test stubs in `internal/scorecard/integration_test.go` (build tag: `//go:build integration`):
   - `TestIntegration_ReconcileEmitRead` — stub: emit test records → `FindByRunID()` → assert correct reviewer/model/fields
   - `TestIntegration_ReconcileEmitAggregate` — stub: emit multiple records → `Aggregate()` → assert ranked correctly
   - `TestIntegration_NoScorecardSuppresses` — stub: emit with NoScorecard=true → assert zero records in store
2. Write docs existence test in `internal/scorecard/docs_test.go`:
   - `TestDocs_ScorecardMdExists` — assert `docs/scorecard.md` exists at repo root
3. Verify all new tests fail (RED confirmed: file does not exist yet, stubs not implemented)

**Files:** `internal/scorecard/integration_test.go`, `internal/scorecard/docs_test.go` | **Duration:** 1-2 hours

---

### 5.2 [ ] **[Document Scorecard — GREEN](plan/user-stories/06-document-scorecard.md)**

1. Create `docs/scorecard.md` covering:
   - **Schema (v1):** Full field table — name, type, required/conditional, description; `schema_version` purpose and value
   - **Storage:** `~/.config/atcr/scorecard/YYYY-MM.jsonl`; monthly rotation; append-only; file permissions (`0600`); warning: do not commit this directory to git
   - **CLI Usage:**
     - `atcr scorecard [id-or-path]` — arguments, output format, error cases
     - `atcr leaderboard [--since N(d|w|m)] [--model X] [--persona X]` — ranking logic, output format, empty/no-match behavior
     - `atcr leaderboard --export [--output path]` — public submission format description, anonymization summary
     - `atcr reconcile --no-scorecard` — suppression behavior and when to use it
   - **Privacy Model:** Fields stripped by `--export` (run_id, paths, API keys, hostnames); fields preserved (metrics, model, persona, role); note that v1 is experimental until Epic 10.0 stabilizes
2. Implement integration test bodies in `internal/scorecard/integration_test.go`:
   - Build minimal test fixtures: mock reconcile output structures in-memory
   - `TestIntegration_ReconcileEmitRead`: call `Emit()` → `FindByRunID()` → assert all schema fields correct
   - `TestIntegration_ReconcileEmitAggregate`: call `Emit()` twice with different reviewers → `Aggregate()` → assert ranking
   - `TestIntegration_NoScorecardSuppresses`: call `Emit()` with `NoScorecard=true` → assert store empty
   - Use `t.TempDir()` for all JSONL file I/O; clean up automatically
3. Make `TestDocs_ScorecardMdExists` pass
4. Run T2; confirm all tests pass
5. `git commit -m "docs(scorecard): add scorecard.md (schema, storage, CLI, privacy)"`
6. `git commit -m "test(scorecard): add integration tests for full pipeline"`

**Files:** `docs/scorecard.md`, `internal/scorecard/integration_test.go`, `internal/scorecard/docs_test.go` | **Duration:** 3-4 hours

---

### 5.2.A [ ] **[Documentation + Integration Tests — ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-document-scorecard.md)**

**Changed Files:** `docs/scorecard.md`, `internal/scorecard/integration_test.go`, `internal/scorecard/docs_test.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 5.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 5.2 documentation and integration tests`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 5.2]
  - Checklist (pass verbatim):
    - SECURITY: Does docs/scorecard.md warn about not committing `~/.config/atcr/scorecard/`? Does it accurately describe what --export strips?
    - EDGE CASES: Do integration tests cover suppression AND default-emit paths? Do tests use t.TempDir() (no test artifact leakage)?
    - ERROR HANDLING: Do integration tests assert error messages and exit codes, not just success paths?
    - COMPLETENESS: Does docs/scorecard.md cover all 4 CLI commands/flags? All schema fields? The privacy model? Any ACs from 06-01 uncovered?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| CRITICAL | | | |
| HIGH | | | |

**Action Required:**
- CRITICAL/HIGH found → List issues for 5.3, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

---

### 5.3 [ ] **[Documentation + Integration Tests — REFACTOR](plan/user-stories/06-document-scorecard.md)**

1. Fix CRITICAL/HIGH issues from 5.2.A (if any)
2. Verify docs/scorecard.md section headers are clear and consistent (Schema, Storage, CLI Usage, Privacy Model)
3. Check all integration tests use `t.TempDir()` for JSONL I/O (no manual cleanup needed)
4. Run T1; validate all tests still pass (T3 + integration: `go test -tags=integration ./...`)
5. `git commit -m "refactor(scorecard): address review + finalize docs and integration tests"`

**Duration:** 1-2 hours

---

### 5.4 [ ] **Phase 5 DoD Verification**

```
Story-6 DoD Complete
Auto: {X}/5 | Story-Specific: 1/1 ACs
Manual Review: [ ] Code reviewed [ ] Docs reviewed
```

- [ ] T3: `go test ./...` — all passing
- [ ] Integration: `go test -tags=integration ./...` — all passing
- [ ] Coverage ≥ 80%
- [ ] `golangci-lint run` — no errors
- [ ] `go vet ./...` — clean
- [ ] Build: `go build ./...` — succeeds
- [ ] AC 06-01: `docs/scorecard.md` exists; schema, storage, CLI usage, and privacy model documented ✓
- [ ] Manual: docs/scorecard.md reviewed for accuracy against current implementation

---

### 5.5 [ ] **Phase 5 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 5 (integration-level, not TDD cadence)

**Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 5 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 5 (absolute paths): [LIST]
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: Integration tests use correct package APIs? t.TempDir() isolation correct?
    - CONFIG SURFACE: docs/scorecard.md accurately reflects all flags and config behavior from all 5 phases?
    - INTEGRATION: End-to-end pipeline (emit → store → read → aggregate → export) tested?
    - PHASE-EXIT CONTRACT: Final validation phase can run all tests and verify all 21 ACs without setup?
    - REGRESSION: All earlier-phase tests still pass?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| CRITICAL | | | |
| HIGH | | | |

**Action Required:**
- CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
- MEDIUM/LOW found → Append to `tech-debt-captured.md`
- None found → Note "Phase gate passed" and proceed to phase stop
**Duration:** 15-30 min

---

## Final Phase: Validation

### Validation Checklist

- [ ] All unit tests passing: `go test ./...`
- [ ] Integration tests passing: `go test -tags=integration ./...`
- [ ] Coverage ≥ 80%: `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out`
- [ ] Lint clean: `golangci-lint run`
- [ ] Vet clean: `go vet ./...`
- [ ] Build succeeds: `go build ./...`

### AC Verification (all 21)

| AC | Description | Status |
|----|-------------|--------|
| 01-01 | JSONL file created at `~/.config/atcr/scorecard/YYYY-MM.jsonl` | [ ] |
| 01-02 | All required schema fields present in emitted records | [ ] |
| 01-03 | Verification fields conditional on verification.json presence | [ ] |
| 01-04 | `--no-scorecard` suppresses all writes | [ ] |
| 01-05 | Aggregate record appended per run alongside per-reviewer records | [ ] |
| 02-01 | `atcr scorecard` resolves by run_id and directory path | [ ] |
| 02-02 | Table renders all columns; conditional verification columns | [ ] |
| 02-03 | Error handling: no records (exit 0), corrupted JSONL (skip+warn) | [ ] |
| 03-01 | Leaderboard ranked by corroboration_rate descending | [ ] |
| 03-02 | `--since` filter applies time window correctly | [ ] |
| 03-03 | `--model` and `--persona` filters composable | [ ] |
| 03-04 | `--export` flag produces JSON output | [ ] |
| 03-05 | Graceful handling of empty store (exit 0) and no-match (exit 1) | [ ] |
| 04-01 | Public schema v1 conformance (schema_version field) | [ ] |
| 04-02 | Anonymization strips run_id, paths, API keys | [ ] |
| 04-03 | All metrics and model/persona/role preserved in export | [ ] |
| 04-04 | Deterministic output; filters before anonymization; exit 1 on no match | [ ] |
| 05-01 | `--no-scorecard` appears in `atcr reconcile --help` | [ ] |
| 05-02 | Zero records written with `--no-scorecard` | [ ] |
| 05-03 | No side effects on exit code or stdout/stderr with `--no-scorecard` | [ ] |
| 06-01 | `docs/scorecard.md` with schema, storage, CLI usage, privacy model | [ ] |

### Optional: Targeted Mutation Testing

Mutation testing is UNAVAILABLE in this environment. Skip this step.

### Drift Analysis

Compare final implementation against [original-requirements.md](plan/original-requirements.md):

- [ ] All 8 original ACs from epic addressed
- [ ] No scope added beyond the original request
- [ ] Hard prerequisite (llmclient usage parsing) resolved in Phase 1
- [ ] Schema versioning (`schema_version: 1`) implemented and documented
- [ ] Storage at `~/.config/atcr/scorecard/` (never committed to git) — documented
- [ ] Public export format documented as experimental until Epic 10.0 stabilizes
- [ ] Out-of-scope items NOT implemented: public leaderboard site, team-shared storage, real-time dashboard, persona quality scoring beyond standard metrics

---

`git add [all sprint-modified files] && git commit -m "chore(sprint): sprint 3.3 complete — scorecard pipeline"`
