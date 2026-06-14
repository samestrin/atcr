# Sprint 3.0: Adversarial Verification Sprint

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 3.0 step-by-step. Complete each step, check off work immediately. After completing a phase, stop at the gate and wait for approval before proceeding.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

The adversarial verification pipeline — a skeptic agent stage that runs after `atcr reconcile`, takes each unique deduped finding, and attempts to disprove it using the Epic 2.0 tool loop (real code access). Skeptics are a different model than any reviewer credited on the finding. Verdicts (`confirmed` | `refuted` | `unverifiable`) feed a second confidence axis so the CI gate can count only non-refuted findings.

### Why This Matters

False positives kill LLM code review adoption: a panel that is 60% noise gets ignored within a week. An adversarial pass by a model explicitly prompted to disprove the finding — with code access to check the actual evidence — attacks the shared-blind-spot problem that agreement-based confidence cannot solve. This is the stage that makes the gate trustworthy enough to block merges.

### Key Deliverables

- `internal/verify/` package: selection, prompt construction, verdict parsing, invocation, vote aggregation, confidence v2, artifact emission
- `atcr verify` CLI subcommand with `--fresh`, `--thorough`, `--min-severity` flags
- `atcr_verify` MCP tool registered in the server
- `atcr review --verify` chaining (review → reconcile → verify in one command)
- `--fail-on <severity> --require-verified` gate semantics
- Report rendering: VERIFIED tier, skeptic panel, collapsed Refuted section
- `docs/verification.md` and fixture corpus (`true-finding.json`, `false-finding.json`, `malformed-response.txt`)

### Success Criteria

