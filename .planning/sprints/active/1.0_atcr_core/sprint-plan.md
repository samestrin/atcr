# Sprint 1.0: atcr-core-review

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 1.0 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Clarifications

### Phase 1 Clarifications (recorded 2026-06-10)

**Key Decisions:**
- Install golangci-lint locally via Homebrew; lint gate stays mandatory at every phase (DoD requires `golangci-lint run` clean alongside `go vet`).
- Update `.github/workflows/ci.yml` to Go 1.24+ and a compatible golangci-lint action (v8 / golangci-lint v2.x) as part of the Phase 1 scaffold — current pins (Go 1.21, lint v1.59) cannot build a `go 1.24` module.
- Go module path: `github.com/samestrin/atcr` (per coding-standards.md import rules).
- Review-id scheme confirmed as proposed: default `<YYYY-MM-DD>_<branch-slug>`, `--id` full override, `.atcr/latest` one-line pointer file, HHMMSS collision suffix, empty-slug fallback, `filepath.Base` sanitization.

**Scope Boundaries:**
- Push steps skipped during sprint execution (no git remote configured; commit process is local-only). A GitHub remote must be added before `/finalize-sprint`.

**Technical Approach:**
- Existing `docs/*.md` are draft stubs; Phase 5 fleshes them out in place. Existing README gets an update/completion pass in 5.1.
- All quality gates (tests, coverage ≥70%, vet, lint) run locally.

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

### Adversarial Review Protocol (used by every N.x ADVERSARIAL REVIEW task)

Spawn a **fresh subagent** via the Agent tool — never review inline (the subagent has no memory of the implementation; this is intentional, to avoid "I wrote it, it's good" bias):
- subagent_type: `general-purpose`
- description: `Adversarial review: <preceding GREEN task number>`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): the files modified in the preceding GREEN task
  - Checklist (pass verbatim):
    - SECURITY: Auth bypass, injection, data exposure?
    - EDGE CASES: Null, empty, boundaries, concurrent access?
    - ERROR HANDLING: Missing catches, swallowed errors?
    - PERFORMANCE: N+1, leaks, blocking ops?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY a findings table (markdown: | Severity | File:Line | Issue | Fix |), no prose

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
- README.md (index)
- cli-architecture.md
- reconciler.md
- findings-format.md
- llm-client-fanout.md
- payload-engine.md
- range-resolution.md
- configuration-management.md
- mcp-server.md
- testing-patterns.md

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation (Days 1-2)

**Focus:** Go module scaffold, cobra CLI skeleton, internal package boundaries, two-tier config loading with validation.

