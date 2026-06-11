# Sprint 1.0: atcr-core-review

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 1.0 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

Build v1 of atcr (Agent Team Code Review): a standalone Go binary (CLI + MCP server) that fans a code change out to a panel of heterogeneous LLM reviewer personas, then deterministically reconciles their findings into a single deduplicated, confidence-scored deliverable. Includes a companion Agent Skill that contributes the host-model review and orchestrates the flow.

### Why This Matters

Current code review workflows rely on single-model reviews or prompt-based merging that lacks determinism and confidence scoring. atcr provides a shareable, reproducible review panel that treats multiple LLM perspectives as a confidence signal rather than noise.

### Key Deliverables

- Go binary with 6 subcommands: `review`, `reconcile`, `report`, `range`, `init`, `serve`
- Deterministic reconciler pipeline: discover → normalize → cluster → dedupe → merge → confidence → emit
- Three payload modes: diff, blocks, files with per-agent overrides
- MCP server exposing engine as 5 tools
- Agent Skill for host-model review and orchestration
- Two-tier config: registry.yaml + .atcr/config.yaml with precedence resolution

### Success Criteria

- All 24 acceptance criteria passing (14 unit + 10 integration)
- `go vet ./...` and `golangci-lint run` clean
- Coverage ≥70%
- MCP server responds to all 5 tools via InMemoryTransport
- End-to-end: `atcr review` → `atcr reconcile` → `atcr report` pipeline works with httptest mock provider

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** Moderate 🔄 (complexity 9/12)
**Adversarial Review:** ENABLED 🎯
**Inline-Fix Severities:** CRITICAL/HIGH
**Deferred to Tech Debt:** MEDIUM/LOW
**Execution Mode:** Gated 🚧 (stops at each phase boundary)

Each AC follows a 3-task pattern:
1. **RED** - Write failing tests
2. **GREEN** - Implement minimal code to pass
3. **ADVERSARIAL REVIEW** - Fresh subagent reviews changed files
4. **REFACTOR** - Fix CRITICAL/HIGH issues, improve quality

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

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./internal/<package>/...` |
| T2: Module | After completing element | `go test ./internal/<module>/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

### DoD Verification Checklist

1. Tests (T3): All passing
2. Coverage: ≥70%
3. Lint: `golangci-lint run` clean
4. Vet: `go vet ./...` clean
5. Docs: Updated

### DoD Report Template

```
Story-{N} DoD Complete
Auto: {X}/5 | Story-Specific: {Y}/{Z}
Manual Review: [ ] Code reviewed
```

### Commit Process

Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

---

## Development Standards

### Implementation Standards

**Core Philosophy:** "It's faster to write five lines of code today than to write one line today and then have to edit it in the future."

**Architecture Principles:**
1. **Black Box Interfaces** - Every module is a black box with a clean API
2. **Replaceable Components** - Any module rewritable using only its interface
3. **Single Responsibility Modules** - One module = one clear purpose
4. **Primitive-First Design** - Identify core data types, build complexity through composition
5. **Format/Interface Design** - Simple to implement, semantic meaning over structural complexity

**Go & MCP Specific:**
- Panic safety: goroutines handle recovery
- Defer cleanup: close resources immediately after creation
- Interface segregation: return concrete types, consume interfaces
- Robust protocol handling: validate input, return proper JSON-RPC errors

### Coding Standards

**Naming:**
- Packages: lowercase, single-word
- Exported: `PascalCase`, Unexported: `camelCase`
- Functions: `PascalCase`
- Files: snake_case or simple lowercase

**Error Handling:**
- Return `error` as last parameter
- Wrap with context: `fmt.Errorf("doing action: %w", err)`
- Never ignore errors

**Testing:**
- Table-driven tests for multiple scenarios
- Test files: `*_test.go` in same package
- Integration tests: `//go:build integration` tag

**Quality:**
- `go fmt` or `goimports` before commit
- `golangci-lint run` must pass
- `go vet ./...` must pass

### Git Strategy

**Workflow:** GitHub Flow / Trunk-Based Development
- `main`: single source of truth, always deployable
- `feature/`: short-lived branches