- `atcr verify` runs skeptics over deduped findings and produces `verification.json` plus re-emitted artifacts with v2 confidence
- Different-model rule enforced: no skeptic shares a model with a credited reviewer; no-eligible-skeptic → `unverifiable` with reason `no_eligible_skeptic`
- Refuted findings demoted to LOW, retained with skeptic reasoning, excluded from `--fail-on` counts
- `--fail-on high --require-verified` passes/fails correctly across fixture matrix (12+ scenarios)
- `verify.min_severity` floor and vote majority both honored
- `go test ./...` passes; ≥ 95% coverage on new code; `go vet ./...` clean; `go build ./...` succeeds

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** MODERATE 🔄 with Adversarial 🎯 for all 6 stories (complexity 8/12 — COMPLEX)
**Pattern per story:** RED (write failing tests) → GREEN (minimal implementation) → ADVERSARIAL REVIEW (fresh subagent) → REFACTOR (fix critical/high issues + quality)
**Inline-Fix Severities:** CRITICAL/HIGH (fix before REFACTOR commit)
**Defer Severities:** MEDIUM/LOW (append to `clarifications/tech-debt-captured.md`)

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, phase structure |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [user-stories/](plan/user-stories/) | Feature requirements (6 stories) |
| [acceptance-criteria/](plan/acceptance-criteria/) | Validation requirements with DoD (28 ACs) |
| [documentation/](plan/documentation/) | Verification pipeline, CLI/MCP, tool loop, testing fixtures |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command |
|------|------|---------|
| T1: Focused | After each small change | `go test ./internal/verify/... -run TestFunctionName` |
| T2: Module | After completing a story element | `go test ./internal/verify/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

### DoD Verification Checklist

1. **Tests (T3):** `go test ./...` — all passing
2. **Coverage:** `go test -coverprofile=coverage.out ./...` — ≥ 80% overall, ≥ 95% on new `internal/verify/` code
3. **Lint:** `golangci-lint run` — no errors; `go vet ./...` — clean
4. **Build:** `go build ./...` — succeeds (verify no import cycles: `verify` imports `reconcile`, NOT vice-versa)
5. **Docs:** Updated where scope requires

### DoD Report Template

```
Phase-{N} DoD Complete
Auto: {X}/5 | Story-Specific: {Y}/{Z}
Manual Review: [ ] Code reviewed
```

### Commit Process

Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).

```
git add [specific files] && git commit -m "type(verify): description"
```

Commit types: `feat`, `fix`, `test`, `refactor`, `docs`, `chore`

---

## Development Standards

### Architecture Principles (from implementation-standards.md)

- **Black Box Interfaces:** Every `internal/verify/*.go` file exposes a clean public API. Implementation details are hidden. Each file is independently replaceable via its interface.
- **Replaceable Components:** `invokeSkeptic` depends on `fanout.Engine` via `ChatCompleter` interface — swap for testing. Verdict parsing is a pure function — replaceable without affecting invocation. Emission functions are isolated.
- **Single Responsibility:** One concern per file: `select.go` (eligibility), `skeptic.go` (prompt), `verdict.go` (parsing), `invoke.go` (invocation), `votes.go` (aggregation), `confidence_v2.go` (recomputation), `emit_*.go` (artifacts), `pipeline.go` (orchestration).
- **Primitive-First:** Core primitives — `Verdict` enum (`confirmed|refuted|unverifiable`), `Confidence` tiers v2 (`VERIFIED > HIGH > MEDIUM > LOW`), `Verification` struct (`{Verdict, Skeptic, Notes}`), `FindingKey` (`{File, Line, Problem}`).
- **Import Cycle Guard:** `verify` imports `reconcile`; `reconcile` must NOT import `verify`. Verify with `go build ./...` after initial scaffolding.
- **Panic Safety:** Goroutines in skeptic invocation must recover panics. `invokeSkeptic` never propagates runtime errors to callers — all failures captured in `Verification.Notes`.

### Coding Standards (from coding-standards.md)

- **Packages:** Lowercase single-word: `verify`, `registry`, `reconcile`
- **Exported identifiers:** `PascalCase` — `AgentsByRole`, `SelectEligibleSkeptics`, `BuildSkepticPrompt`, `ParseVerdict`, `AggregateVerdicts`, `ConfidenceV2`
- **Unexported identifiers:** `camelCase`
- **Interfaces:** `-er` suffix for single-action — `ChatCompleter`
- **Error handling:** Return `error` last; wrap with `fmt.Errorf("invokeSkeptic: %w", err)`; no panics in normal flow
- **Context propagation:** Accept `context.Context` as first param in I/O functions; respect cancellation in skeptic invocation
- **Tests:** Table-driven; `testify/assert` and `testify/require`; `*_test.go` co-located; integration tests use `//go:build integration` build tag
- **Formatting:** `go fmt` / `goimports` before every commit; `golangci-lint run` must pass

### Git Strategy (from git-strategy.md)

- Branch: `feature/3.0_adversarial_verification` (already scoped by sprint)
- Commits: `type(verify): description` — atomic, small; one logical change per commit
- Do NOT squash during sprint — keep granular history; squash happens on PR merge to `main`
- Commit examples:
  - `test(verify): add failing tests for AgentsByRole filtering` (RED)
  - `feat(verify): implement SelectEligibleSkeptics with different-model rule` (GREEN)
  - `refactor(verify): clean up select.go after adversarial review` (REFACTOR)

---

## External Resources

### Critical — Read Before Implementation

- **[verification-pipeline.md](plan/documentation/verification-pipeline.md)** — Core architecture: `Verification` struct at `emit.go:36`, verdict enum, confidence v2 tiers, artifact schemas
- **[cli-mcp-integration.md](plan/documentation/cli-mcp-integration.md)** — `cmd/atcr/verify.go` Cobra pattern, MCP handler at `handlers.go`, gate counter at `gate.go:57` and `handlers.go:339`

### Important — Review During Development

- **[llm-tool-loop.md](plan/documentation/llm-tool-loop.md)** — `invokeToolLoop` at `loop.go:81`, per-finding budgets (MaxTurns=10, ToolBudgetBytes=1MB, Timeout=60s)

### Reference — Consult As Needed

- **[testing-fixtures.md](plan/documentation/testing-fixtures.md)** — Verdict parsing test cases (7), golden file testing, fixture corpus patterns

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation (2 days)

**Focus:** Role plumbing, `internal/verify/` package scaffolding, skeptic selection primitives

**Files:**
- CREATE: `internal/verify/select.go`
- CREATE: `internal/verify/select_test.go`
- MODIFY: `internal/registry/config.go` (add `AgentsByRole` method)

**Stories:** [Story 1: Skeptic Selection & Role Plumbing](plan/user-stories/01-skeptic-selection-role-plumbing.md)

---

### 1.1 [x] **[Skeptic Selection — RED](plan/user-stories/01-skeptic-selection-role-plumbing.md)**

**Mode:** Moderate | **ACs:** [01-01](plan/acceptance-criteria/01-01-agentsbyrole-filtering.md) [01-02](plan/acceptance-criteria/01-02-different-model-exclusion.md) [01-03](plan/acceptance-criteria/01-03-empty-selection-unverifiable.md) [01-04](plan/acceptance-criteria/01-04-empty-role-backward-compat.md) [01-05](plan/acceptance-criteria/01-05-test-coverage-requirements.md)

1. Scaffold `internal/verify/` package: create `internal/verify/select.go` with package declaration and stub signatures only
2. Write table-driven tests in `internal/verify/select_test.go`:
   - `TestAgentsByRole_FiltersByRole` — mixed registry, only agents with `role: skeptic` returned
   - `TestAgentsByRole_EmptyRoleDefaultsToReviewer` — empty Role treated as `RoleReviewer`
   - `TestAgentsByRole_UnknownRole` — unknown role → empty map
   - `TestSelectEligibleSkeptics_DifferentModelRule` — excludes skeptics sharing model with any reviewer
   - `TestSelectEligibleSkeptics_NoEligible` — all skeptics share models → empty slice
   - `TestSelectEligibleSkeptics_NSelection` — fewer candidates than n → returns all available
   - `TestSelectEligibleSkeptics_UnresolvableReviewer` — reviewer not in registry → silently skipped
3. Add test for `AgentsByRole` to `internal/registry/config_test.go` (or existing test file)
4. Verify tests fail correctly: `go test ./internal/verify/... ./internal/registry/...`

**Files:** `internal/verify/select_test.go`, `internal/registry/config_test.go` | **Duration:** 3h

---

### 1.2 [x] **[Skeptic Selection — GREEN](plan/user-stories/01-skeptic-selection-role-plumbing.md)**

**Mode:** Moderate | **ACs:** 01-01 through 01-05

1. Add `AgentsByRole(role string) map[string]AgentConfig` to `internal/registry/config.go`:
   - Normalize empty `Role` to `RoleReviewer` before comparison
   - Return filtered copy of `agents` map
2. Implement `SelectEligibleSkeptics(reg *Registry, finding reconcile.JSONFinding, n int) []AgentConfig` in `internal/verify/select.go`:
   - Call `reg.AgentsByRole(RoleSkeptic)`
   - Build reviewer model set by resolving each name in `finding.Reviewers` to `AgentConfig.Model`; skip unresolvable names
   - Exclude skeptics whose `Model` is in reviewer model set
   - Sort remaining by agent name (deterministic ordering)
   - Return up to `n` candidates
3. Run T1 after each function: `go test ./internal/verify/... -run TestSelectEligible`
4. Verify all tests pass (T2): `go test ./internal/verify/... ./internal/registry/...`
5. Verify no import cycle: `go build ./...`
6. COMMIT: `git add internal/verify/select.go internal/verify/select_test.go internal/registry/config.go internal/registry/config_test.go && git commit -m "feat(verify): implement AgentsByRole and SelectEligibleSkeptics"`

**Files:** `internal/verify/select.go`, `internal/registry/config.go` | **Duration:** 3h

---

### 1.2.A [x] **[Skeptic Selection — ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-skeptic-selection-role-plumbing.md)**

**Changed Files:** `internal/verify/select.go`, `internal/verify/select_test.go`, `internal/registry/config.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 1.2 skeptic selection`
- prompt: Self-contained brief including:
  - Files to review (absolute paths):
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/select.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/select_test.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/registry/config.go`
  - Checklist (pass verbatim):
    - SECURITY: Auth bypass, injection, data exposure?
    - EDGE CASES: Null, empty, boundaries, concurrent access? (pay special attention to: empty Role normalization, missing reviewer names, n > available candidates)
    - ERROR HANDLING: Missing catches, swallowed errors?
    - PERFORMANCE: N+1, leaks, blocking ops?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings (no CRITICAL/HIGH):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| LOW | select.go:65 / config.go:294 | Returned AgentConfig values shallow-copy reference fields (Scope, *Temperature, etc.) — alias registry backing memory; caller writing through them corrupts the registry. | Deferred to TD-001: deep-copy or document read-only. |
| LOW | select.go:61-66 | Truncation loop re-looks-up `skeptics[name]` (redundant); `len(out) == n` is a brittle invariant. | Addressed in 1.3: capture configs in first pass; use `>=`. |
| LOW | select_test.go:28-39 | `namesOf` reverse-maps via reflect.DeepEqual, fragile if two configs are identical. | Deferred to TD-002: test-only robustness. |

**Action Taken:** No CRITICAL/HIGH. Loop redundancy + `>=` defensiveness folded into 1.3 REFACTOR; aliasing (TD-001) and test-fragility (TD-002) deferred to `tech-debt-captured.md`.

**Deviation note:** Plan 1.3 step 2 suggests a "structured warning" log for unresolvable reviewers, but AC 01-02 specifies they are "skipped silently" (defensive — an agent may have been removed). AC wins: no logging added; `SelectEligibleSkeptics` stays a pure, logger-free function.

---

### 1.3 [x] **[Skeptic Selection — REFACTOR](plan/user-stories/01-skeptic-selection-role-plumbing.md)**

1. Fix CRITICAL/HIGH issues from 1.2.A (if any)
2. Improve code quality: ensure deterministic ordering is documented, empty-role normalization is clear, reviewer-not-found skip is logged (structured warning)
3. Run T1 to verify all tests still pass
4. Validate (T3): `go test ./...`
5. COMMIT: `git add internal/verify/select.go internal/registry/config.go && git commit -m "refactor(verify): address review + clean up select.go"`

**Duration:** 1h

---

### 1.4 [x] **Phase 1 DoD Verification**

**Run all checks — all must pass before gate:**

- [x] `go test ./internal/verify/... ./internal/registry/...` — all passing
- [x] Coverage ≥ 95% on new code: 100% on both `AgentsByRole` and `SelectEligibleSkeptics`
- [x] `go vet ./...` — clean
- [x] `go build ./...` — succeeds (no import cycles)
- [x] `AgentsByRole` and `SelectEligibleSkeptics` APIs match spec (signatures as specified in plan task 1.2)

```
Phase 1 DoD Complete
Auto: [4]/4 | Story-Specific: [1]/1
Manual Review: [x] Code reviewed (1.2.A adversarial subagent — no CRITICAL/HIGH)
```

---

### 1.5 [x] **Phase 1 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 1 (integration-level, not TDD cadence)

**Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 1 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 1 (absolute paths):
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/select.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/select_test.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/registry/config.go`
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: `AgentsByRole` and `SelectEligibleSkeptics` signatures match what Phase 2 (invoke.go) will call — correct return types, parameter shapes?
    - CONFIG SURFACE: New `RoleSkeptic` constant documented, backward-compat for empty Role confirmed?
    - INTEGRATION: `verify` package imports `reconcile` and `registry` but NOT vice-versa — no hidden coupling?
    - PHASE-EXIT CONTRACT: Phase 2 can call `SelectEligibleSkeptics(reg, finding, n)` without rework?
    - REGRESSION: Existing `registry` tests (non-Phase-1 ones) still pass?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Gate review (first pass — CRITICAL found):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| CRITICAL | select.go:34,68 | `SelectEligibleSkeptics` returned bare `[]registry.AgentConfig`; `AgentConfig` has no Name field, so Phase 2 could not recover the skeptic name needed for `reconcile.Verification.Skeptic`. | FIXED before phase exit — return `[]Skeptic{Name, Config}`; tests assert names directly (commit 66f4401). |

**Gate review (re-run after fix — PASSED):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| None | None | None | None |

**Action Taken:** CRITICAL fixed in-phase (Skeptic return type), gate re-run, **Phase gate passed.** Resolved TD-002 as a side effect. Proceeding to gated phase stop.

**Duration:** 15-30 min

---

## Phase 2: Core — Skeptic Invocation (3 days)

**Focus:** Prompt construction, verdict parsing, tool loop invocation, vote aggregation, `min_severity` config

**Files:**
- CREATE: `internal/verify/skeptic.go`
- CREATE: `internal/verify/verdict.go`
- CREATE: `internal/verify/invoke.go`
- CREATE: `internal/verify/votes.go`
- CREATE: `internal/verify/skeptic_test.go`
- CREATE: `internal/verify/verdict_test.go`
- CREATE: `internal/verify/invoke_test.go`
- CREATE: `internal/verify/votes_test.go`
- CREATE: `internal/verify/testdata/true-finding.json`
- CREATE: `internal/verify/testdata/false-finding.json`
- CREATE: `internal/verify/testdata/malformed-response.txt`

**Stories:** [Story 2: Skeptic Invocation & Verdict Parsing](plan/user-stories/02-skeptic-invocation-verdict-parsing.md)

---

### 2.1 [ ] **[Skeptic Invocation — RED](plan/user-stories/02-skeptic-invocation-verdict-parsing.md)**

**Mode:** Moderate | **ACs:** [02-01](plan/acceptance-criteria/02-01-skeptic-prompt-construction.md) [02-02](plan/acceptance-criteria/02-02-verdict-parsing.md) [02-03](plan/acceptance-criteria/02-03-skeptic-invocation.md) [02-04](plan/acceptance-criteria/02-04-failure-isolation.md) [02-05](plan/acceptance-criteria/02-05-budget-forwarding.md) [02-06](plan/acceptance-criteria/02-06-test-coverage.md) [02-07](plan/acceptance-criteria/02-07-verify-min-severity-config.md)

1. Create stub files: `skeptic.go`, `verdict.go`, `invoke.go`, `votes.go` with package declaration and function stubs only
2. Create test fixtures:
   - `internal/verify/testdata/true-finding.json` — a plausible but provably correct finding
   - `internal/verify/testdata/false-finding.json` — a plausible but demonstrably wrong finding
   - `internal/verify/testdata/malformed-response.txt` — LLM output with markdown fences and invalid JSON
3. Write failing tests in `verdict_test.go` — all 7 cases:
   - `TestParseVerdict_Confirmed` — valid JSON with `"confirmed"`
   - `TestParseVerdict_Refuted` — valid JSON with `"refuted"`
   - `TestParseVerdict_Unverifiable` — valid JSON with `"unverifiable"`
   - `TestParseVerdict_MalformedJSON` — malformed JSON → `unverifiable`, raw text in Notes
   - `TestParseVerdict_InvalidEnum` — valid JSON but verdict not in enum → `unverifiable`
   - `TestParseVerdict_EmptyResponse` — empty string → `unverifiable`, `notes:"empty_response"`
   - `TestParseVerdict_ExtraFields` — extra JSON fields silently ignored, verdict extracted
   - `TestParseVerdict_FencedJSON` — verdict wrapped in markdown fences → extracted correctly
4. Write failing tests in `skeptic_test.go`:
   - `TestBuildSkepticPrompt_ContainsAdversarialFraming` — prompt contains "try to disprove"
   - `TestBuildSkepticPrompt_ContainsFindingDetails` — problem, fix, evidence, severity, confidence included
   - `TestBuildSkepticPrompt_ContainsVerdictEnvelopeSpec` — `{"verdict": "...", "reasoning": "..."}` included
   - `TestBuildSkepticPrompt_Deterministic` — same input → same output
5. Write failing tests in `votes_test.go`:
   - `TestAggregateVerdicts_Unanimous` — all confirmed → confirmed
   - `TestAggregateVerdicts_Majority` — 2 confirmed, 1 refuted → confirmed (majority)
   - `TestAggregateVerdicts_Disagreement` — split → `unverifiable` with all reasonings
   - `TestAggregateVerdicts_SingleSkeptic` — single verdict passes through
6. Write failing tests in `invoke_test.go` (mock `ChatCompleter`):
   - `TestInvokeSkeptic_ProviderError` — provider error → `unverifiable`, no error propagated
   - `TestInvokeSkeptic_BudgetTripped` → `unverifiable`, no error propagated
   - `TestInvokeSkeptic_MalformedOutput` → `unverifiable` via `parseVerdict`
7. Write failing test for `min_severity` config (AC 02-07): findings below floor are skipped without invocation
8. Verify all tests fail correctly: `go test ./internal/verify/...`

**Files:** `internal/verify/*_test.go`, `internal/verify/testdata/` | **Duration:** 4h

---

### 2.2 [ ] **[Skeptic Invocation — GREEN](plan/user-stories/02-skeptic-invocation-verdict-parsing.md)**

**Mode:** Moderate | **ACs:** 02-01 through 02-07

1. Implement `parseVerdict(response string) *reconcile.Verification` in `verdict.go`:
   - Attempt `json.Unmarshal`; on failure, scan for `{...}` (handle fenced output); on both fail → `unverifiable` with raw text
   - Validate enum against `{"confirmed", "refuted", "unverifiable"}`; invalid → `unverifiable`
   - Empty response → `unverifiable` with `notes:"empty_response"`
   - Extra fields: silently ignored (default unmarshal behavior)
   - Run T1 after each case: `go test ./internal/verify/... -run TestParseVerdict`
2. Implement `buildSkepticPrompt(finding reconcile.JSONFinding, entries []payload.FileEntry) string` in `skeptic.go`:
   - Role framing: "You are an adversarial skeptic. Your job is to try to disprove the following finding."
   - Finding details: problem, fix, evidence, severity, confidence as markdown
   - Code context: file path + body in fenced blocks (pre-truncated by caller)
   - Tool-access instructions
   - Output spec: `{"verdict": "confirmed|refuted|unverifiable", "reasoning": "..."}`
   - Pure function — no side effects
   - Run T1: `go test ./internal/verify/... -run TestBuildSkepticPrompt`
3. Implement `aggregateVerdicts(perSkeptic []*reconcile.Verification) *reconcile.Verification` in `votes.go`:
   - Count each verdict type; if clear majority → that verdict; else → `unverifiable` with all reasonings
   - Run T1: `go test ./internal/verify/... -run TestAggregateVerdicts`
4. Implement `invokeSkeptic(ctx context.Context, engine *fanout.Engine, skeptic AgentConfig, finding reconcile.JSONFinding, entries []payload.FileEntry) *reconcile.Verification` in `invoke.go`:
   - Build prompt via `buildSkepticPrompt`
   - Construct `fanout.Slot` with per-finding budgets (MaxTurns=10, ToolBudgetBytes=1MB, Timeout=60s from registry config)
   - Call `engine.Run(ctx, []fanout.Slot{slot})`
   - On `Status != OK` → return `&reconcile.Verification{Verdict: "unverifiable", Notes: explanation, Skeptic: skeptic.Name}`
   - On success → pass content to `parseVerdict`, set `Skeptic` field
   - **Never return a non-nil error** — all runtime failures captured in `Verification.Notes`
   - Run T1: `go test ./internal/verify/... -run TestInvokeSkeptic`
5. Add `min_severity` floor check: findings below `verify.min_severity` (default MEDIUM) skip invocation, retain v1 confidence
6. Verify all tests pass (T2): `go test ./internal/verify/...`
7. COMMIT: `git add internal/verify/ && git commit -m "feat(verify): implement skeptic invocation, verdict parsing, vote aggregation"`

**Files:** `internal/verify/skeptic.go`, `internal/verify/verdict.go`, `internal/verify/invoke.go`, `internal/verify/votes.go` | **Duration:** 5h

---

### 2.2.A [ ] **[Skeptic Invocation — ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-skeptic-invocation-verdict-parsing.md)**

**Changed Files:** `internal/verify/skeptic.go`, `internal/verify/verdict.go`, `internal/verify/invoke.go`, `internal/verify/votes.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.2 — this is intentional. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 2.2 skeptic invocation`
- prompt: Self-contained brief including:
  - Files to review (absolute paths):
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/skeptic.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/verdict.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/invoke.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/votes.go`
  - Checklist (pass verbatim):
    - SECURITY: Prompt injection via finding description content? Skeptic response containing crafted content that bypasses enum validation?
    - EDGE CASES: Empty `perSkeptic` slice in `aggregateVerdicts`? nil context in `invokeSkeptic`? `entries` with zero-byte file bodies? fenced JSON in various fence styles (``` vs ~~~)?
    - ERROR HANDLING: Does `invokeSkeptic` ever propagate a runtime error to the caller (it must not)? Are all `Verification.Notes` fields populated with diagnostic info on failure?
    - PERFORMANCE: Large file entries inflating prompt beyond context limit (contract requires pre-truncation — is this documented)? Blocking in `aggregateVerdicts` for large skeptic slices?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| | | | |

**Action Required:**
- CRITICAL/HIGH found → List issues for 2.3, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

---

### 2.3 [ ] **[Skeptic Invocation — REFACTOR](plan/user-stories/02-skeptic-invocation-verdict-parsing.md)**

1. Fix CRITICAL/HIGH issues from 2.2.A (if any)
2. Improve code and tests: ensure `invokeSkeptic` error-capture contract is clearly commented (the only place a comment is warranted), structured logging added at invocation site (skeptic name, finding ID, error class)
3. Run T1: `go test ./internal/verify/...`
4. Validate (T3): `go test ./...`
5. COMMIT: `git add internal/verify/ && git commit -m "refactor(verify): address review + clean up invocation layer"`

**Duration:** 2h

---

### 2.4 [ ] **Phase 2 DoD Verification**

**Run all checks — all must pass before gate:**

- [ ] `go test ./internal/verify/...` — all passing
- [ ] Verdict parsing: all 7 test cases pass (confirmed, refuted, unverifiable, malformed JSON, invalid enum, empty response, extra fields)
- [ ] Invocation: provider error → `unverifiable`, budget trip → `unverifiable`, `invokeSkeptic` never propagates runtime errors
- [ ] Vote aggregation: unanimous, majority, disagreement cases all tested
- [ ] Coverage ≥ 95% on `skeptic.go`, `verdict.go`, `invoke.go`, `votes.go`
- [ ] `go vet ./...` clean; `go build ./...` succeeds
- [ ] Fixture files exist: `testdata/true-finding.json`, `testdata/false-finding.json`, `testdata/malformed-response.txt`

```
Phase 2 DoD Complete
Auto: [_]/6 | Story-Specific: [_]/5 (7 verdict cases + 5 invocation paths + majority rule)
Manual Review: [ ] Code reviewed
```

---

### 2.5 [ ] **Phase 2 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 2

**Spawn a fresh subagent** via the Agent tool to perform this integration review. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 2 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 2 (absolute paths):
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/skeptic.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/verdict.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/invoke.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/votes.go`
    - (plus all `*_test.go` counterparts and `testdata/`)
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: `invokeSkeptic` signature matches what Phase 3 (`pipeline.go`) will call? Returns `*reconcile.Verification`, never errors?
    - CONFIG SURFACE: `verify.min_severity` config key documented and defaulted (MEDIUM)?
    - INTEGRATION: `verify` imports `fanout` — does `fanout` NOT import `verify`?
    - PHASE-EXIT CONTRACT: Phase 3 can call `invokeSkeptic(ctx, engine, skeptic, finding, entries)` without rework?
    - REGRESSION: Phase 1 tests (`select_test.go`) still pass?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| | | | |

**Action Required:**
- CRITICAL/HIGH found → Fix before phase boundary. Re-run gate.
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Phase gate passed" and proceed to phase stop

**Duration:** 15-30 min

---

## Phase 3: Advanced — Confidence v2 & Re-emit (2 days)

**Focus:** Confidence recomputation, artifact emission, gate counter update

**Files:**
- CREATE: `internal/verify/confidence_v2.go`
- CREATE: `internal/verify/emit_verification.go`
- CREATE: `internal/verify/emit_findings.go`
- CREATE: `internal/verify/emit_manifest.go`
- CREATE: `internal/verify/emit_summary.go`
- CREATE: `internal/verify/confidence_v2_test.go`
- CREATE: `internal/verify/emit_test.go`
- MODIFY: `internal/reconcile/gate.go` (update `CountAtOrAbove` to exclude refuted)

**Stories:** [Story 3: Confidence v2 & Re-emit](plan/user-stories/03-confidence-v2-re-emit.md)

---

### 3.1 [ ] **[Confidence v2 & Re-emit — RED](plan/user-stories/03-confidence-v2-re-emit.md)**

**Mode:** Moderate | **ACs:** [03-01](plan/acceptance-criteria/03-01-confidence-v2-recomputation.md) [03-02](plan/acceptance-criteria/03-02-verification-json-emission.md) [03-03](plan/acceptance-criteria/03-03-findings-re-emit.md) [03-04](plan/acceptance-criteria/03-04-manifest-summary-updates.md) [03-05](plan/acceptance-criteria/03-05-gate-excludes-refuted.md)

1. Write failing tests in `confidence_v2_test.go`:
   - `TestConfidenceV2_Confirmed` — confirmed verdict → `VERIFIED`
   - `TestConfidenceV2_Refuted` — refuted verdict → `LOW`
   - `TestConfidenceV2_Unverifiable_High` — unverifiable with HIGH v1 confidence → `HIGH` (unchanged)
   - `TestConfidenceV2_Unverifiable_Medium` — unverifiable with MEDIUM v1 confidence → `MEDIUM` (unchanged)
   - `TestConfidenceV2_NoVerification` — nil `*Verification` (below min_severity) → v1 confidence unchanged
2. Write failing tests in `emit_test.go`:
   - `TestWriteVerification_Schema` — output matches `verification.json` schema (per-finding: skeptic, model, verdict, reasoning, budgets, duration)
   - `TestReEmitFindings_VerificationBlocks` — findings.json gains `verification` blocks for each finding
   - `TestReEmitFindings_RefutedDemoted` — refuted finding has `confidence: LOW`
   - `TestUpdateManifestStage_Idempotent` — "verify" stage not duplicated on re-run
   - `TestUpdateSummaryVerdicts_Counts` — `verdictCounts` added to summary.json
3. Write failing tests for gate counter in `internal/reconcile/gate_test.go` (or new file):
   - `TestCountAtOrAbove_ExcludesRefuted` — refuted findings not counted at any severity
   - `TestCountAtOrAbove_IncludesConfirmed` — confirmed findings counted at/above threshold
   - `TestCountAtOrAbove_V1Finding_NilVerification` — v1 finding (no verification block) counted as non-refuted
4. Verify tests fail correctly

**Files:** `internal/verify/confidence_v2_test.go`, `internal/verify/emit_test.go`, `internal/reconcile/gate_test.go` | **Duration:** 3h

---

### 3.2 [ ] **[Confidence v2 & Re-emit — GREEN](plan/user-stories/03-confidence-v2-re-emit.md)**

**Mode:** Moderate | **ACs:** 03-01 through 03-05

1. Implement `confidenceV2(finding reconcile.JSONFinding) string` in `confidence_v2.go`:
   - `confirmed` → `VERIFIED`
   - `refuted` → `LOW`
   - `unverifiable` → v1 confidence unchanged
   - nil `*Verification` (below min_severity floor) → v1 confidence unchanged
   - Run T1: `go test ./internal/verify/... -run TestConfidenceV2`
2. Implement artifact emitters in `emit_verification.go`, `emit_findings.go`, `emit_manifest.go`, `emit_summary.go`:
   - `WriteVerification(path string, results []VerificationResult) error` — atomic write (temp file + rename)
   - `ReEmitFindings(path string, findings []reconcile.JSONFinding) error` — populate `verification` blocks, recompute confidence; atomic write
   - `UpdateManifestStage(path string) error` — append "verify" to stages idempotently; atomic write
   - `UpdateSummaryVerdicts(path string, counts VerdictCounts) error` — add `verdictCounts` to summary; atomic write
   - Run T1 after each: `go test ./internal/verify/... -run TestWrite... -run TestReEmit... -run TestUpdate...`
3. Update `internal/reconcile/gate.go` `CountAtOrAbove` to exclude findings with `verdict == "refuted"`:
   - Add `requireVerified bool` parameter (false = exclude refuted; true = count only VERIFIED)
   - v1 findings (nil `*Verification`) count as non-refuted, non-VERIFIED
   - Run T1: `go test ./internal/reconcile/... -run TestCountAtOrAbove`
4. Verify all tests pass (T2): `go test ./internal/verify/... ./internal/reconcile/...`
5. COMMIT: `git add internal/verify/ internal/reconcile/gate.go && git commit -m "feat(verify): implement confidence v2, artifact emission, gate counter update"`

**Files:** `internal/verify/confidence_v2.go`, `internal/verify/emit_*.go`, `internal/reconcile/gate.go` | **Duration:** 5h

---

### 3.2.A [ ] **[Confidence v2 & Re-emit — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-confidence-v2-re-emit.md)**

**Changed Files:** `internal/verify/confidence_v2.go`, `internal/verify/emit_verification.go`, `internal/verify/emit_findings.go`, `internal/verify/emit_manifest.go`, `internal/verify/emit_summary.go`, `internal/reconcile/gate.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 3.2 confidence v2 and emit`
- prompt: Self-contained brief including:
  - Files to review (absolute paths):
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/confidence_v2.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/emit_verification.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/emit_findings.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/emit_manifest.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/emit_summary.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/reconcile/gate.go`
  - Checklist (pass verbatim):
    - SECURITY: Atomic writes safe? Temp file left behind on process kill? Race between WriteVerification and ReEmitFindings on the same findings.json?
    - EDGE CASES: `confidenceV2` when `Verification.Verdict` is a non-canonical casing (e.g., "Confirmed")? `UpdateManifestStage` when manifest file does not yet exist? `CountAtOrAbove` when `requireVerified=true` and zero VERIFIED findings?
    - ERROR HANDLING: Is `os.Rename` error surfaced or swallowed? What if the output directory does not exist?
    - PERFORMANCE: `ReEmitFindings` for 1000 findings — any N+1 pattern? Are atomic writes truly non-blocking for concurrent readers?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| | | | |

**Action Required:**
- CRITICAL/HIGH found → List issues for 3.3, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

---

### 3.3 [ ] **[Confidence v2 & Re-emit — REFACTOR](plan/user-stories/03-confidence-v2-re-emit.md)**

1. Fix CRITICAL/HIGH issues from 3.2.A (if any)
2. Improve code: normalize verdict case in `confidenceV2` for robustness; ensure `UpdateManifestStage` handles missing manifest gracefully; document atomic-write POSIX-only assumption
3. Run T1: `go test ./internal/verify/... ./internal/reconcile/...`
4. Validate (T3): `go test ./...`
5. COMMIT: `git add internal/verify/ internal/reconcile/gate.go && git commit -m "refactor(verify): address review + clean up emit layer"`

**Duration:** 1.5h

---

### 3.4 [ ] **Phase 3 DoD Verification**

- [ ] `go test ./internal/verify/... ./internal/reconcile/...` — all passing
- [ ] `confidenceV2` maps: confirmed→VERIFIED, refuted→LOW, unverifiable→v1 unchanged, nil→v1 unchanged
- [ ] All 4 emitters produce valid artifact schemas (verify against [verification-pipeline.md](plan/documentation/verification-pipeline.md))
- [ ] `UpdateManifestStage` idempotent on re-run: "verify" not duplicated
- [ ] `CountAtOrAbove` excludes refuted; `requireVerified` counts only VERIFIED
- [ ] Coverage ≥ 95% on new code; `go vet ./...` clean; `go build ./...` succeeds

```
Phase 3 DoD Complete
Auto: [_]/5 | Story-Specific: [_]/6 (5 confidence cases + 4 emitters + 3 gate cases + idempotency)
Manual Review: [ ] Code reviewed
```

---

### 3.5 [ ] **Phase 3 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 3

**Spawn a fresh subagent** via the Agent tool to perform this integration review. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 3 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 3 (absolute paths):
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/confidence_v2.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/emit_verification.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/emit_findings.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/emit_manifest.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/verify/emit_summary.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/reconcile/gate.go`
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: `confidenceV2` signature, `WriteVerification` schema, `CountAtOrAbove` new signature — do they match what Phase 4 (pipeline.go and gate integration) will call?
    - CONFIG SURFACE: `requireVerified` param and `verdict == "refuted"` exclusion documented?
    - INTEGRATION: `reconcile.gate.go` modified — does this break any existing callers of `CountAtOrAbove` outside the verify package?
    - PHASE-EXIT CONTRACT: Phase 4 can wire `invokeSkeptic` → `confidenceV2` → `WriteVerification` → `ReEmitFindings` without rework?
    - REGRESSION: Phase 1 and 2 tests still pass?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| | | | |

**Action Required:**
- CRITICAL/HIGH found → Fix before phase boundary. Re-run gate.
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Phase gate passed" and proceed to phase stop

**Duration:** 15-30 min

---

## Phase 4: Integration — CLI, MCP, Gate (2 days)

**Focus:** `atcr verify` CLI, `--verify` chaining, `atcr_verify` MCP tool, gate semantics, `--require-verified`

**Files:**
- CREATE: `cmd/atcr/verify.go`
- CREATE: `cmd/atcr/verify_test.go`
- MODIFY: `cmd/atcr/main.go` (register verify subcommand)
- MODIFY: `cmd/atcr/review.go` (add `--verify` flag)
- MODIFY: `cmd/atcr/reconcile.go` (add `--require-verified` flag)
- MODIFY: `internal/mcp/server.go` (register `atcr_verify`)
- MODIFY: `internal/mcp/handlers.go` (add `handleVerify`, update `failingFindings`)
- CREATE: `internal/mcp/handlers_verify_test.go`
- CREATE: `internal/reconcile/gate_matrix_test.go`

**Stories:**
- [Story 4: CLI Command & MCP Tool](plan/user-stories/04-cli-command-mcp-tool.md)
- [Story 5: Gate Semantics](plan/user-stories/05-gate-semantics.md)

---

### 4.1 [ ] **[CLI & Gate — RED](plan/user-stories/04-cli-command-mcp-tool.md)**

**Mode:** Moderate | **ACs:** [04-01](plan/acceptance-criteria/04-01-verify-subcommand.md) [04-02](plan/acceptance-criteria/04-02-review-verify-chaining.md) [04-03](plan/acceptance-criteria/04-03-mcp-verify-tool.md) [04-04](plan/acceptance-criteria/04-04-artifact-consistency-error-handling.md) [04-05](plan/acceptance-criteria/04-05-skip-already-verified.md) [05-01](plan/acceptance-criteria/05-01-gate-filtering-require-verified.md) [05-02](plan/acceptance-criteria/05-02-mcp-parity-matrix-tests.md)

1. Write failing tests in `cmd/atcr/verify_test.go`:
   - `TestVerifyCmd_Exists` — `atcr verify` subcommand registered and help text includes `--fresh`, `--thorough`, `--min-severity`
   - `TestVerifyCmd_MissingReconciledFindings` — clear error message when `reconciled/findings.json` absent
   - `TestVerifyCmd_FreshFlag` — `--fresh` forces re-verification of already-verified findings
   - `TestVerifyCmd_SkipAlreadyVerified` — findings with existing verdict skipped without `--fresh`
2. Write failing tests in `internal/mcp/handlers_verify_test.go`:
   - `TestHandleVerify_MatchesCLI` — identical input produces identical artifacts for MCP and CLI paths
3. Write failing gate matrix tests in `internal/reconcile/gate_matrix_test.go`:
   - 12+ scenarios: 3 verdicts (`confirmed`, `refuted`, `unverifiable`) × 3 severities × 2 flag states (`--fail-on` only vs `--fail-on --require-verified`)
   - `TestGateMatrix_RefutedNeverCounted` — refuted finding never counted regardless of severity or flags
   - `TestGateMatrix_V1FindingNilVerification` — v1-only finding counts as non-refuted
   - `TestGateMatrix_RequireVerified` — only VERIFIED confidence counted when flag set
   - `TestGateMatrix_EmptyVerdictNaturallyLow` — naturally-LOW finding (not refuted) counts if at/above threshold
4. Verify all tests fail correctly

**Files:** `cmd/atcr/verify_test.go`, `internal/mcp/handlers_verify_test.go`, `internal/reconcile/gate_matrix_test.go` | **Duration:** 3h

---

### 4.2 [ ] **[CLI & Gate — GREEN](plan/user-stories/04-cli-command-mcp-tool.md)**

**Mode:** Moderate | **ACs:** 04-01 through 05-02

1. Create `cmd/atcr/verify.go` — Cobra `verifyCmd` with flags:
   - `--fresh` (bool, default false): re-verify already-verified findings
   - `--thorough` (bool, default false): use 3 skeptics + majority rule
   - `--min-severity` (string, default "MEDIUM"): skip findings below this floor
   - Orchestrate: read `reconciled/findings.json` → `SelectEligibleSkeptics` → `invokeSkeptic` (per finding, skip already-verified unless `--fresh`) → `aggregateVerdicts` → `confidenceV2` → emit all 4 artifacts
   - Error: missing `reconciled/findings.json` → clear error message (no stack trace)
   - Run T1: `go test ./cmd/atcr/... -run TestVerifyCmd`
2. Register in `cmd/atcr/main.go`: `rootCmd.AddCommand(verifyCmd)`
3. Add `--verify` flag to `cmd/atcr/review.go`: if set, chain → `reconcileCmd.RunE` → `verifyCmd.RunE` after review completes
4. Add `--require-verified` flag to `cmd/atcr/reconcile.go`: pass to `CountAtOrAbove(requireVerified=true)` in gate logic
5. Register `atcr_verify` in `internal/mcp/server.go` and implement `handleVerify` in `internal/mcp/handlers.go`:
   - Mirrors CLI verify logic
   - Updates `failingFindings` to use updated `CountAtOrAbove` (exclude refuted; honor `requireVerified`)
   - Run T1: `go test ./internal/mcp/... -run TestHandleVerify`
6. Implement skip-already-verified: before calling `invokeSkeptic`, check `finding.Verification != nil && !fresh`; if so, skip with existing verdict
7. Verify all tests pass (T2): `go test ./cmd/atcr/... ./internal/mcp/... ./internal/reconcile/...`
8. COMMIT: `git add cmd/atcr/verify.go cmd/atcr/main.go cmd/atcr/review.go cmd/atcr/reconcile.go internal/mcp/server.go internal/mcp/handlers.go && git commit -m "feat(verify): add atcr verify CLI, MCP tool, --require-verified gate"`

**Files:** `cmd/atcr/verify.go`, `cmd/atcr/main.go`, `cmd/atcr/review.go`, `cmd/atcr/reconcile.go`, `internal/mcp/server.go`, `internal/mcp/handlers.go` | **Duration:** 5h

---

### 4.2.A [ ] **[CLI & Gate — ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-cli-command-mcp-tool.md)**

**Changed Files:** `cmd/atcr/verify.go`, `cmd/atcr/main.go`, `cmd/atcr/review.go`, `cmd/atcr/reconcile.go`, `internal/mcp/server.go`, `internal/mcp/handlers.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 4.2 CLI and gate integration`
- prompt: Self-contained brief including:
  - Files to review (absolute paths):
    - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/verify.go`
    - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/review.go`
    - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/reconcile.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/mcp/handlers.go`
  - Checklist (pass verbatim):
    - SECURITY: Can `--min-severity` be set to an invalid value crashing the process? MCP tool input validation sufficient?
    - EDGE CASES: `--verify` chaining when review or reconcile step fails (should not call verify)? `--fresh` + `--min-severity LOW` combination? Empty `reconciled/` directory?
    - ERROR HANDLING: Does CLI produce clear error (not a stack trace) when `findings.json` missing? MCP handler returns proper JSON-RPC error on failure?
    - PERFORMANCE: `--thorough` with 50 findings × 3 skeptics = 150 LLM calls — is any rate limiting in place? Does the pipeline complete findings in parallel or serial?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| | | | |

**Action Required:**
- CRITICAL/HIGH found → List issues for 4.3, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

---

### 4.3 [ ] **[CLI & Gate — REFACTOR](plan/user-stories/04-cli-command-mcp-tool.md)**

1. Fix CRITICAL/HIGH issues from 4.2.A (if any)
2. Improve code: validate `--min-severity` enum at flag parse time (not at invocation); ensure MCP handler propagates errors as JSON-RPC error codes
3. Run T1: `go test ./cmd/atcr/... ./internal/mcp/...`
4. Validate (T3): `go test ./...`
5. COMMIT: `git add cmd/atcr/ internal/mcp/ && git commit -m "refactor(verify): address review + clean up CLI and MCP integration"`

**Duration:** 1.5h

---

### 4.4 [ ] **[Gate Semantics — RED](plan/user-stories/05-gate-semantics.md)**

**Mode:** Moderate | **ACs:** [05-01](plan/acceptance-criteria/05-01-gate-filtering-require-verified.md) [05-02](plan/acceptance-criteria/05-02-mcp-parity-matrix-tests.md)

(Gate matrix tests were written in 4.1 — verify they cover all 12+ scenarios. Add any missing edge cases here.)

1. Review `gate_matrix_test.go` — ensure coverage of:
   - Naturally-LOW finding (not refuted) counts if at/above threshold
   - `unverifiable` finding counts as non-refuted (retains v1 confidence in gate)
   - v1-only finding (no `*Verification` block) counts at its v1 confidence
   - MCP `failingFindings` mirrors CLI gate logic for all 12 scenarios
2. Add any missing test cases
3. Verify all new/updated tests fail correctly

**Files:** `internal/reconcile/gate_matrix_test.go` | **Duration:** 1h

---

### 4.5 [ ] **[Gate Semantics — GREEN](plan/user-stories/05-gate-semantics.md)**

1. Ensure `CountAtOrAbove` handles all gate matrix edge cases (naturally-LOW, unverifiable, v1-only)
2. Verify MCP `failingFindings` updated to match CLI gate logic exactly (same call to `CountAtOrAbove`)
3. Run T1: `go test ./internal/reconcile/... -run TestGateMatrix`
4. Verify all tests pass (T2): `go test ./internal/reconcile/... ./internal/mcp/...`
5. COMMIT: `git add internal/reconcile/gate.go internal/reconcile/gate_matrix_test.go internal/mcp/handlers.go && git commit -m "feat(verify): complete gate matrix — 12+ scenarios including naturally-LOW and v1-only"`

**Files:** `internal/reconcile/gate.go`, `internal/mcp/handlers.go` | **Duration:** 1.5h

---

### 4.5.A [ ] **[Gate Semantics — ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-gate-semantics.md)**

**Changed Files:** `internal/reconcile/gate.go`, `internal/reconcile/gate_matrix_test.go`, `internal/mcp/handlers.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 4.5 gate semantics`
- prompt: Self-contained brief including:
  - Files to review (absolute paths):
    - `/Users/samestrin/Documents/GitHub/atcr/internal/reconcile/gate.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/reconcile/gate_matrix_test.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/mcp/handlers.go`
  - Checklist (pass verbatim):
    - SECURITY: Can a crafted `confidence` value bypass the gate (e.g., casing, whitespace, extra characters)?
    - EDGE CASES: Zero findings → gate passes (correct)? All findings refuted → gate passes even for `--fail-on CRITICAL`? `--require-verified` with zero VERIFIED findings → gate fails (correct)?
    - ERROR HANDLING: MCP `failingFindings` diverges from CLI `CountAtOrAbove` — any path where they return different counts for identical input?
    - PERFORMANCE: Gate counter called per-request in MCP — any caching or iteration cost concerns for large finding sets?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| | | | |

**Action Required:**
- CRITICAL/HIGH found → List issues for 4.6, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

---

### 4.6 [ ] **[Gate Semantics — REFACTOR](plan/user-stories/05-gate-semantics.md)**

1. Fix CRITICAL/HIGH issues from 4.5.A (if any)
2. Normalize confidence string comparison (trim, lowercase) to defend against malformed input
3. Run T1: `go test ./internal/reconcile/...`
4. Validate (T3): `go test ./...`
5. COMMIT: `git add internal/reconcile/ internal/mcp/handlers.go && git commit -m "refactor(verify): address review + harden gate counter"`

**Duration:** 1h

---

### 4.7 [ ] **Phase 4 DoD Verification**

- [ ] `go test ./cmd/atcr/... ./internal/mcp/... ./internal/reconcile/...` — all passing
- [ ] Gate matrix: 12+ scenarios all passing
- [ ] `atcr verify` CLI: `--fresh`, `--thorough`, `--min-severity` flags functional
- [ ] `atcr review --verify` chains correctly
- [ ] `atcr_verify` MCP tool registered and mirrors CLI
- [ ] `--fail-on <sev> --require-verified` passes/fails correctly
- [ ] Skip-already-verified logic: findings with existing verdict skipped without `--fresh`
- [ ] `go vet ./...` clean; `go build ./...` succeeds

```
Phase 4 DoD Complete
Auto: [_]/7 | Story-Specific: [_]/7 (3 entry points + gate matrix + skip-verified + require-verified + MCP parity)
Manual Review: [ ] Code reviewed
```

---

### 4.8 [ ] **Phase 4 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 4

**Spawn a fresh subagent** via the Agent tool to perform this integration review. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 4 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 4 (absolute paths):
    - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/verify.go`
    - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/main.go`
    - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/review.go`
    - `/Users/samestrin/Documents/GitHub/atcr/cmd/atcr/reconcile.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/mcp/server.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/mcp/handlers.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/reconcile/gate.go`
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: `atcr verify` produces identical `verification.json` and `findings.json` as the MCP path for the same input?
    - CONFIG SURFACE: All new flags (`--fresh`, `--thorough`, `--min-severity`, `--require-verified`) have defaults, help text, and validation?
    - INTEGRATION: `CountAtOrAbove` signature change — are all existing callers in `reconcile` and `mcp` updated?
    - PHASE-EXIT CONTRACT: Phase 5 (report rendering) can read `verification.json` and `findings.json` with verification blocks without rework?
    - REGRESSION: Phases 1, 2, 3 tests still pass?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| | | | |

**Action Required:**
- CRITICAL/HIGH found → Fix before phase boundary. Re-run gate.
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Phase gate passed" and proceed to phase stop

**Duration:** 15-30 min

---

## Phase 5: Validation — Report, Docs, Fixtures (1 day)

**Focus:** Report rendering with verification sections, backward compatibility, verification docs, fixture corpus

**Files:**
- MODIFY: `internal/report/render.go` (add Skeptic section, Refuted section, VERIFIED tier)
- CREATE: `internal/report/testdata/findings-with-verification.json`
- CREATE: `internal/report/testdata/report-v2.md` (golden file)
- CREATE: `internal/report/render_verification_test.go`
- CREATE: `internal/verify/verify_e2e_test.go` (end-to-end: planted true/false findings through the pipeline with a scripted mock skeptic — AC 06-04 Scenario 6)
- CREATE: `docs/verification.md`
- MODIFY: `docs/registry.md` (add `role: skeptic` subsection)
- MODIFY: `docs/findings-format.md` (document verification block)

**Stories:** [Story 6: Report Updates & Documentation](plan/user-stories/06-report-updates-documentation.md)

---

### 5.1 [ ] **[Report & Docs — RED](plan/user-stories/06-report-updates-documentation.md)**

**Mode:** Moderate | **ACs:** [06-01](plan/acceptance-criteria/06-01-report-rendering-with-verification.md) [06-02](plan/acceptance-criteria/06-02-backward-compatibility-v1.md) [06-03](plan/acceptance-criteria/06-03-verification-documentation.md) [06-04](plan/acceptance-criteria/06-04-verification-fixture-corpus.md)

1. Create `internal/report/testdata/findings-with-verification.json` — test fixture with:
   - One VERIFIED (confirmed) finding with skeptic section
   - One LOW (refuted) finding with refutation reasoning
   - One MEDIUM (unverifiable) finding with v1 confidence retained
   - One finding with no `*Verification` block (v1 finding, backward compat test)
2. Write failing tests in `internal/report/render_verification_test.go`:
   - `TestRenderReport_SkepticSection` — VERIFIED findings render skeptic name + reasoning
   - `TestRenderReport_RefutedSection` — refuted findings appear in collapsed "Refuted" section at bottom
   - `TestRenderReport_VerifiedTier` — VERIFIED rendered distinctly (e.g., ✅ badge or distinct heading)
   - `TestRenderReport_V1Backward` — findings without verification block render identically to pre-v2
   - `TestRenderReport_GoldenFile` — full report matches `testdata/report-v2.md` golden file
3. Write failing end-to-end test in `internal/verify/verify_e2e_test.go` (AC [06-04](plan/acceptance-criteria/06-04-verification-fixture-corpus.md) Scenario 6): load `internal/verify/testdata/true-finding.json` and `false-finding.json`, drive each through `invokeSkeptic` → `aggregateVerdicts` → `confidenceV2` with a scripted mock skeptic (`fakeChatCompleter`) returning `confirmed` for the true finding and `refuted` for the false finding; assert true → `confirmed`/`VERIFIED`, false → `refuted`/`LOW` (the "false finding refuted, true finding confirmed" success criterion)
4. Verify tests fail correctly

**Files:** `internal/report/testdata/`, `internal/report/render_verification_test.go`, `internal/verify/verify_e2e_test.go` | **Duration:** 2h

---

### 5.2 [ ] **[Report & Docs — GREEN](plan/user-stories/06-report-updates-documentation.md)**

**Mode:** Moderate | **ACs:** 06-01 through 06-04

1. Update `internal/report/render.go`:
   - Add `VERIFIED` tier rendering: distinct marker (✅ or `[VERIFIED]` prefix) before VERIFIED findings
   - Add Skeptic panel section for VERIFIED findings: skeptic name, model, verdict, reasoning excerpt
   - Add collapsed Refuted section at bottom: refuted findings with skeptic reasoning (never deleted from report)
   - Guard backward compat: findings with nil `*Verification` render identically to v1 report
   - Run T1 after each section: `go test ./internal/report/... -run TestRenderReport`
2. Generate golden file `testdata/report-v2.md` by running the render with `findings-with-verification.json` and saving output
3. Create `docs/verification.md` covering:
   - Pipeline mechanics (placement after reconcile, per-finding cost)
   - Skeptic selection and different-model rule
   - Verdict envelope and verdict types
   - Confidence v2 tier model (VERIFIED > HIGH > MEDIUM > LOW)
   - Gate semantics (`--fail-on`, `--require-verified`)
   - Cost controls (`min_severity`, `verify.votes`, `--fresh`)
4. Update `docs/registry.md`: add `role: skeptic` subsection with example YAML and note on backward compat
5. Update `docs/findings-format.md`: document `verification` block schema
6. Confirm the end-to-end test in `internal/verify/verify_e2e_test.go` passes — all pipeline components (`invokeSkeptic`, `aggregateVerdicts`, `confidenceV2`) from Phases 1–3 already exist, so no new production code is expected: `go test ./internal/verify/... -run TestVerifyE2E`
7. Verify all tests pass (T2): `go test ./internal/report/... ./internal/verify/...`
8. COMMIT: `git add internal/report/ internal/verify/verify_e2e_test.go docs/ && git commit -m "feat(verify): add report v2 rendering with skeptic/refuted sections; add verification.md; add end-to-end planted-finding test"`

**Files:** `internal/report/render.go`, `internal/verify/verify_e2e_test.go`, `docs/verification.md`, `docs/registry.md`, `docs/findings-format.md` | **Duration:** 4h

---

### 5.2.A [ ] **[Report & Docs — ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-report-updates-documentation.md)**

**Changed Files:** `internal/report/render.go`, `internal/report/render_verification_test.go`, `docs/verification.md`

**Spawn a fresh subagent** via the Agent tool to perform this review. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 5.2 report rendering and docs`
- prompt: Self-contained brief including:
  - Files to review (absolute paths):
    - `/Users/samestrin/Documents/GitHub/atcr/internal/report/render.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/report/render_verification_test.go`
    - `/Users/samestrin/Documents/GitHub/atcr/docs/verification.md`
  - Checklist (pass verbatim):
    - SECURITY: Report markdown injection via finding description or skeptic reasoning? Crafted reasoning that escapes the collapsed Refuted section to appear in the main body?
    - EDGE CASES: Rendering when ALL findings are refuted (main body empty)? Finding with empty skeptic reasoning? VERIFIED tier with 100+ findings?
    - ERROR HANDLING: Golden file cascade — does updating `report-v2.md` break tests reading `testdata/report.md` (the v1 golden file)? Separate file names confirmed?
    - PERFORMANCE: Rendering 1000 findings — any O(n²) string building?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| | | | |

**Action Required:**
- CRITICAL/HIGH found → List issues for 5.3, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

---

### 5.3 [ ] **[Report & Docs — REFACTOR](plan/user-stories/06-report-updates-documentation.md)**

1. Fix CRITICAL/HIGH issues from 5.2.A (if any)
2. Use `strings.Builder` throughout render.go to avoid O(n²) concatenation; verify v1 and v2 golden files are separate (`report.md` vs `report-v2.md`)
3. Run T1: `go test ./internal/report/...`
4. Validate (T3): `go test ./...`
5. COMMIT: `git add internal/report/ docs/ && git commit -m "refactor(verify): address review + harden report rendering"`

**Duration:** 1h

---

### 5.4 [ ] **Phase 5 DoD Verification**

- [ ] `go test ./internal/report/...` — all passing including golden file match
- [ ] Backward compat: v1 findings render identically to pre-v2 (no regression)
- [ ] Refuted section collapsed at bottom, never empty-but-visible
- [ ] VERIFIED tier renders distinctly; skeptic panel present for confirmed findings
- [ ] `docs/verification.md` exists and covers all 6 mechanics sections
- [ ] `docs/registry.md` has `role: skeptic` subsection
- [ ] `docs/findings-format.md` has verification block schema
- [ ] End-to-end test `internal/verify/verify_e2e_test.go` passes: planted false finding → `refuted`/`LOW`, planted true finding → `confirmed`/`VERIFIED`
- [ ] `go test ./...` — full suite passing; `go vet ./...` clean; `go build ./...` succeeds

```
Phase 5 DoD Complete
Auto: [_]/7 | Story-Specific: [_]/4 (4 render cases + golden file + 3 doc files)
Manual Review: [ ] Code reviewed
```

---

### 5.5 [ ] **Phase 5 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 5

**Spawn a fresh subagent** via the Agent tool to perform this integration review. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 5 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 5 (absolute paths):
    - `/Users/samestrin/Documents/GitHub/atcr/internal/report/render.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/report/render_verification_test.go`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/report/testdata/findings-with-verification.json`
    - `/Users/samestrin/Documents/GitHub/atcr/internal/report/testdata/report-v2.md`
    - `/Users/samestrin/Documents/GitHub/atcr/docs/verification.md`
    - `/Users/samestrin/Documents/GitHub/atcr/docs/registry.md`
    - `/Users/samestrin/Documents/GitHub/atcr/docs/findings-format.md`
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: `render.go` public API unchanged for callers that do not use verification features?
    - CONFIG SURFACE: `docs/verification.md` covers all user-facing flags (`--fresh`, `--thorough`, `--min-severity`, `--require-verified`) with examples?
    - INTEGRATION: Golden file `report-v2.md` name does NOT collide with `report.md` used by existing render tests?
    - PHASE-EXIT CONTRACT: Full sprint DoD can be declared — all 6 stories' ACs satisfied?
    - REGRESSION: All phases 1-4 tests still passing after render changes?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| | | | |

**Action Required:**
- CRITICAL/HIGH found → Fix before phase boundary. Re-run gate.
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Phase gate passed" and proceed to final validation

**Duration:** 15-30 min

---

## Final Phase: Validation

### Full Sprint Validation Checklist

- [ ] `go test ./...` — entire test suite passing (unit + integration)
- [ ] Coverage: `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out` — ≥ 80% overall; ≥ 95% on `internal/verify/`
- [ ] Lint: `golangci-lint run` — no errors
- [ ] Vet: `go vet ./...` — clean
- [ ] Build: `go build ./...` — succeeds
- [ ] No import cycle: `verify` → `reconcile` and `fanout` (NOT vice-versa)
- [ ] All 28 acceptance criteria checked off in `plan/acceptance-criteria/`
- [ ] All 5 phase gates passed

### Artifact Checklist

- [ ] `reconciled/verification.json` — per-finding: skeptic(s), model(s), verdict, reasoning, budgets, duration
- [ ] `reconciled/findings.json` — re-emitted with `verification` blocks and v2 confidence
- [ ] `reconciled/manifest.json` — `stages` gains `"verify"` (idempotent)
- [ ] `reconciled/summary.json` — gains `verdictCounts`
- [ ] `verify/raw/<skeptic>/transcript.jsonl` — generated by tool loop infrastructure
- [ ] `docs/verification.md` — complete
- [ ] `docs/registry.md` — `role: skeptic` documented
- [ ] `docs/findings-format.md` — verification block documented

### Optional: Targeted Mutation Testing

MUTATION_TOOL = UNAVAILABLE — skip.

If mutation testing becomes available later, target: `internal/verify/verdict.go` (7 verdict parsing paths), `internal/reconcile/gate.go` (counting logic), `internal/verify/confidence_v2.go` (tier mapping).

### Drift Analysis

Compare implementation against [original-requirements.md](plan/original-requirements.md):

- [ ] `atcr verify [<id-or-path>]` CLI matches spec
- [ ] Pipeline placement: runs after `atcr reconcile`, on `reconciled/findings.json`
- [ ] Different-model rule enforced (not left to configuration discipline)
- [ ] Refuted findings demoted to LOW, retained with reasoning, never deleted
- [ ] `--fail-on` counts only non-refuted findings
- [ ] `verify.min_severity` floor honored (default MEDIUM)
- [ ] Skip-already-verified unless `--fresh`
- [ ] `verify.votes` config honored (default 1; `--thorough` forces 3 with majority rule)
- [ ] End-to-end fixture: planted false finding refuted, planted true finding confirmed (`internal/verify/verify_e2e_test.go`)
- [ ] Out-of-scope confirmed absent: no multi-round debate, no code execution, no auto-tuning

### Final Commit

```
git add .planning/
git commit -m "docs(planning): sprint 3.0 adversarial verification — execution complete"
```