### 1.1 [x] **[Scaffold Go module + cobra CLI - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-01 End-to-End Review](plan/acceptance-criteria/01-01-end-to-end-review.md)
   1. Analyze AC, identify testable units
   2. Write tests: `go build` succeeds, `atcr --help` shows subcommands
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 1.2 [x] **[Scaffold Go module + cobra CLI - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 1.3 [x] **[Scaffold Go module + cobra CLI - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** cmd/atcr/{main,review,reconcile,report,range,init,serve,main_test}.go, .github/workflows/ci.yml

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
   | MEDIUM | cmd/atcr/main.go:23-33 | Exit-2 contract documented but no code path produces 2 (usage errors map to 1) | SetFlagErrorFunc + coded usage errors |
   | MEDIUM | cmd/atcr/main.go:38-51 | asExitCoder reimplements errors.As; misses Unwrap() []error chains | Replace with errors.As |
   | MEDIUM | cmd/atcr/review.go:20-21, range.go:19-20 | --head and --merge-commit not mutually exclusive; conflict caught only by accident with misleading message | MarkFlagsMutuallyExclusive("head", "merge-commit") |
   | MEDIUM | cmd/atcr/main_test.go | exitCode/asExitCoder logic has zero test coverage | Table-driven exitCode tests |
   | LOW | cmd/atcr/main_test.go | Flag relationships untested | Add group-constraint tests |
   | LOW | cmd/atcr/main.go:16-18 | ExitCode()==0 error prints stderr banner then exits 0 | Skip banner when code 0 |
   | LOW | ci.yml:1-12 | No permissions: block (default GITHUB_TOKEN grant) | permissions: contents: read |
   | LOW | ci.yml:3-7 | No concurrency group; redundant runs queue | Add concurrency group |
   | LOW | ci.yml:34-53 | Dead go.mod guards, two guard styles, standalone vet redundant with golangci govet | Captured as TD-001 |
   | LOW | ci.yml:49 | coverage.out generated but never consumed | Captured as TD-002 |
   | LOW | cmd/atcr/report.go:15 | --format accepts any string; enum validation deferred | Captured as TD-003 (lands with task 3.37) |
   | LOW | cmd/atcr/review.go,range.go | Range-flag block copy-pasted across two commands | Extract addRangeFlags helper |

   **Action Required:** No CRITICAL/HIGH. MEDIUMs + cheap LOWs fixed in 1.4; remaining LOWs appended to `tech-debt-captured.md` (TD-001..TD-003).

### 1.4 [x] **[Scaffold Go module + cobra CLI - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 1.3 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 1.5 [x] **[Internal package boundaries - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-01 End-to-End Review](plan/acceptance-criteria/01-01-end-to-end-review.md)
   1. Analyze AC, identify testable units
   2. Write tests: package imports compile, no circular deps
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 20 min

### 1.6 [x] **[Internal package boundaries - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 40 min

### 1.7 [x] **[Internal package boundaries - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/boundaries_test.go, internal/*/doc.go (9 packages)

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
   | HIGH | internal/boundaries_test.go:46 | TestImports/XTestImports ignored — test files can import forbidden packages undetected | Union all import sets |
   | HIGH | internal/boundaries_test.go:51,60 | Allowlist completeness never enforced — new internal package escapes review | Compare directory listing vs allowlist keys |
   | HIGH | internal/boundaries_test.go:44 | ImportDir non-recursive — subpackages bypass the check | WalkDir the internal/ tree |
   | MEDIUM | internal/boundaries_test.go:44 | Host build constraints exclude tagged files from the check | Parse all .go files with go/parser ImportsOnly |
   | LOW | internal/boundaries_test.go:70 | cmd prefix match also matches cmdutil | Match modulePath+"/cmd/" or equality |
   | LOW | internal/boundaries_test.go:67 | Module-root import falls through checks | Treat imp == modulePath as in-module |
   | LOW | internal/boundaries_test.go:36-38 | runtime.Caller path breaks under -trimpath | Walk up from cwd to go.mod |
   | LOW | internal/boundaries_test.go:21-31 | Allowlist itself never checked for cycles | DFS acyclicity assertion |

   **Action Required:** 3 HIGH — fixed in 1.8 (full rewrite of the boundary test addressing all 8 findings) before proceeding.

### 1.8 [x] **[Internal package boundaries - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 1.7 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 1.9 [x] **[Registry config loading - RED](plan/user-stories/02-agent-configuration.md)**
   **AC:** [02-02 Provider/Agent Registry](plan/acceptance-criteria/02-02-provider-agent-registry.md)
   1. Analyze AC, identify testable units
   2. Write tests: parse valid/invalid YAML, required field validation
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 1.10 [x] **[Registry config loading - GREEN](plan/user-stories/02-agent-configuration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 1.11 [x] **[Registry config loading - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-agent-configuration.md)**
   **Changed Files:** internal/registry/{config.go, config_test.go}

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
   | MEDIUM | config.go:69-78 | Comments-only file: Decode returns io.EOF which is swallowed; zero-value Registry passes validation | Treat io.EOF as the "is empty" error |
   | MEDIUM | config.go:37,132-133 | Explicit timeout_secs: 0 indistinguishable from unset, silently rewritten to 600 | TimeoutSecs *int for parity with Temperature |
   | MEDIUM | config.go:74-78 | Only first YAML document parsed; trailing docs silently discarded | Second Decode must return io.EOF |
   | LOW | config.go:113-115 | Empty-name check runs last; whitespace names pass | TrimSpace check first, both loops |
   | LOW | config.go:89-98 | Provider names never validated | Same TrimSpace check |
   | LOW | config.go:90 | api_key_env format unvalidated | ^[A-Za-z_][A-Za-z0-9_]*$ |
   | LOW | config.go:93-98 | base_url with embedded userinfo accepted (credential in plaintext config) | Reject u.User != nil |
   | LOW | config.go:100-116 | Temperature unbounded | Reject outside [0, 2] |
   | LOW | config_test.go | Missing tests for the above validation paths | Added with fixes |

   **Action Required:** No CRITICAL/HIGH. All 9 findings fixed directly in 1.12 (no deferrals).

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

### 2.1 [ ] **[Range resolution decision tree + default-branch detection - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-02 Git Range Resolution](plan/acceptance-criteria/01-02-git-range-resolution.md)
   1. Analyze AC, identify testable units
   2. Write tests: explicit --base/--head, --merge-commit (base = SHA^), auto merge-base; default-branch probes origin/HEAD → origin/main → origin/master → local main → local master; DetectionMode strings ("explicit", "merge_commit", "auto")
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 2.2 [ ] **[Range resolution decision tree + default-branch detection - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 2.3 [ ] **[Range resolution decision tree + default-branch detection - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.2]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 2.2`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.4, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.4 [ ] **[Range resolution decision tree + default-branch detection - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 2.3 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 2.5 [ ] **[Empty-range + shallow-clone hard errors - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-02 Git Range Resolution](plan/acceptance-criteria/01-02-git-range-resolution.md)
   1. Analyze AC, identify testable units
   2. Write tests: base==head error; 0-commit range error before any provider call; shallow clone detected → hard error with `git fetch --unshallow` guidance; invalid SHA; not a git repository
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 2.6 [ ] **[Empty-range + shallow-clone hard errors - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 2.7 [ ] **[Empty-range + shallow-clone hard errors - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.6]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 2.6`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.8, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.8 [ ] **[Empty-range + shallow-clone hard errors - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 2.7 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.9 [ ] **[atcr range command - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-02 Git Range Resolution](plan/acceptance-criteria/01-02-git-range-resolution.md)
   1. Analyze AC, identify testable units
   2. Write tests: prints Resolution JSON to stdout (base/head SHAs, DetectionMode, CommitCount); exit 0 on success, exit 2 on resolution failure
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 20 min

### 2.10 [ ] **[atcr range command - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 40 min

### 2.11 [ ] **[atcr range command - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.10]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 2.10`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.12, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.12 [ ] **[atcr range command - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 2.11 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.13 [ ] **[Diff payload builder - RED](plan/user-stories/06-payload-mode-selection.md)**
   **AC:** [06-01 Payload Builders](plan/acceptance-criteria/06-01-payload-builders.md)
   1. Analyze AC, identify testable units
   2. Write tests: unified diff output verbatim from `git diff base..head` (temp git repo fixtures); ref validation via `git rev-parse --verify`; argv-only exec (no shell)
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 2.14 [ ] **[Diff payload builder - GREEN](plan/user-stories/06-payload-mode-selection.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 45 min

### 2.15 [ ] **[Diff payload builder - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-payload-mode-selection.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.14]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 2.14`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.16, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.16 [ ] **[Diff payload builder - REFACTOR](plan/user-stories/06-payload-mode-selection.md)**
   1. Fix CRITICAL/HIGH issues from 2.15 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.17 [ ] **[Blocks payload builder + function-context fallback - RED](plan/user-stories/06-payload-mode-selection.md)**
   **AC:** [06-01 Payload Builders](plan/acceptance-criteria/06-01-payload-builders.md)
   1. Analyze AC, identify testable units
   2. Write tests: `--function-context` expansion with real line numbers; per-file fallback to plain `-U10` when function-context fails or yields zero hunks for a changed file; binary file → `[binary file changed: <path>]` marker
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 2.18 [ ] **[Blocks payload builder + function-context fallback - GREEN](plan/user-stories/06-payload-mode-selection.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 2.19 [ ] **[Blocks payload builder + function-context fallback - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-payload-mode-selection.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.18]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 2.18`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.20, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.20 [ ] **[Blocks payload builder + function-context fallback - REFACTOR](plan/user-stories/06-payload-mode-selection.md)**
   1. Fix CRITICAL/HIGH issues from 2.19 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 2.21 [ ] **[Files payload builder - RED](plan/user-stories/06-payload-mode-selection.md)**
   **AC:** [06-01 Payload Builders](plan/acceptance-criteria/06-01-payload-builders.md)
   1. Analyze AC, identify testable units
   2. Write tests: full head-version content with changed-region sentinel markers; deleted file → `[deleted file: <path>]` marker; renamed file content under new path; binary marker
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 2.22 [ ] **[Files payload builder - GREEN](plan/user-stories/06-payload-mode-selection.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 2.23 [ ] **[Files payload builder - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-payload-mode-selection.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.22]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 2.22`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.24, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.24 [ ] **[Files payload builder - REFACTOR](plan/user-stories/06-payload-mode-selection.md)**
   1. Fix CRITICAL/HIGH issues from 2.23 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.25 [ ] **[Payload mode configuration + per-agent override - RED](plan/user-stories/06-payload-mode-selection.md)**
   **AC:** [06-02 Payload Mode Configuration](plan/acceptance-criteria/06-02-payload-mode-configuration.md)
   1. Analyze AC, identify testable units
   2. Write tests: default `blocks`; project config override; registry per-agent `payload:` override wins over project default; enum validation {diff, blocks, files} (lowercase only, invalid → load error)
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 2.26 [ ] **[Payload mode configuration + per-agent override - GREEN](plan/user-stories/06-payload-mode-selection.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 45 min

### 2.27 [ ] **[Payload mode configuration + per-agent override - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-payload-mode-selection.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.26]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 2.26`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.28, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.28 [ ] **[Payload mode configuration + per-agent override - REFACTOR](plan/user-stories/06-payload-mode-selection.md)**
   1. Fix CRITICAL/HIGH issues from 2.27 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.29 [ ] **[Byte budget + deterministic truncation - RED](plan/user-stories/06-payload-mode-selection.md)**
   **AC:** [06-03 Byte Budget and Truncation](plan/acceptance-criteria/06-03-byte-budget-truncation.md)
   1. Analyze AC, identify testable units
   2. Write tests: smallest-first whole-file drop with alphabetical tie-break; zero budget = unlimited; negative budget = usage error (exit 2); truncation recorded in status.json + manifest.json (never silent); exact-fit boundary
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 2.30 [ ] **[Byte budget + deterministic truncation - GREEN](plan/user-stories/06-payload-mode-selection.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 2.31 [ ] **[Byte budget + deterministic truncation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-payload-mode-selection.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.30]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 2.30`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.32, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.32 [ ] **[Byte budget + deterministic truncation - REFACTOR](plan/user-stories/06-payload-mode-selection.md)**
   1. Fix CRITICAL/HIGH issues from 2.31 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.33 [ ] **[Payload template vars + per-mode scope rules - RED](plan/user-stories/06-payload-mode-selection.md)**
   **AC:** [06-04 Payload Templates and Documentation](plan/acceptance-criteria/06-04-payload-templates-documentation.md)
   1. Analyze AC, identify testable units
   2. Write tests: {{.Payload}}, {{.PayloadMode}}, {{.FileCount}}, {{.BaseRef}}, {{.HeadRef}}, {{.AgentName}} render; `Option("missingkey=error")` → unknown variable is an error; payload content containing `{{` treated as data (never parsed); per-mode scope rule injection
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 2.34 [ ] **[Payload template vars + per-mode scope rules - GREEN](plan/user-stories/06-payload-mode-selection.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 2.35 [ ] **[Payload template vars + per-mode scope rules - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-payload-mode-selection.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.34]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 2.34`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.36, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.36 [ ] **[Payload template vars + per-mode scope rules - REFACTOR](plan/user-stories/06-payload-mode-selection.md)**
   1. Fix CRITICAL/HIGH issues from 2.35 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.37 [ ] **[Findings stream parser/writer (atcr-findings/v1) - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-05 Reconciliation Pipeline](plan/acceptance-criteria/01-05-reconciliation-pipeline.md), [05-02 Host Review Findings](plan/acceptance-criteria/05-02-host-review-findings-generation.md)
   1. Analyze ACs, identify testable units
   2. Write tests: strict severity-prefix regex `^(CRITICAL|HIGH|MEDIUM|LOW)\|` skips prose; literal `|` in fields → `/`; short rows padded to 8 (per-source) / 9 (reconciled) columns; `# atcr-findings/v1` header required (unknown version = hard error); comment/blank lines skipped
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 2.38 [ ] **[Findings stream parser/writer (atcr-findings/v1) - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 2.39 [ ] **[Findings stream parser/writer (atcr-findings/v1) - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.38]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 2.38`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.40, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.40 [ ] **[Findings stream parser/writer (atcr-findings/v1) - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 2.39 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 2.41 [ ] **[OpenAI-compatible LLM client + retry policy - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-04 Fan-out Agent Execution](plan/acceptance-criteria/01-04-fanout-agent-execution.md)
   1. Analyze AC, identify testable units
   2. Write tests (httptest provider mocks): POST /chat/completions with Bearer auth from env var at invoke time; retry on 429/5xx (up to 2 retries, ~500ms initial delay, 1.5× backoff); other 4xx fails immediately; per-agent temperature/timeout; API key never logged
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 2.42 [ ] **[OpenAI-compatible LLM client + retry policy - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 2.43 [ ] **[OpenAI-compatible LLM client + retry policy - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.42]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 2.42`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.44, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.44 [ ] **[OpenAI-compatible LLM client + retry policy - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 2.43 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 2.45 [ ] **[Persona resolution chain + six embedded personas - RED](plan/user-stories/02-agent-configuration.md)**
   **AC:** [02-04 Persona Resolution and Override](plan/acceptance-criteria/02-04-persona-resolution-override.md)
   1. Analyze AC, identify testable units
   2. Write tests: six-level chain (--task-message > persona ref > <agent>.md in .atcr/personas/ > <agent>.md in ~/.config/atcr/ > _base.md (project then registry) > embedded); explicit persona ref with no file at any level = hard error; template parse error = exit 1 with file path and line; six embedded personas render with payload vars and per-mode scope rules; personas emit 7 columns (engine appends REVIEWER)
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 2.46 [ ] **[Persona resolution chain + six embedded personas - GREEN](plan/user-stories/02-agent-configuration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 2.47 [ ] **[Persona resolution chain + six embedded personas - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-agent-configuration.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 2.46]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 2.46`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.48, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.48 [ ] **[Persona resolution chain + six embedded personas - REFACTOR](plan/user-stories/02-agent-configuration.md)**
   1. Fix CRITICAL/HIGH issues from 2.47 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 2.49 [ ] **Phase 2 DoD Validation**
   1. Run `go test ./...` - all passing
   2. Run `go vet ./...` - clean
   3. Run `golangci-lint run` - clean
   4. Verify `atcr range` prints Resolution JSON on a temp repo
   5. Verify all three payload builders produce correct output on a fixture repo
   6. Update metadata.md with Phase 2 completion metrics
   **Duration:** 30 min

### 2.50 [ ] **Phase 2 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool (subagent_type: `general-purpose`, description: `Phase 2 gate review`) with a self-contained brief:
   - Files changed during Phase 2 (absolute paths): [LIST]
   - Checklist (pass verbatim, hostile integrator perspective):
     - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
     - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
     - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
     - PHASE-EXIT CONTRACT: Downstream phases can consume outputs without rework?
     - REGRESSION: Earlier-phase behavior still intact?
   - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
   - Required output: ONLY the findings table (markdown), no prose

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

**Phase 2 Complete. Stop and await human review before proceeding to Phase 3.**

---

## Phase 3: Engines (Days 6-8)

**Focus:** Fan-out concurrency engine, reconciler pipeline, report rendering.

### 3.1 [ ] **[Fan-out parallel + serial lanes - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-04 Fan-out Agent Execution](plan/acceptance-criteria/01-04-fanout-agent-execution.md)
   1. Analyze AC, identify testable units
   2. Write tests (httptest + atomic counters): parallel agents run concurrently; serial lane runs agents sequentially with ctx.Err() check before each invocation, concurrent with the parallel lane; global timeout cancels via context; WaitGroup always drains on cancel
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.2 [ ] **[Fan-out parallel + serial lanes - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 2 hours

### 3.3 [ ] **[Fan-out parallel + serial lanes - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.2]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 3.2`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 3.4, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.4 [ ] **[Fan-out parallel + serial lanes - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.3 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.5 [ ] **[Fallback chains + partial-success semantics - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-04 Fan-out Agent Execution](plan/acceptance-criteria/01-04-fanout-agent-execution.md)
   1. Analyze AC, identify testable units
   2. Write tests: primary failure → fallback agent tried (same persona), fallback_used/fallback_from recorded; fallback chain exhausted → agent failed; ≥1 success → exit 0 + partial:true; all fail → nonzero exit
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 3.6 [ ] **[Fallback chains + partial-success semantics - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 3.7 [ ] **[Fallback chains + partial-success semantics - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.6]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 3.6`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 3.8, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.8 [ ] **[Fallback chains + partial-success semantics - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.7 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.9 [ ] **[Per-agent artifacts + merged pool findings - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-04 Fan-out Agent Execution](plan/acceptance-criteria/01-04-fanout-agent-execution.md)
   1. Analyze AC, identify testable units
   2. Write tests: sources/pool/raw/<agent>/{review.md, findings.txt, status.json} written per agent (dirs created at agent start); status.json always written with status ok|failed|timeout (+ truncated/files_dropped fields); engine appends REVIEWER column to persona 7-col output; merged sources/pool/findings.txt; summary.json stats; crash-safe incremental writes
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.10 [ ] **[Per-agent artifacts + merged pool findings - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 3.11 [ ] **[Per-agent artifacts + merged pool findings - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.10]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 3.10`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 3.12, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.12 [ ] **[Per-agent artifacts + merged pool findings - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.11 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.13 [ ] **[Review directory + manifest + ID + latest pointer - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-03 Review Directory Structure](plan/acceptance-criteria/01-03-review-directory-structure.md)
   1. Analyze AC, identify testable units
   2. Write tests: .atcr/reviews/<YYYY-MM-DD>_<branch-slug>/ layout (payload/, sources/, reconciled/); manifest.json fields (base/head SHAs, detection_mode, payload_modes, roster, timestamps); .atcr/latest is a text file with the review id; --id override; --id with path traversal rejected; collision → suffix; empty slug → fallback
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.14 [ ] **[Review directory + manifest + ID + latest pointer - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 3.15 [ ] **[Review directory + manifest + ID + latest pointer - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.14]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 3.14`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 3.16, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.16 [ ] **[Review directory + manifest + ID + latest pointer - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.15 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.17 [ ] **[atcr review command (end-to-end wiring) - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-01 End-to-End Review](plan/acceptance-criteria/01-01-end-to-end-review.md)
   1. Analyze AC, identify testable units
   2. Write tests (integration, httptest mock provider): zero-arg `atcr review` on a feature branch resolves range, builds payloads, fans out, writes artifacts; explicit --base/--head path; exit codes; payload/ recorded per mode
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.18 [ ] **[atcr review command (end-to-end wiring) - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 3.19 [ ] **[atcr review command (end-to-end wiring) - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.18]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 3.18`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 3.20, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.20 [ ] **[atcr review command (end-to-end wiring) - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.19 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.21 [ ] **[Source discovery + normalization - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-05 Reconciliation Pipeline](plan/acceptance-criteria/01-05-reconciliation-pipeline.md)
   1. Analyze AC, identify testable units
   2. Write tests: any child of sources/ containing findings.txt is discovered (open extension point); reconciled/ never an input; --sources allowlist filters immediate children (pool, host, ...); normalization pads short rows, skips comments/blanks
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 3.22 [ ] **[Source discovery + normalization - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 3.23 [ ] **[Source discovery + normalization - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.22]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 3.22`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 3.24, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.24 [ ] **[Source discovery + normalization - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.23 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 3.25 [ ] **[Clustering + Jaccard dedupe + ambiguous sidecar - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-05 Reconciliation Pipeline](plan/acceptance-criteria/01-05-reconciliation-pipeline.md)
   1. Analyze AC, identify testable units
   2. Write tests (fixture corpus): (FILE, LINE±3) clustering incl. delta-3 same cluster / delta-4 different; Jaccard token-set ≥0.7 → merge; gray zone 0.4–0.7 → ambiguous.json (always written, empty array when none; default unmerged); <0.4 → distinct; thresholds fixed in v1
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.26 [ ] **[Clustering + Jaccard dedupe + ambiguous sidecar - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 2 hours

### 3.27 [ ] **[Clustering + Jaccard dedupe + ambiguous sidecar - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.26]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 3.26`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 3.28, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.28 [ ] **[Clustering + Jaccard dedupe + ambiguous sidecar - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.27 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.29 [ ] **[Merge rules + confidence + disagreement + emit - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-05 Reconciliation Pipeline](plan/acceptance-criteria/01-05-reconciliation-pipeline.md)
   1. Analyze AC, identify testable units
   2. Write tests: REVIEWERS comma-joined deduplicated; SEVERITY = max with `disagreement: <lo> vs <hi>` preserved; PROBLEM/FIX = longest; CATEGORY = modal; EST_MINUTES = max; CONFIDENCE categorical (HIGH = 2+ distinct reviewers, MEDIUM = single, LOW = untrusted); emits findings.txt (9-col), findings.json, report.md, summary.json
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.30 [ ] **[Merge rules + confidence + disagreement + emit - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 2 hours

### 3.31 [ ] **[Merge rules + confidence + disagreement + emit - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.30]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 3.30`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 3.32, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.32 [ ] **[Merge rules + confidence + disagreement + emit - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.31 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.33 [ ] **[atcr reconcile --fail-on + one-shot review --fail-on - RED](plan/user-stories/03-ci-integration.md)**
   **AC:** [03-01 Fail-on Severity Threshold](plan/acceptance-criteria/03-01-fail-on-severity-threshold.md), [03-02 CI One-Shot Mode](plan/acceptance-criteria/03-02-ci-one-shot-and-example.md)
   1. Analyze ACs, identify testable units
   2. Write tests: exit 1 when findings at/above SEVERITY threshold survive, 0 below, 2 on usage/config errors; threshold validated against enum before any I/O; one-shot `atcr review --fail-on` runs review + reconcile + gate in-process; exit-code mapping centralized in main()
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 3.34 [ ] **[atcr reconcile --fail-on + one-shot review --fail-on - GREEN](plan/user-stories/03-ci-integration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 3.35 [ ] **[atcr reconcile --fail-on + one-shot review --fail-on - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-ci-integration.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.34]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 3.34`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 3.36, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.36 [ ] **[atcr reconcile --fail-on + one-shot review --fail-on - REFACTOR](plan/user-stories/03-ci-integration.md)**
   1. Fix CRITICAL/HIGH issues from 3.35 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 3.37 [ ] **[Report renderers + atcr report - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-06 Report Rendering](plan/acceptance-criteria/01-06-report-rendering.md)
   1. Analyze AC, identify testable units
   2. Write tests (golden files): md/json/checklist from the same findings.json; zero-findings message; markdown/HTML special chars escaped in md output; --output routing; invalid format error
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.38 [ ] **[Report renderers + atcr report - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 3.39 [ ] **[Report renderers + atcr report - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.38]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 3.38`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 3.40, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.40 [ ] **[Report renderers + atcr report - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.39 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.41 [ ] **Phase 3 DoD Validation**
   1. Run `go test ./...` - all passing
   2. Run `go vet ./...` - clean
   3. Run `golangci-lint run` - clean
   4. Verify end-to-end: `atcr review` → `atcr reconcile` → `atcr report` on a fixture repo with httptest provider
   5. Verify `--fail-on` exit codes (0/1/2) against fixture findings
   6. Update metadata.md with Phase 3 completion metrics
   **Duration:** 30 min

### 3.42 [ ] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool (subagent_type: `general-purpose`, description: `Phase 3 gate review`) with a self-contained brief:
   - Files changed during Phase 3 (absolute paths): [LIST]
   - Checklist (pass verbatim, hostile integrator perspective):
     - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
     - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
     - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
     - PHASE-EXIT CONTRACT: Downstream phases can consume outputs without rework?
     - REGRESSION: Earlier-phase behavior still intact?
   - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
   - Required output: ONLY the findings table (markdown), no prose

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

**Phase 3 Complete. Stop and await human review before proceeding to Phase 4.**

---

## Phase 4: Integration (Days 9-10)

**Focus:** MCP server, Skill definition, end-to-end orchestration validation.

### 4.1 [ ] **[MCP stdio server + stderr discipline - RED](plan/user-stories/04-mcp-integration.md)**
   **AC:** [04-01 MCP Stdio Server](plan/acceptance-criteria/04-01-mcp-stdio-server.md)
   1. Analyze AC, identify testable units
   2. Write tests (InMemoryTransport): initialize handshake; slog writes to stderr only (stdout owned by protocol); stdin close → in-flight requests complete, clean exit 0; malformed JSON-RPC → protocol error response, no crash
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 4.2 [ ] **[MCP stdio server + stderr discipline - GREEN](plan/user-stories/04-mcp-integration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 4.3 [ ] **[MCP stdio server + stderr discipline - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-mcp-integration.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 4.2]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 4.2`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 4.4, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.4 [ ] **[MCP stdio server + stderr discipline - REFACTOR](plan/user-stories/04-mcp-integration.md)**
   1. Fix CRITICAL/HIGH issues from 4.3 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 4.5 [ ] **[Tool registration + typed schemas - RED](plan/user-stories/04-mcp-integration.md)**
   **AC:** [04-02 Tool Registration and Schemas](plan/acceptance-criteria/04-02-tool-registration-schemas.md)
   1. Analyze AC, identify testable units
   2. Write tests: exactly 5 tools (atcr_review, atcr_reconcile, atcr_report, atcr_range, atcr_status); schemas inferred from typed args/result structs via generic mcp.AddTool; unknown/extra arg fields rejected; duplicate registration → error at startup (no panic)
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 4.6 [ ] **[Tool registration + typed schemas - GREEN](plan/user-stories/04-mcp-integration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 4.7 [ ] **[Tool registration + typed schemas - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-mcp-integration.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 4.6]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 4.6`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 4.8, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.8 [ ] **[Tool registration + typed schemas - REFACTOR](plan/user-stories/04-mcp-integration.md)**
   1. Fix CRITICAL/HIGH issues from 4.7 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 4.9 [ ] **[atcr_review + atcr_reconcile handlers - RED](plan/user-stories/04-mcp-integration.md)**
   **AC:** [04-03 Review and Reconcile Handlers](plan/acceptance-criteria/04-03-review-reconcile-handlers.md)
   1. Analyze AC, identify testable units
   2. Write tests (InMemoryTransport): handlers are thin wrappers over the same engine as the CLI; atcr_review returns immediately with {review_id, review_path, status: "running", agent_count} while fan-out continues (completion polled via atcr_status); atcr_reconcile defaults to .atcr/latest, fail_on filters by SEVERITY; invalid fail_on → error before execution; path containment under .atcr/reviews/
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 4.10 [ ] **[atcr_review + atcr_reconcile handlers - GREEN](plan/user-stories/04-mcp-integration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 4.11 [ ] **[atcr_review + atcr_reconcile handlers - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-mcp-integration.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 4.10]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 4.10`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 4.12, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.12 [ ] **[atcr_review + atcr_reconcile handlers - REFACTOR](plan/user-stories/04-mcp-integration.md)**
   1. Fix CRITICAL/HIGH issues from 4.11 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 4.13 [ ] **[atcr_report / atcr_range / atcr_status handlers - RED](plan/user-stories/04-mcp-integration.md)**
   **AC:** [04-04 Report, Range, and Status Handlers](plan/acceptance-criteria/04-04-report-range-status-handlers.md)
   1. Analyze AC, identify testable units
   2. Write tests (InMemoryTransport): atcr_report renders md/json/checklist; invalid format rejected by schema enum (handler enum check as defense in depth); atcr_range returns Resolution JSON; atcr_status reads manifest (corrupt manifest → structured error); git ops via exec.Command argument arrays only
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 4.14 [ ] **[atcr_report / atcr_range / atcr_status handlers - GREEN](plan/user-stories/04-mcp-integration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 4.15 [ ] **[atcr_report / atcr_range / atcr_status handlers - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-mcp-integration.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 4.14]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 4.14`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 4.16, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.16 [ ] **[atcr_report / atcr_range / atcr_status handlers - REFACTOR](plan/user-stories/04-mcp-integration.md)**
   1. Fix CRITICAL/HIGH issues from 4.15 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 4.17 [ ] **[Skill structure + installation - RED](plan/user-stories/05-host-review-via-skill.md)**
   **AC:** [05-01 Skill Structure and Installation](plan/acceptance-criteria/05-01-skill-structure-and-installation.md)
   1. Analyze AC, identify testable units
   2. Write tests: skill/SKILL.md has YAML frontmatter (name, description) and all required sections (structure test in `package skill` using go:embed); input parsing covers git range, branch, and PR reference; no absolute or .claude-specific paths in the skill body
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 4.18 [ ] **[Skill structure + installation - GREEN](plan/user-stories/05-host-review-via-skill.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 4.19 [ ] **[Skill structure + installation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-host-review-via-skill.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 4.18]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 4.18`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 4.20, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.20 [ ] **[Skill structure + installation - REFACTOR](plan/user-stories/05-host-review-via-skill.md)**
   1. Fix CRITICAL/HIGH issues from 4.19 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 4.21 [ ] **[Host review findings generation - RED](plan/user-stories/05-host-review-via-skill.md)**
   **AC:** [05-02 Host Review Findings Generation](plan/acceptance-criteria/05-02-host-review-findings-generation.md)
   1. Analyze AC, identify testable units
   2. Write tests: host findings.txt is full 8-column v1 format with REVIEWER="host" (the Skill writes the complete row; engine-append applies only to pool personas); `# atcr-findings/v1` header; pipe escape and short-row padding validated by the shared stream parser; file-level findings use line 0
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 4.22 [ ] **[Host review findings generation - GREEN](plan/user-stories/05-host-review-via-skill.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 4.23 [ ] **[Host review findings generation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-host-review-via-skill.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 4.22]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 4.22`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 4.24, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.24 [ ] **[Host review findings generation - REFACTOR](plan/user-stories/05-host-review-via-skill.md)**
   1. Fix CRITICAL/HIGH issues from 4.23 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 4.25 [ ] **[Orchestration loop - RED](plan/user-stories/05-host-review-via-skill.md)**
   **AC:** [05-03 Orchestration Loop](plan/acceptance-criteria/05-03-orchestration-loop.md)
   1. Analyze AC, identify testable units
   2. Write tests (integration): sequence `atcr range` → `atcr review` (background, bounded `atcr status` polling — no --wait flag in v1) → host review → `atcr reconcile` → present report.md; zero findings from all sources = success ("no issues found", exit 0); partial pool failure → proceed with partial:true noted
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 4.26 [ ] **[Orchestration loop - GREEN](plan/user-stories/05-host-review-via-skill.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 4.27 [ ] **[Orchestration loop - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-host-review-via-skill.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 4.26]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 4.26`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 4.28, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.28 [ ] **[Orchestration loop - REFACTOR](plan/user-stories/05-host-review-via-skill.md)**
   1. Fix CRITICAL/HIGH issues from 4.27 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 4.29 [ ] **[Adversarial review + ambiguity adjudication - RED](plan/user-stories/05-host-review-via-skill.md)**
   **AC:** [05-04 Adversarial Review and Adjudication](plan/acceptance-criteria/05-04-adversarial-review-and-adjudication.md)
   1. Analyze AC, identify testable units
   2. Write tests: ambiguous.json always written (empty array when no gray-zone clusters); adjudication decisions written to reconciled/adjudication.json with original preserved as ambiguous.original.json; re-invoked merge applies decisions; undecided clusters remain unmerged (conservative default); decision referencing unknown cluster ID rejected
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 4.30 [ ] **[Adversarial review + ambiguity adjudication - GREEN](plan/user-stories/05-host-review-via-skill.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 4.31 [ ] **[Adversarial review + ambiguity adjudication - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-host-review-via-skill.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 4.30]
   Run the **Adversarial Review Protocol** (Sprint Conventions) with a fresh subagent (description: `Adversarial review: 4.30`).

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 4.32, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.32 [ ] **[Adversarial review + ambiguity adjudication - REFACTOR](plan/user-stories/05-host-review-via-skill.md)**
   1. Fix CRITICAL/HIGH issues from 4.31 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 4.33 [ ] **Phase 4 DoD Validation**
   1. Run `go test ./...` - all passing
   2. Run `go vet ./...` - clean
   3. Run `golangci-lint run` - clean
   4. Verify all 5 MCP tools respond via InMemoryTransport
   5. Verify skill/SKILL.md structure test passes
   6. Update metadata.md with Phase 4 completion metrics
   **Duration:** 30 min

### 4.34 [ ] **Phase 4 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool (subagent_type: `general-purpose`, description: `Phase 4 gate review`) with a self-contained brief:
   - Files changed during Phase 4 (absolute paths): [LIST]
   - Checklist (pass verbatim, hostile integrator perspective):
     - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
     - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
     - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
     - PHASE-EXIT CONTRACT: Downstream phases can consume outputs without rework?
     - REGRESSION: Earlier-phase behavior still intact?
   - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
   - Required output: ONLY the findings table (markdown), no prose

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

**Phase 4 Complete. Stop and await human review before proceeding to Phase 5.**

---

## Phase 5: Validation & Docs (Day 11)

**Focus:** Documentation, CI examples, lint/vet clean, final validation.

### 5.1 [ ] **[README rewrite](plan/user-stories/01-cli-review-workflow.md)**
   Rewrite README.md around the actual architecture (panel + reconcile): overview, quickstart (`atcr init` → `atcr review && atcr reconcile`), command table, payload-mode guidance explicitly stating `diff` is the most compact/token-friendly mode and `blocks` is the default for small-model quality, and a CI integration section with a GitHub Actions snippet (YAML-valid).
   **Files:** `README.md` | **Duration:** 1.5 hours

### 5.2 [ ] **[docs/findings-format.md](plan/user-stories/01-cli-review-workflow.md)**
   Write the versioned `atcr-findings/v1` spec: 8-col per-source and 9-col reconciled layouts with examples, severity enum, extraction regex, pipe escaping, short-row padding, version header, additive-only evolution policy.
   **Files:** `docs/findings-format.md` | **Duration:** 1 hour

### 5.3 [ ] **[docs/registry.md](plan/user-stories/02-agent-configuration.md)**
   Write the configuration reference: providers/agents/personas schemas, two-tier files (~/.config/atcr/registry.yaml, .atcr/config.yaml), precedence chain (CLI > project > registry > embedded), fallback-chain validation rules, persona resolution chain.
   **Files:** `docs/registry.md` | **Duration:** 1 hour

### 5.4 [ ] **[docs/payload-modes.md](plan/user-stories/06-payload-mode-selection.md)**
   Write payload-mode guidance: when to use each mode, diff-vs-blocks token trade-offs, per-agent override examples, byte budget + truncation behavior, files-mode changed-region marker syntax.
   **Files:** `docs/payload-modes.md` | **Duration:** 45 min

### 5.5 [ ] **[examples/ci-gate.sh](plan/user-stories/03-ci-integration.md)**
   **AC:** [03-02 CI One-Shot Mode](plan/acceptance-criteria/03-02-ci-one-shot-and-example.md)
   Working CI gate script using `atcr review --fail-on`; passes `bash -n` and `shellcheck` with zero errors; referenced from README CI section.
   **Files:** `examples/ci-gate.sh` | **Duration:** 30 min

### 5.6 [ ] **[Quality gates](plan/user-stories/01-cli-review-workflow.md)**
   1. `go test ./...` - all passing
   2. `go test -coverprofile=coverage.out ./...` - coverage ≥70%
   3. `go vet ./...` - clean
   4. `golangci-lint run` - clean
   **Duration:** 1 hour

### 5.7 [ ] **Phase 5 DoD Validation**
   1. All docs written and linked from README
   2. Verify every documented command/flag exists in the binary (`atcr --help` cross-check)
   3. Update metadata.md with final sprint metrics
   **Duration:** 30 min

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