**Commit Messages:** `type(scope): description`
- Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`

**Pull Requests:**
- One logical change per PR
- Size: <400 lines ideally
- Squash and merge to maintain linear history

---

## External Resources

Documentation available in [documentation/](plan/documentation/):
- cli-architecture.md
- reconciler.md
- llm-client-fanout.md
- payload-engine.md
- mcp-server.md
- README.md

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation (Days 1-2)

**Focus:** Go module scaffold, cobra CLI skeleton, internal package boundaries, two-tier config loading with validation.

### 1.1 [ ] **[Scaffold Go module + cobra CLI - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-01 End-to-End Review](plan/acceptance-criteria/01-01-end-to-end-review.md)
   1. Analyze AC, identify testable units
   2. Write tests: `go build` succeeds, `atcr --help` shows subcommands
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 1.2 [ ] **[Scaffold Go module + cobra CLI - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 1.3 [ ] **[Scaffold Go module + cobra CLI - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 1.2]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 1.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 1.4, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.4 [ ] **[Scaffold Go module + cobra CLI - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 1.3 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 1.5 [ ] **[Internal package boundaries - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-01 End-to-End Review](plan/acceptance-criteria/01-01-end-to-end-review.md)
   1. Analyze AC, identify testable units
   2. Write tests: package imports compile, no circular deps
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 20 min

### 1.6 [ ] **[Internal package boundaries - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 40 min

### 1.7 [ ] **[Internal package boundaries - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 1.6]

   **Spawn a fresh subagent** via the Agent tool to perform this review.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.6`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 1.6]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 1.8, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.8 [ ] **[Internal package boundaries - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 1.7 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 1.9 [ ] **[Registry config loading - RED](plan/user-stories/02-agent-configuration.md)**
   **AC:** [02-02 Provider/Agent Registry](plan/acceptance-criteria/02-02-provider-agent-registry.md)
   1. Analyze AC, identify testable units
   2. Write tests: parse valid/invalid YAML, required field validation
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 1.10 [ ] **[Registry config loading - GREEN](plan/user-stories/02-agent-configuration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 1.11 [ ] **[Registry config loading - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-agent-configuration.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 1.10]

   **Spawn a fresh subagent** via the Agent tool to perform this review.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.10`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 1.10]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 1.12, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.12 [ ] **[Registry config loading - REFACTOR](plan/user-stories/02-agent-configuration.md)**
   1. Fix CRITICAL/HIGH issues from 1.11 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 1.13 [ ] **[Project config loading - RED](plan/user-stories/02-agent-configuration.md)**
   **AC:** [02-02 Provider/Agent Registry](plan/acceptance-criteria/02-02-provider-agent-registry.md)
   1. Analyze AC, identify testable units
   2. Write tests: parse project config, embedded defaults
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 1.14 [ ] **[Project config loading - GREEN](plan/user-stories/02-agent-configuration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 1.15 [ ] **[Project config loading - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-agent-configuration.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 1.14]

   **Spawn a fresh subagent** via the Agent tool to perform this review.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.14`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 1.14]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 1.16, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.16 [ ] **[Project config loading - REFACTOR](plan/user-stories/02-agent-configuration.md)**
   1. Fix CRITICAL/HIGH issues from 1.15 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 1.17 [ ] **[Precedence resolution - RED](plan/user-stories/02-agent-configuration.md)**
   **AC:** [02-03 Precedence and Validation](plan/acceptance-criteria/02-03-precedence-and-validation.md)
   1. Analyze AC, identify testable units
   2. Write tests: each override level tested independently
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 1.18 [ ] **[Precedence resolution - GREEN](plan/user-stories/02-agent-configuration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 1.19 [ ] **[Precedence resolution - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-agent-configuration.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 1.18]

   **Spawn a fresh subagent** via the Agent tool to perform this review.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.18`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 1.18]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 1.20, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.20 [ ] **[Precedence resolution - REFACTOR](plan/user-stories/02-agent-configuration.md)**
   1. Fix CRITICAL/HIGH issues from 1.19 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 1.21 [ ] **[Fallback chain validation - RED](plan/user-stories/02-agent-configuration.md)**
   **AC:** [02-03 Precedence and Validation](plan/acceptance-criteria/02-03-precedence-and-validation.md)
   1. Analyze AC, identify testable units
   2. Write tests: DFS cycle detection, dangling ref detection
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 1.22 [ ] **[Fallback chain validation - GREEN](plan/user-stories/02-agent-configuration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 1.23 [ ] **[Fallback chain validation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-agent-configuration.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 1.22]

   **Spawn a fresh subagent** via the Agent tool to perform this review.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.22`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 1.22]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 1.24, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.24 [ ] **[Fallback chain validation - REFACTOR](plan/user-stories/02-agent-configuration.md)**
   1. Fix CRITICAL/HIGH issues from 1.23 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 1.25 [ ] **[atcr init command - RED](plan/user-stories/02-agent-configuration.md)**
   **AC:** [02-01 Init Command](plan/acceptance-criteria/02-01-init-command.md)
   1. Analyze AC, identify testable units
   2. Write tests: creates .atcr/ dir, writes config + 6 persona files
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 1.26 [ ] **[atcr init command - GREEN](plan/user-stories/02-agent-configuration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 1.27 [ ] **[atcr init command - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-agent-configuration.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 1.26]

   **Spawn a fresh subagent** via the Agent tool to perform this review.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.26`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 1.26]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 1.28, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.28 [ ] **[atcr init command - REFACTOR](plan/user-stories/02-agent-configuration.md)**
   1. Fix CRITICAL/HIGH issues from 1.27 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 1.29 [ ] **Phase 1 DoD Validation**
   1. Run `go test ./...` - all passing
   2. Run `go vet ./...` - clean
   3. Run `golangci-lint run` - clean
   4. Verify `go build` succeeds
   5. Verify `atcr --help` shows all subcommands
   6. Verify `atcr init` creates .atcr/ directory with config and persona files
   7. Update metadata.md with Phase 1 completion metrics
   **Duration:** 30 min

### 1.30 [ ] **Phase 1 - GATE: Integration & Exit Review (subagent)**
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
       - PHASE-EXIT CONTRACT: Downstream phases can consume outputs without rework?
       - REGRESSION: Earlier-phase behavior still intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as N.X.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**Phase 1 Complete. Stop and await human review before proceeding to Phase 2.**

---

## Phase 2: Core Systems (Days 3-5)

**Focus:** Git range resolution, three-mode payload engine, findings stream parser, OpenAI-compatible LLM client.

[Continue with Phase 2 tasks following the same pattern...]

---

**Phase 2 Complete. Stop and await human review before proceeding to Phase 3.**

---

## Phase 3: Engines (Days 6-8)

**Focus:** Fan-out concurrency engine, reconciler pipeline, report rendering.

[Continue with Phase 3 tasks...]

---

**Phase 3 Complete. Stop and await human review before proceeding to Phase 4.**

---

## Phase 4: Integration (Days 9-10)

**Focus:** MCP server, Skill definition, end-to-end orchestration validation.

[Continue with Phase 4 tasks...]

---

**Phase 4 Complete. Stop and await human review before proceeding to Phase 5.**

---

## Phase 5: Validation & Docs (Day 11)

**Focus:** Documentation, CI examples, lint/vet clean, final validation.

[Continue with Phase 5 tasks...]

---

## Final Phase: Validation

### Validation Checklist
- [ ] All tests passing (T3)
- [ ] Coverage ≥70%
- [ ] `go vet ./...` clean
- [ ] `golangci-lint run` clean
- [ ] Build succeeds
- [ ] Documentation complete

### Drift Analysis

Compare implementation against [original-requirements.md](plan/original-requirements.md):
- [ ] All 6 user stories addressed
- [ ] All 24 acceptance criteria passing
- [ ] No scope creep beyond original request
- [ ] MVP feature set complete

---

**Sprint Complete. Run `/finalize-sprint` to generate completion report.**
