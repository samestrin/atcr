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

### Phase 3 Clarifications (recorded 2026-06-10)

**Key Decisions (resolved by source-of-truth + engineering judgment, no blocker):**
- **Review-dir/manifest/latest placement:** lives in `internal/fanout` (not a new `internal/reviewdir`). original-requirements Task #1 locks the internal package set to 9 packages and the boundary allowlist enforces it; Task #7 bundles "review-directory/manifest/latest management" into the fan-out engine; `payload.Manifest` already exists. The AC 01-03 "internal/reviewdir/creator.go" file path is superseded by the locked architecture.
- **Per-agent artifact path:** `sources/pool/raw/agent/<agent-name>/{review.md, findings.txt, status.json}` (AC 01-03/04/05 all agree on the literal `raw/agent/` segment; supersedes the original-requirements `raw/<agent>/` shorthand).
- **Reconcile source discovery (leaf-preference):** a directory's `findings.txt` is a reconcile input only when no subdirectory beneath it also contains a `findings.txt`. This resolves the conflict between reconciler.md's "immediate children" Quick Reference and AC 01-05 Scenario 1's nested `pool/raw/agent/*/findings.txt`: per-agent raw files become the pool inputs, the merged `pool/findings.txt` is written for downstream convenience but is NOT re-discovered (no double-count), `host/findings.txt` is read directly, and `reconciled/` is never an input. Confidence counts distinct REVIEWER values across all discovered rows.
- **Serial lane = sequential execution** of project `serial_agents`; the config model is a `rate_limited` bool + `serial_agents` list (no rps value), so no `golang.org/x/time/rate` dependency is added (keep-deps-small constraint).
- **Engine sets `Finding.Reviewer` = agent name** itself, ignoring any model-supplied 8th column (TD-016 remediation).
- **Reconciled findings.txt is 9 columns** (AC 01-05 spec-alignment note; the "10 columns" heading in reconciler.md/findings-format.md is a doc typo).

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

### 1.12 [x] **[Registry config loading - REFACTOR](plan/user-stories/02-agent-configuration.md)**
   1. Fix CRITICAL/HIGH issues from 1.11 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 1.13 [x] **[Project config loading - RED](plan/user-stories/02-agent-configuration.md)**
   **AC:** [02-02 Provider/Agent Registry](plan/acceptance-criteria/02-02-provider-agent-registry.md)
   1. Analyze AC, identify testable units
   2. Write tests: parse project config, embedded defaults
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 1.14 [x] **[Project config loading - GREEN](plan/user-stories/02-agent-configuration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 1.15 [x] **[Project config loading - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-agent-configuration.md)**
   **Changed Files:** internal/registry/{project.go, project_test.go}

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
   | MEDIUM | project.go:54 | Trailing `---\n` (single logical doc) rejected as "second YAML document" — also affects config.go | Shared strict-decode helper tolerating a null second doc |
   | MEDIUM | project.go:62-76 | Explicit timeout_secs: 0 silently rewritten to 600 | *int, "must be positive" validation |
   | LOW | project.go:59-61 | Empty/whitespace roster entries pass load validation | Reject at load with clear message |
   | LOW | project.go:85-89 | ValidateAgainst(nil) panics | Nil guard |
   | LOW | project.go:40 | Not-found message hardcodes .atcr/config.yaml | Kept: exact message is AC-mandated (01-01 Error Scenario 1); path is included |
   | LOW | project_test.go | Gaps: serial-lane duplicate, trailing ---, empty file, zero timeout | Added with fixes |

   **Action Required:** No CRITICAL/HIGH. All fixed in 1.16 except the AC-mandated error string (kept by design).

### 1.16 [x] **[Project config loading - REFACTOR](plan/user-stories/02-agent-configuration.md)**
   1. Fix CRITICAL/HIGH issues from 1.15 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 1.17 [x] **[Precedence resolution - RED](plan/user-stories/02-agent-configuration.md)**
   **AC:** [02-03 Precedence and Validation](plan/acceptance-criteria/02-03-precedence-and-validation.md)
   1. Analyze AC, identify testable units
   2. Write tests: each override level tested independently
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 1.18 [x] **[Precedence resolution - GREEN](plan/user-stories/02-agent-configuration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 1.19 [x] **[Precedence resolution - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-agent-configuration.md)**
   **Changed Files:** internal/registry/{precedence.go, precedence_test.go, config.go, project.go}

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
   | MEDIUM | precedence.go:38-40 | CLI tier bypasses positive-timeout invariant (ResolveSettings cannot fail) | Return error; validate CLI timeout |
   | MEDIUM | config.go:153-156 | Agent TimeoutSecs eagerly defaulted to 600 — global/project/CLI timeout can never reach an agent | Leave nil; EffectiveTimeoutSecs(Settings) resolves at use |
   | LOW | project.go:34-35 | Stale doc comment about eager defaults | Updated |
   | LOW | precedence.go:35-43 | Empty-string CLI override clobbers lower tiers with the "unset" sentinel | TrimSpace-empty treated as unset |
   | LOW | precedence.go:49,55 | Whitespace YAML values count as set | TrimSpace in applyTier |
   | LOW | config.go:95 | No upper bound on timeout_secs (Duration overflow) | Max 86400 at all tiers |
   | LOW | config.go:16-19 | Constant comment stale / dual-purpose coupling | Comment fixed; coupling gone with the nil-default fix |
   | LOW | precedence_test.go | Missing zero-timeout and absent-registry-globals cases | Added |

   **Action Required:** No CRITICAL/HIGH. All 8 findings fixed in 1.20.

### 1.20 [x] **[Precedence resolution - REFACTOR](plan/user-stories/02-agent-configuration.md)**
   1. Fix CRITICAL/HIGH issues from 1.19 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 1.21 [x] **[Fallback chain validation - RED](plan/user-stories/02-agent-configuration.md)**
   **AC:** [02-03 Precedence and Validation](plan/acceptance-criteria/02-03-precedence-and-validation.md)
   1. Analyze AC, identify testable units
   2. Write tests: DFS cycle detection, dangling ref detection
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 1.22 [x] **[Fallback chain validation - GREEN](plan/user-stories/02-agent-configuration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 1.23 [x] **[Fallback chain validation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-agent-configuration.md)**
   **Changed Files:** internal/registry/{graph.go, graph_test.go, config.go}

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
   | MEDIUM | graph.go:65-73 | Gray-node fall-through could loop forever after a future refactor (no fail-closed guard) | Fail closed with generic cycle error |
   | MEDIUM | graph_test.go:45-51 | 3-node cycle test asserts only "->" substring, not full path | Exact path assertion (deterministic via sorted iteration) |
   | MEDIUM | graph_test.go | Lead-in trim branch (cycle reached via prefix a->b->c->b) untested | Added test asserting "b -> c -> b" |
   | LOW | graph.go:30-38 | First-error-only reporting | Kept: AC mandates exact single-error messages |
   | LOW | graph.go:36,46 | No sentinel errors for programmatic discrimination | ErrDanglingFallback/ErrFallbackCycle added, wrapped with %w |
   | LOW | graph.go:36 | Names interpolated with '%s' (log forging) | Kept: AC mandates the exact quoted format |
   | LOW | graph.go:54 | Plain-arg helper + nil-slice cycle signal fragile | Method with (path, found) return |

   **Action Required:** No CRITICAL/HIGH. Fixed in 1.24 except two LOWs kept by design (AC-mandated message contract).

### 1.24 [x] **[Fallback chain validation - REFACTOR](plan/user-stories/02-agent-configuration.md)**
   1. Fix CRITICAL/HIGH issues from 1.23 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 1.25 [x] **[atcr init command - RED](plan/user-stories/02-agent-configuration.md)**
   **AC:** [02-01 Init Command](plan/acceptance-criteria/02-01-init-command.md)
   1. Analyze AC, identify testable units
   2. Write tests: creates .atcr/ dir, writes config + 6 persona files
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 1.26 [x] **[atcr init command - GREEN](plan/user-stories/02-agent-configuration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 1.27 [x] **[atcr init command - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-agent-configuration.md)**
   **Changed Files:** cmd/atcr/{init.go, init_test.go}, personas/* (8 files), internal/registry/project.go

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
   | HIGH | init.go:38 | Overwrite guard keys only on config.yaml; customized personas silently overwritten when config absent | Guard checks all target paths |
   | MEDIUM | init.go:38 | Non-NotExist Stat errors swallowed (guard bypass) | Surface stat errors |
   | MEDIUM | init.go:38-60 | TOCTOU between concurrent inits | O_CREATE/O_EXCL when not forcing |
   | MEDIUM | init_test.go:54,58 | Permission assertions umask-dependent | Chmod after create pins documented modes |
   | LOW | init.go:52 | WriteFile follows symlinks under --force | Remove target before write when forcing |
   | LOW | personas.go:27-31 | Underlying embed error discarded | Wrap with %w |
   | LOW | init.go:40 | Message hardcodes .atcr/config.yaml | Kept: dir-relative display matches AC text |
   | LOW | init.go:42 | --force warning mixed into stdout | Routed to stderr |
   | LOW | init_test.go:65-74 | _base.md content never asserted | Included in content loop |

   **Action Required:** 1 HIGH — fixed in 1.28 along with all MEDIUM/LOW (one LOW kept by design).

### 1.28 [x] **[atcr init command - REFACTOR](plan/user-stories/02-agent-configuration.md)**
   1. Fix CRITICAL/HIGH issues from 1.27 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 1.29 [x] **Phase 1 DoD Validation** — tests ✅, vet ✅, lint ✅, build ✅, help shows 6 subcommands ✅, init smoke test ✅, coverage 81.4% ✅
   1. Run `go test ./...` - all passing
   2. Run `go vet ./...` - clean
   3. Run `golangci-lint run` - clean
   4. Verify `go build` succeeds
   5. Verify `atcr --help` shows all subcommands
   6. Verify `atcr init` creates .atcr/ directory with config and persona files
   7. Update metadata.md with Phase 1 completion metrics
   **Duration:** 30 min

### 1.30 [x] **Phase 1 - GATE: Integration & Exit Review (subagent)** — GATE: PASS (re-run after fixes)
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

   **Gate findings (first run):** 1 HIGH (docs/registry.md "reserved fields parsed, inert" contradicted strict parser — doc corrected), 4 MEDIUM (project timeout bound fixed; enum validation → TD-004 / planned 2.25+3.33; personas template contract → TD-005 / planned 2.33+2.45; init explicit defaults kept per AC 02-01 → TD-006), 5 LOW (go.mod reclassified, PreRunE chained, boundaries comment fixed, EffectivePayloadMode added; init message kept per AC). **Re-run verdict: GATE: PASS** — all 9 claims independently verified by fresh subagent.

---

**Phase 1 Complete. Stop and await human review before proceeding to Phase 2.**

---

## Phase 2: Core Systems (Days 3-5)

**Focus:** Git range resolution, three-mode payload engine, findings stream parser, OpenAI-compatible LLM client.

### 2.1 [x] **[Range resolution decision tree + default-branch detection - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-02 Git Range Resolution](plan/acceptance-criteria/01-02-git-range-resolution.md)
   1. Analyze AC, identify testable units
   2. Write tests: explicit --base/--head, --merge-commit (base = SHA^), auto merge-base; default-branch probes origin/HEAD → origin/main → origin/master → local main → local master; DetectionMode strings ("explicit", "merge_commit", "auto")
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 2.2 [x] **[Range resolution decision tree + default-branch detection - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 2.3 [x] **[Range resolution decision tree + default-branch detection - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/gitrange/{resolver.go, resolver_test.go}, cmd/atcr/range.go (holistic review of the gitrange unit — covers 2.2, 2.6, and 2.10).

   Fresh subagent (description: `Adversarial review: gitrange`) reviewed the unit.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | resolver.go mergeBase | exit-0/empty stdout could let rev-list fabricate a bogus range | Fixed in 2.4: reject empty merge-base stdout |
   | MEDIUM | resolver.go run | not-a-repo detection matched localized stderr; non-English locale misses it | Fixed in 2.4: pin LC_ALL=C/LANG=C |
   | MEDIUM | resolver.go merge-commit | base=SHA^ first-parent only; undocumented for octopus merges | Deferred → TD-007 (Phase 5 docs) |
   | LOW | resolver.go run | cancelled context reported as generic git failure | Fixed in 2.4: surface ctx.Err() |
   | LOW | resolver.go resolveRef | conflates hard git failure with invalid ref | Deferred → TD-008 |
   | LOW | resolver_test.go | missing cancellation / leading-dash ref tests | Fixed in 2.4: added both tests |

   **Action Required:** No CRITICAL/HIGH. Four findings fixed in 2.4 (empty merge-base guard, locale pinning, ctx cancellation, `--end-of-options` leading-dash hardening + tests); two LOWs deferred (TD-007, TD-008).

### 2.4 [x] **[Range resolution decision tree + default-branch detection - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 2.3 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 2.5 [x] **[Empty-range + shallow-clone hard errors - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-02 Git Range Resolution](plan/acceptance-criteria/01-02-git-range-resolution.md)
   1. Analyze AC, identify testable units
   2. Write tests: base==head error; 0-commit range error before any provider call; shallow clone detected → hard error with `git fetch --unshallow` guidance; invalid SHA; not a git repository
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 2.6 [x] **[Empty-range + shallow-clone hard errors - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 2.7 [x] **[Empty-range + shallow-clone hard errors - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/gitrange/resolver.go (empty-range + shallow-clone hard errors). Reviewed as part of the holistic gitrange adversarial review in 2.3.

   **Action Required:** Covered by the 2.3 gitrange review — empty-range (base==head and 0-commit) and shallow-clone hard errors were in scope. No CRITICAL/HIGH; relevant fixes landed in 2.4. Adversarial review passed.

### 2.8 [x] **[Empty-range + shallow-clone hard errors - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 2.7 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.9 [x] **[atcr range command - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-02 Git Range Resolution](plan/acceptance-criteria/01-02-git-range-resolution.md)
   1. Analyze AC, identify testable units
   2. Write tests: prints Resolution JSON to stdout (base/head SHAs, DetectionMode, CommitCount); exit 0 on success, exit 2 on resolution failure
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 20 min

### 2.10 [x] **[atcr range command - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 40 min

### 2.11 [x] **[atcr range command - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** cmd/atcr/range.go (`atcr range` command wiring). Reviewed as part of the holistic gitrange adversarial review in 2.3.

   **Action Required:** Covered by the 2.3 gitrange review — `atcr range` prints the Resolution JSON and maps resolution failures to exit 1 (flag-misuse stays exit 2 via addRangeFlags). No CRITICAL/HIGH. Adversarial review passed.

### 2.12 [x] **[atcr range command - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 2.11 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.13 [x] **[Diff payload builder - RED](plan/user-stories/06-payload-mode-selection.md)**
   **AC:** [06-01 Payload Builders](plan/acceptance-criteria/06-01-payload-builders.md)
   1. Analyze AC, identify testable units
   2. Write tests: unified diff output verbatim from `git diff base..head` (temp git repo fixtures); ref validation via `git rev-parse --verify`; argv-only exec (no shell)
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 2.14 [x] **[Diff payload builder - GREEN](plan/user-stories/06-payload-mode-selection.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 45 min

### 2.15 [x] **[Diff payload builder - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-payload-mode-selection.md)**
   **Changed Files:** internal/payload/{builder.go, diff.go} (diff builder). Reviewed as part of the holistic payload-builders adversarial review in 2.19.

   **Action Required:** Covered by the 2.19 payload review — `BuildDiff` emits `git diff base..head` verbatim with ref validation. No CRITICAL/HIGH specific to diff mode. Adversarial review passed.

### 2.16 [x] **[Diff payload builder - REFACTOR](plan/user-stories/06-payload-mode-selection.md)**
   1. Fix CRITICAL/HIGH issues from 2.15 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.17 [x] **[Blocks payload builder + function-context fallback - RED](plan/user-stories/06-payload-mode-selection.md)**
   **AC:** [06-01 Payload Builders](plan/acceptance-criteria/06-01-payload-builders.md)
   1. Analyze AC, identify testable units
   2. Write tests: `--function-context` expansion with real line numbers; per-file fallback to plain `-U10` when function-context fails or yields zero hunks for a changed file; binary file → `[binary file changed: <path>]` marker
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 2.18 [x] **[Blocks payload builder + function-context fallback - GREEN](plan/user-stories/06-payload-mode-selection.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 2.19 [x] **[Blocks payload builder + function-context fallback - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-payload-mode-selection.md)**
   **Changed Files:** internal/payload/{builder.go, diff.go, builder_test.go} (holistic review of the payload-builders unit — covers 2.14, 2.18, and 2.22).

   Fresh subagent (description: `Adversarial review: payload builders`) reviewed the unit.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | diff.go name-status parsing | non-ASCII / control-char paths returned C-quoted by git, breaking `git show`/`git diff -- path` (BuildFiles/BuildBlocks hard-fail on e.g. `café.go`) | Fixed in 2.20: `-c core.quotePath=false` on every git invocation + non-ASCII path test |
   | LOW | builder.go renderWithSentinels | file without trailing newline gained a spurious one | Fixed in 2.20: preserve trailing-newline fidelity |
   | LOW | diff.go changedFiles | unguarded `status[0]` index | Fixed in 2.20: empty-status guard |
   | MEDIUM | builder.go renderWithSentinels | files-mode sentinels spoofable by file content | Deferred → TD-009 |
   | LOW | diff.go functionContextFile / isBinary | genuine git errors swallowed into fallback / non-binary | Deferred → TD-010 |
   | LOW | builder.go per-file fan-out | N×4-5 git processes on large changesets | Deferred → TD-011 |

   **Action Required:** 1 HIGH (quoted-path break) fixed in 2.20 with a regression test, plus two LOWs; three findings deferred (TD-009 MEDIUM, TD-010/TD-011 LOW).

### 2.20 [x] **[Blocks payload builder + function-context fallback - REFACTOR](plan/user-stories/06-payload-mode-selection.md)**
   1. Fix CRITICAL/HIGH issues from 2.19 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 2.21 [x] **[Files payload builder - RED](plan/user-stories/06-payload-mode-selection.md)**
   **AC:** [06-01 Payload Builders](plan/acceptance-criteria/06-01-payload-builders.md)
   1. Analyze AC, identify testable units
   2. Write tests: full head-version content with changed-region sentinel markers; deleted file → `[deleted file: <path>]` marker; renamed file content under new path; binary marker
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 2.22 [x] **[Files payload builder - GREEN](plan/user-stories/06-payload-mode-selection.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 2.23 [x] **[Files payload builder - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-payload-mode-selection.md)**
   **Changed Files:** internal/payload/{builder.go, diff.go} (files builder: head content + sentinels + deleted/renamed/binary markers). Reviewed as part of the holistic payload-builders adversarial review in 2.19.

   **Action Required:** Covered by the 2.19 payload review — `BuildFiles` renders full head content with changed-region sentinels and deleted/renamed/binary markers. The quoted-path HIGH (which broke files mode on non-ASCII names) was fixed in 2.20 with a regression test. Adversarial review passed.

### 2.24 [x] **[Files payload builder - REFACTOR](plan/user-stories/06-payload-mode-selection.md)**
   1. Fix CRITICAL/HIGH issues from 2.23 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.25 [x] **[Payload mode configuration + per-agent override - RED](plan/user-stories/06-payload-mode-selection.md)**
   **AC:** [06-02 Payload Mode Configuration](plan/acceptance-criteria/06-02-payload-mode-configuration.md)
   1. Analyze AC, identify testable units
   2. Write tests: default `blocks`; project config override; registry per-agent `payload:` override wins over project default; enum validation {diff, blocks, files} (lowercase only, invalid → load error)
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 2.26 [x] **[Payload mode configuration + per-agent override - GREEN](plan/user-stories/06-payload-mode-selection.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 45 min

### 2.27 [x] **[Payload mode configuration + per-agent override - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-payload-mode-selection.md)**
   **Changed Files:** internal/payload/{resolve.go, resolve_test.go}, internal/registry/{payload.go, payload_test.go, config.go, project.go, precedence.go}.

   Fresh subagent (description: `Adversarial review: payload config`) reviewed the unit.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | precedence.go:53 | CLI `--payload` tier skipped enum validation, contradicting the comment + AC load-time contract (TD-004) | Fixed in 2.28: ResolveSettings now enum-validates the CLI payload_mode override + test |
   | MEDIUM | resolve.go ResolveMode | dead code with a divergent 2-tier precedence vs the real 4-tier ResolveSettings | Fixed in 2.28: removed ResolveMode; precedence stays single-sourced in registry |
   | MEDIUM | payload.go / resolve.go | duplicated enum across registry+payload has no drift guard | Deferred → TD-012 (cross-package parity test needs fanout/mcp) |
   | LOW | payload_test.go | whitespace-around-valid value untested | Fixed in 2.28: added `"  diff  "` validate+resolve test |

   **Action Required:** 1 HIGH (CLI enum gap) fixed in 2.28; one MEDIUM resolved by deleting dead code; enum-drift guard deferred (TD-012). Per-agent > project > default resolution is covered by `registry.EffectivePayloadMode` tests (precedence_test.go).

### 2.28 [x] **[Payload mode configuration + per-agent override - REFACTOR](plan/user-stories/06-payload-mode-selection.md)**
   1. Fix CRITICAL/HIGH issues from 2.27 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.29 [x] **[Byte budget + deterministic truncation - RED](plan/user-stories/06-payload-mode-selection.md)**
   **AC:** [06-03 Byte Budget and Truncation](plan/acceptance-criteria/06-03-byte-budget-truncation.md)
   1. Analyze AC, identify testable units
   2. Write tests: smallest-first whole-file drop with alphabetical tie-break; zero budget = unlimited; negative budget = usage error (exit 2); truncation recorded in status.json + manifest.json (never silent); exact-fit boundary
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 2.30 [x] **[Byte budget + deterministic truncation - GREEN](plan/user-stories/06-payload-mode-selection.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 2.31 [x] **[Byte budget + deterministic truncation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-payload-mode-selection.md)**
   **Changed Files:** internal/payload/{budget.go, budget_test.go, manifest.go, manifest_test.go}, internal/fanout/{status.go, status_test.go}.

   Fresh subagent (description: `Adversarial review: byte budget`) reviewed the unit.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | budget.go drop loop | keyed drops by Path → over-drop / miscount when two entries share a path | Fixed in 2.32: drop by entry index + duplicate-path test |
   | MEDIUM | budget.go fast paths | returned input slice aliased the caller's backing array | Fixed in 2.32: copyEntries on all return paths |
   | MEDIUM | manifest.go/status.go | non-atomic in-place writes risk a half-written file on crash | Fixed in 2.32: temp-file + rename atomic write |
   | MEDIUM | manifest.go | nil PerAgentPayload marshals as null, not {} | Fixed in 2.32: normalize nil map → {} |
   | MEDIUM | budget.go sum | int64 overflow could skip truncation silently | Deferred → TD-013 (unreachable with real file sizes) |
   | LOW | status.go | files_dropped omitempty contradicted never-silent invariant | Fixed in 2.32: dropped omitempty, normalize non-nil |
   | LOW | budget_test.go | no duplicate-path / zero-size coverage | Fixed in 2.32: added both tests |

   **Action Required:** 1 HIGH (duplicate-path miscount) fixed in 2.32 with a regression test, plus three MEDIUM and two LOW; overflow guard deferred (TD-013).

### 2.32 [x] **[Byte budget + deterministic truncation - REFACTOR](plan/user-stories/06-payload-mode-selection.md)**
   1. Fix CRITICAL/HIGH issues from 2.31 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.33 [x] **[Payload template vars + per-mode scope rules - RED](plan/user-stories/06-payload-mode-selection.md)**
   **AC:** [06-04 Payload Templates and Documentation](plan/acceptance-criteria/06-04-payload-templates-documentation.md)
   1. Analyze AC, identify testable units
   2. Write tests: {{.Payload}}, {{.PayloadMode}}, {{.FileCount}}, {{.BaseRef}}, {{.HeadRef}}, {{.AgentName}} render; `Option("missingkey=error")` → unknown variable is an error; payload content containing `{{` treated as data (never parsed); per-mode scope rule injection
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 2.34 [x] **[Payload template vars + per-mode scope rules - GREEN](plan/user-stories/06-payload-mode-selection.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 2.35 [x] **[Payload template vars + per-mode scope rules - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-payload-mode-selection.md)**
   **Changed Files:** internal/payload/{template.go, scope.go, template_test.go}. docs/payload-modes.md already satisfies AC 06-04 (decision table + per-mode guidance from the Phase 1 stub; Phase 5 polishes).

   Fresh subagent (description: `Adversarial review: templates`) reviewed the unit.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH→info | template.go RenderPrompt | persona `{{define}}`/`{{template}}` directives are honored | Not a vuln: personas are developer-controlled/trusted per AC 06-04; documented as intended in 2.36. Untrusted diff reaches only `{{.Payload}}` as data (proven by new round-trip test) |
   | MEDIUM | template.go missingkey comment | comment implied missingkey guards struct fields (it only guards map keys) | Fixed in 2.36: comment corrected |
   | MEDIUM | template.go unknown-field regex | Go-version-coupled error wording | Already covered: TestRenderPrompt_UnknownVar asserts exact Field extraction (golden test breaks if wording changes) |
   | LOW | template.go Error() | `switch{}` boolean precedence readability | Fixed in 2.36: explicit if-guards |
   | LOW | template_test.go | payload-with-directives threat not asserted | Fixed in 2.36: added directive round-trip test |
   | LOW | template_test.go docs check | relative-path docs test is non-hermetic | Kept: AC 06-04 test case 10 mandates a docs-existence check |

   **Action Required:** No genuine CRITICAL/HIGH — the flagged directive behavior is safe under the trusted-persona model and now documented + proven. MEDIUM/LOW addressed inline or already covered. Adversarial review passed. (TD-005 typed PayloadContext contract now defined; all-personas-render test lands in 2.45.)

### 2.36 [x] **[Payload template vars + per-mode scope rules - REFACTOR](plan/user-stories/06-payload-mode-selection.md)**
   1. Fix CRITICAL/HIGH issues from 2.35 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 2.37 [x] **[Findings stream parser/writer (atcr-findings/v1) - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-05 Reconciliation Pipeline](plan/acceptance-criteria/01-05-reconciliation-pipeline.md), [05-02 Host Review Findings](plan/acceptance-criteria/05-02-host-review-findings-generation.md)
   1. Analyze ACs, identify testable units
   2. Write tests: strict severity-prefix regex `^(CRITICAL|HIGH|MEDIUM|LOW)\|` skips prose; literal `|` in fields → `/`; short rows padded to 8 (per-source) / 9 (reconciled) columns; `# atcr-findings/v1` header required (unknown version = hard error); comment/blank lines skipped
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 2.38 [x] **[Findings stream parser/writer (atcr-findings/v1) - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 2.39 [x] **[Findings stream parser/writer (atcr-findings/v1) - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/stream/{parser.go, writer.go, parser_test.go, writer_test.go}.

   Fresh subagent (description: `Adversarial review: findings stream`) reviewed the unit.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | writer.go escapeField | embedded newline in a field split a finding across physical lines → silent data loss / unstable round-trip | Fixed in 2.40: escapeField neutralizes CR/LF → space + newline-escape test |
   | HIGH | writer.go fieldsFor | comma inside a reviewer name round-trips as multiple reviewers, forging CONFIDENCE | Fixed in 2.40: sanitize commas in reviewer names before join + test |
   | HIGH | parser.go column check | a valid finding written with a trailing pipe was discarded as malformed | Fixed in 2.40: trim trailing-empty columns before count check + test |
   | MEDIUM | parser.go version match | `# atcr-findings/v1x` loosely matched as unknown-version | Deferred → TD-014 (reasonable v1 classification) |
   | LOW | parser.go splitFileLine | bare trailing colon (`a.go:`) kept in File | Fixed in 2.40: strip empty line-number colon |
   | LOW | parser.go/writer.go | control bytes beyond CR/LF pass through | Deferred → TD-014 |

   **Action Required:** 1 CRITICAL + 2 HIGH (all wire-contract integrity defects) fixed in 2.40 with regression tests; one LOW fixed inline; two minor items deferred (TD-014). The format is the public contract, so these were fixed, not deferred.

### 2.40 [x] **[Findings stream parser/writer (atcr-findings/v1) - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 2.39 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 2.41 [x] **[OpenAI-compatible LLM client + retry policy - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-04 Fan-out Agent Execution](plan/acceptance-criteria/01-04-fanout-agent-execution.md)
   1. Analyze AC, identify testable units
   2. Write tests (httptest provider mocks): POST /chat/completions with Bearer auth from env var at invoke time; retry on 429/5xx (up to 2 retries, ~500ms initial delay, 1.5× backoff); other 4xx fails immediately; per-agent temperature/timeout; API key never logged
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 2.42 [x] **[OpenAI-compatible LLM client + retry policy - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 2.43 [x] **[OpenAI-compatible LLM client + retry policy - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/llmclient/{client.go, client_test.go}.

   Fresh subagent (description: `Adversarial review: LLM client`) reviewed the unit. The reviewer confirmed the core contract holds: key resolved at invoke time, key never in any error, retry only on 429/5xx, 3 attempts max, body re-created per attempt, backoff respects context, 200+badjson fails without retry, bodies drained/closed.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | client.go retry loop | a retryable status on the LAST attempt fell through to a bare "HTTP 502"; "exhausted retries" message was dead code | Fixed in 2.44: last-attempt retryable returns the exhausted-retries error + assertion |
   | MEDIUM | client.go New | default client auto-followed redirects, forwarding the Bearer header on a same-host 3xx | Fixed in 2.44: CheckRedirect → ErrUseLastResponse (3xx is a hard failure) |
   | MEDIUM | client.go | base_url with embedded userinfo could leak via wrapped errors | Deferred → TD-015 (registry already rejects userinfo at load) |
   | LOW | client.go decode | trailing JSON garbage accepted; empty content indistinguishable | Accepted: empty completion is valid; trailing-garbage risk negligible |
   | LOW | client_test.go | timeout test weak; no cancel-during-backoff / race coverage | Fixed in 2.44: timeout asserts DeadlineExceeded+promptness, added cancel-during-backoff test, verified `go test -race` clean |

   **Action Required:** 1 HIGH (dead exhausted-retries path) + 1 MEDIUM (redirect Bearer forwarding) fixed in 2.44 with tests; base_url-cred leak deferred (TD-015, already mitigated by registry). Adversarial review passed.

### 2.44 [x] **[OpenAI-compatible LLM client + retry policy - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 2.43 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 2.45 [x] **[Persona resolution chain + six embedded personas - RED](plan/user-stories/02-agent-configuration.md)**
   **AC:** [02-04 Persona Resolution and Override](plan/acceptance-criteria/02-04-persona-resolution-override.md)
   1. Analyze AC, identify testable units
   2. Write tests: six-level chain (--task-message > persona ref > <agent>.md in .atcr/personas/ > <agent>.md in ~/.config/atcr/ > _base.md (project then registry) > embedded); explicit persona ref with no file at any level = hard error; template parse error = exit 1 with file path and line; six embedded personas render with payload vars and per-mode scope rules; personas emit 7 columns (engine appends REVIEWER)
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 2.46 [x] **[Persona resolution chain + six embedded personas - GREEN](plan/user-stories/02-agent-configuration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 2.47 [x] **[Persona resolution chain + six embedded personas - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-agent-configuration.md)**
   **Changed Files:** internal/registry/{persona.go, persona_test.go}, internal/payload/personas_render_test.go.

   Fresh subagent (description: `Adversarial review: persona resolution`) reviewed the unit.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | persona.go readNonEmpty | symlinked persona file was followed and read into the LLM prompt (exfiltration vector) | Fixed in 2.48: lstat + skip symlinks with warning + test |
   | LOW | persona.go validateName | explicit ref `_base` resolved to the shared base file | Fixed in 2.48: `_base` reserved-name rejection + test |
   | LOW | persona.go validateName | dot-prefixed names (`.`, `.hidden`) accepted | Fixed in 2.48: reject leading-dot names |
   | LOW | persona_test.go | agentName traversal / symlink / `_base` ref untested | Fixed in 2.48: added all three tests |
   | LOW | persona.go taskMessage early return | names unvalidated on the task-message path | Accepted: taskMessage wins outright and the names never touch the FS; documented with a comment |

   **Action Required:** No CRITICAL/HIGH. 1 MEDIUM (symlink follow) + LOWs fixed in 2.48 with tests. Persona/agent names are sanitized against path traversal, dotfiles, and the reserved `_base`. (TD-005 resolved: all six embedded personas + _base render against PayloadContext.)

### 2.48 [x] **[Persona resolution chain + six embedded personas - REFACTOR](plan/user-stories/02-agent-configuration.md)**
   1. Fix CRITICAL/HIGH issues from 2.47 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 2.49 [x] **Phase 2 DoD Validation**
   1. Run `go test ./...` - all passing
   2. Run `go vet ./...` - clean
   3. Run `golangci-lint run` - clean
   4. Verify `atcr range` prints Resolution JSON on a temp repo
   5. Verify all three payload builders produce correct output on a fixture repo
   6. Update metadata.md with Phase 2 completion metrics
   **Duration:** 30 min

### 2.50 [x] **Phase 2 - GATE: Integration & Exit Review (subagent)** — GATE: PASS (re-run after fixes)
   **Scope:** All files changed during Phase 2 (integration-level, not TDD cadence)

   Fresh subagent (description: `Phase 2 gate review`) ran a hostile-integrator review over all Phase 2 files.

   **First-run findings (HIGH fixed before boundary, then gate re-run):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | payload/builder.go + budget.go | builder returned a flat string but ApplyByteBudget needs []FileEntry — no exported bridge; Phase 3 could not apply truncation / derive FileCount / fill status fields | Fixed: added exported `BuildEntries(ctx, mode, repo, base, head) []FileEntry`; BuildBlocks/BuildFiles delegate via `joinEntries`; BuildDiff stays verbatim |
   | HIGH | payload/template.go FileCount | no exported producer for FileCount / status truncation fields | Fixed: `len(BuildEntries(...))` supplies FileCount; Truncation from ApplyByteBudget supplies status fields (test TestBuildEntries_BudgetIntegration) |
   | MEDIUM | stream parser | no per-source→reconciled Finding migration helper | Fixed: added `Finding.AsReconciled(reviewers, confidence)` + round-trip test |
   | MEDIUM | stream/parser.go | engine-appends-REVIEWER is convention-only; a model could self-attribute via a padded 8th column | Deferred → TD-016 (Phase 3 fan-out engine sets Reviewer from the agent name) |
   | MEDIUM | payload Build typed vs config string | mode seam requires routing through ParseMode | Accepted by design (typed at the builder boundary; Phase 3 calls ParseMode) |
   | LOW | payload/registry enum duplication | already tracked | Deferred → TD-012 |

   **Re-run verdict: GATE: PASS** — fresh subagent verified `BuildEntries` closes the budget/FileCount/status seam, BuildBlocks/BuildFiles remain byte-identical (confirmed via git diff of the pre-refactor builder), BuildDiff stays verbatim, and `AsReconciled` round-trips. No regression to Phase 1 config behavior. Sole remaining note: one LOW (a tautological equivalence test) — no action required.

   **Action Required:** No CRITICAL/HIGH remain. HIGHs fixed before the boundary and the gate re-ran to PASS; MEDIUM/LOW deferred (TD-012, TD-016) or accepted by design.
   **Duration:** 15-30 min

---

**Phase 2 Complete. Stop and await human review before proceeding to Phase 3.**

---

## Phase 3: Engines (Days 6-8)

**Focus:** Fan-out concurrency engine, reconciler pipeline, report rendering.

### 3.1 [x] **[Fan-out parallel + serial lanes - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-04 Fan-out Agent Execution](plan/acceptance-criteria/01-04-fanout-agent-execution.md)
   1. Analyze AC, identify testable units
   2. Write tests (httptest + atomic counters): parallel agents run concurrently; serial lane runs agents sequentially with ctx.Err() check before each invocation, concurrent with the parallel lane; global timeout cancels via context; WaitGroup always drains on cancel
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.2 [x] **[Fan-out parallel + serial lanes - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 2 hours

### 3.3 [x] **[Fan-out parallel + serial lanes - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/fanout/engine.go, internal/fanout/engine_test.go
   Fresh subagent (description: `Adversarial review: 3.2`) reviewed the scheduler.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | engine.go invokeAgent | Timeout vs failed misclassified by inferring from ambient ctx.Err() instead of the returned error | Fixed in 3.4: classifyStatus(err) via errors.Is(DeadlineExceeded/Canceled); else failed |
   | HIGH | engine.go invokeSlot | ctx-cancel mid-chain overwrote prior real failure's diagnostics with a generic timeout | Fixed in 3.4: preserve prior last.Err; only synthesize when no attempt ran |
   | MEDIUM | engine.go invokeSlot | context.Canceled reported as timeout (conflates abort with deadline) | Fixed in 3.4: classifyStatus handles both as timeout intentionally (documented) |
   | MEDIUM | engine.go invokeSlot | truncation/payload provenance only restored when PayloadMode=="" — fallback failure recorded substitute's provenance | Fixed in 3.4: always stamp primary's PayloadMode/Truncation on slot failure |
   | MEDIUM | engine.go Run | unbounded parallel-lane goroutine fan-out, no concurrency cap | Deferred → TD-017 |
   | LOW | engine.go | nil-completer guard, DurationMS=0 on short-circuit, divergent slot capture | Deferred → TD-018 |

   **Action Required:** 2 HIGH + 2 correctness MEDIUM fixed in 3.4 (classification on error, last-error preservation, canceled/deadline handling, primary provenance) with new tests (TestClassifyStatus, TestInvokeAgent_RealErrorUnderCancelledCtx...). 1 MEDIUM + LOWs deferred (TD-017, TD-018). Race-clean under `go test -race`.

### 3.4 [x] **[Fan-out parallel + serial lanes - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.3 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.5 [x] **[Fallback chains + partial-success semantics - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-04 Fan-out Agent Execution](plan/acceptance-criteria/01-04-fanout-agent-execution.md)
   1. Analyze AC, identify testable units
   2. Write tests: primary failure → fallback agent tried (same persona), fallback_used/fallback_from recorded; fallback chain exhausted → agent failed; ≥1 success → exit 0 + partial:true; all fail → nonzero exit
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 3.6 [x] **[Fallback chains + partial-success semantics - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 3.7 [x] **[Fallback chains + partial-success semantics - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/fanout/outcome.go, internal/fanout/outcome_test.go (+ engine.go fallback path)
   Fresh subagent (description: `Adversarial review: 3.6`) reviewed the unit.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | outcome.go formatFailures | all-failed error embeds raw r.Err — could leak secrets | Non-issue (documented): llmclient uses header-only Bearer auth (no key in URL/query/error), registry rejects base_url userinfo, key-not-set error names only the env var — no credential reaches the string. Comment added. |
   | MEDIUM | outcome.go Outcome | zero-value/unknown Status folded into Failed | Fixed in 3.8: explicit switch counts only StatusFailed/StatusTimeout as failed |
   | MEDIUM | outcome.go formatFailures | did not filter OK rows (contract unenforced) | Fixed in 3.8: skips StatusOK rows independent of caller |
   | MEDIUM | engine.go/outcome.go | duplicate primary names indistinguishable in failure list | Non-issue: ProjectConfig.ValidateAgainst enforces unique, single-lane agent names |
   | LOW | outcome.go | empty Agent name renders " (reason)" | Fixed in 3.8: <unnamed> placeholder + test |
   | LOW | outcome.go | empty-roster vs all-failed not distinguishable by errors.Is | Fixed in 3.8: ErrEmptyRoster / ErrAllAgentsFailed sentinels |

   **Action Required:** No true CRITICAL/HIGH (the HIGH is prevented by existing llmclient/registry guarantees, documented inline). Correctness MEDIUMs + LOWs fixed in 3.8 with new tests (sentinel errors.Is, placeholder name, OK-row filter). Race-clean.

### 3.8 [x] **[Fallback chains + partial-success semantics - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.7 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.9 [x] **[Per-agent artifacts + merged pool findings - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-04 Fan-out Agent Execution](plan/acceptance-criteria/01-04-fanout-agent-execution.md)
   1. Analyze AC, identify testable units
   2. Write tests: sources/pool/raw/<agent>/{review.md, findings.txt, status.json} written per agent (dirs created at agent start); status.json always written with status ok|failed|timeout (+ truncated/files_dropped fields); engine appends REVIEWER column to persona 7-col output; merged sources/pool/findings.txt; summary.json stats; crash-safe incremental writes
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.10 [x] **[Per-agent artifacts + merged pool findings - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 3.11 [x] **[Per-agent artifacts + merged pool findings - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/fanout/artifacts.go, internal/fanout/artifacts_test.go, internal/fanout/status.go, internal/stream/parser.go (ParseModelOutput)
   Fresh subagent (description: `Adversarial review: 3.10`) reviewed the unit.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | artifacts.go agentDir | filepath.Base leaves ".."/"."/"" intact → escape/alias | Fixed in 3.12: agentDirName rejects ".",".." ,"" explicitly + test |
   | HIGH | artifacts.go WritePool | no dedup → distinct names sharing a base clobber silently | Fixed in 3.12: seen-set rejects duplicate agent dirs + test |
   | MEDIUM | status.go atomicWriteFile | artifacts landed 0600 (CreateTemp default), AC mandates 0644 | Fixed in 3.12: tmp.Chmod(0644) before rename + perm test |
   | MEDIUM | artifacts.go WritePool | pool write not transactional / no fsync durability | Deferred → TD-019 (documented; per-file atomic is the guarantee) |
   | LOW | parser.go ParseModelOutput | degenerate "HIGH\|" rows became empty findings | Fixed in 3.12: minimum SEVERITY\|FILE:LINE\|PROBLEM guard + test |
   | LOW | parser.go ParseModelOutput | lossy truncation of pipe-leaked columns | Fixed in 3.12: overflow folds back into EVIDENCE (no loss, forge-proof) |

   **Action Required:** 2 HIGH (traversal name, dir collision) + AC-mandated 0644 perms fixed in 3.12 with tests. Overflow-into-EVIDENCE both fixes the LOW and hardens REVIEWER-forge resistance. fsync durability deferred (TD-019). Race-clean.

### 3.12 [x] **[Per-agent artifacts + merged pool findings - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.11 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.13 [x] **[Review directory + manifest + ID + latest pointer - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-03 Review Directory Structure](plan/acceptance-criteria/01-03-review-directory-structure.md)
   1. Analyze AC, identify testable units
   2. Write tests: .atcr/reviews/<YYYY-MM-DD>_<branch-slug>/ layout (payload/, sources/, reconciled/); manifest.json fields (base/head SHAs, detection_mode, payload_modes, roster, timestamps); .atcr/latest is a text file with the review id; --id override; --id with path traversal rejected; collision → suffix; empty slug → fallback
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.14 [x] **[Review directory + manifest + ID + latest pointer - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 3.15 [x] **[Review directory + manifest + ID + latest pointer - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/fanout/reviewdir.go, internal/fanout/reviewdir_test.go
   Fresh subagent (description: `Adversarial review: 3.14`) reviewed the unit.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | reviewdir.go validateReviewID | `--id="."` accepted → Join collapses to the reviews root, breaking per-review isolation | Fixed in 3.16: positive allowlist regex `^[A-Za-z0-9][A-Za-z0-9._-]*$` rejects "."/".."/""/leading-dash |
   | HIGH | reviewdir.go ReviewID | branch-derived ids skipped validation; slugifyBranch can emit "."/".." | Fixed in 3.16: validate computed id in both paths + all-dots slug → "review" fallback |
   | MEDIUM | reviewdir.go validateReviewID | leading-dash ids (flag injection) accepted | Fixed in 3.16: regex requires alnum first char |
   | MEDIUM | reviewdir.go ReviewID | collision suffix appended once, no re-probe → same-second clobber | Fixed in 3.16: resolveCollision loops suffix + counter |
   | MEDIUM | reviewdir.go ScaffoldReviewDir | MkdirAll over a pre-existing file gives opaque error | Kept: error surfaced; AC 01-03 Error Scenario 1 message is generic by design |
   | LOW | reviewdir.go ReadLatest | empty .atcr/latest → ("",nil) resolves to reviews root | Fixed in 3.16: empty/invalid pointer is an error |

   **Action Required:** 2 HIGH + 2 MEDIUM + 1 LOW fixed in 3.16 via an allowlist-regex redesign + collision loop + ReadLatest guard, with new tests. One MEDIUM kept by design (generic mkdir error). Escape invariant confirmed against ../absolute/Windows-separator.

### 3.16 [x] **[Review directory + manifest + ID + latest pointer - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.15 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.17 [x] **[atcr review command (end-to-end wiring) - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-01 End-to-End Review](plan/acceptance-criteria/01-01-end-to-end-review.md)
   1. Analyze AC, identify testable units
   2. Write tests (integration, httptest mock provider): zero-arg `atcr review` on a feature branch resolves range, builds payloads, fans out, writes artifacts; explicit --base/--head path; exit codes; payload/ recorded per mode
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.18 [x] **[atcr review command (end-to-end wiring) - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 3.19 [x] **[atcr review command (end-to-end wiring) - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/fanout/review.go, internal/fanout/review_test.go, internal/fanout/engine.go (per-agent timeout), cmd/atcr/review.go, internal/gitrange/resolver.go (CurrentBranch)
   Fresh subagent (description: `Adversarial review: 3.18`) reviewed the orchestration + cmd wiring.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | cmd/review.go | range-resolution failure returned raw → exit 1, but AC 03-02 Error Scenario 2 says pipeline failure → exit 2 | Fixed in 3.20: usageError wrap with "review failed:" message (exit 2) |
   | MEDIUM | review.go buildPayloads | FileCount reported pre-truncation len(entries) | Fixed in 3.20: FileCount = len(kept) (what the reviewer saw) |
   | MEDIUM | review.go buildSlots | one agent's persona/render failure aborts whole roster | Kept fail-fast (config error, nothing to preserve) — documented asymmetry vs all-fail path |
   | LOW | review.go buildAgent/buildFallbackAgent | provider map miss → zero-value Provider (confusing invoke-time error) | Fixed in 3.20: explicit unknown-provider build error + test |
   | LOW | review.go RunReview | empty roster scaffolds dir + repoints latest before ErrEmptyRoster | Fixed in 3.20: short-circuit empty roster before scaffolding + test |

   **Cleared by reviewer:** API key invoke-time only/never logged, nil Temperature omitempty, payloads built once per mode, both timeout ctxs have defer cancel, WaitGroup drains, no shell exposure.

   **Action Required:** 1 HIGH (exit-code) + correctness MEDIUM (FileCount) + 2 LOW (provider guard, empty-roster) fixed in 3.20 with tests. 1 MEDIUM kept by design (fail-fast on config error, documented).

### 3.20 [x] **[atcr review command (end-to-end wiring) - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.19 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.21 [x] **[Source discovery + normalization - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-05 Reconciliation Pipeline](plan/acceptance-criteria/01-05-reconciliation-pipeline.md)
   1. Analyze AC, identify testable units
   2. Write tests: any child of sources/ containing findings.txt is discovered (open extension point); reconciled/ never an input; --sources allowlist filters immediate children (pool, host, ...); normalization pads short rows, skips comments/blanks
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 3.22 [x] **[Source discovery + normalization - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 3.23 [x] **[Source discovery + normalization - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/reconcile/discover.go, internal/reconcile/discover_test.go
   Fresh subagent (description: `Adversarial review: 3.22`) reviewed the unit.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | discover.go leaf read | symlinked findings.txt could read a file outside the review dir | Fixed in 3.24: d.Type().IsRegular() gate skips symlinks/FIFOs/devices + containment test |
   | HIGH | discover.go WalkDir cb | one unreadable subtree aborted discovery of all sources | Fixed in 3.24: warn + SkipDir/nil instead of propagating |
   | MEDIUM | discover.go ReadFile | transient read error was fatal (inconsistent with parse-error skip) | Fixed in 3.24: warn + continue |
   | MEDIUM | discover.go walk | FIFO/device findings.txt would block/error | Fixed in 3.24: same IsRegular gate |
   | MEDIUM | discover.go | top-level findings.txt file directly under sources/ ignored | Documented as by-design (only child dirs are sources) |
   | LOW | discover.go leaf check | O(n²) over dirs | Kept: realistic trees are tiny |
   | LOW | discover.go HasPrefix | sibling-prefix (pool vs pool2) — reviewer CONFIRMED not a bug | Comment added: trailing separator is load-bearing |

   **Action Required:** 2 HIGH + 2 MEDIUM fixed in 3.24 with a symlink-containment test. Remaining items documented/kept. HasPrefix sibling-prefix concern independently confirmed NOT present.

### 3.24 [x] **[Source discovery + normalization - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.23 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 3.25 [x] **[Clustering + Jaccard dedupe + ambiguous sidecar - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-05 Reconciliation Pipeline](plan/acceptance-criteria/01-05-reconciliation-pipeline.md)
   1. Analyze AC, identify testable units
   2. Write tests (fixture corpus): (FILE, LINE±3) clustering incl. delta-3 same cluster / delta-4 different; Jaccard token-set ≥0.7 → merge; gray zone 0.4–0.7 → ambiguous.json (always written, empty array when none; default unmerged); <0.4 → distinct; thresholds fixed in v1
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.26 [x] **[Clustering + Jaccard dedupe + ambiguous sidecar - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 2 hours

### 3.27 [x] **[Clustering + Jaccard dedupe + ambiguous sidecar - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/reconcile/cluster.go, internal/reconcile/dedupe.go (+ tests)
   Fresh subagent (description: `Adversarial review: 3.26`) reviewed the unit. **No CRITICAL/HIGH** — determinism independently confirmed sound (sorted file order, union-toward-smaller-root, i,j ambiguous order).

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | cluster.go | "exact ±3" comment understated single-linkage transitive spread | Fixed in 3.28: comment corrected; noted clustering only scopes comparison, merge gated by Jaccard |
   | MEDIUM | dedupe.go | O(n²) re-tokenized strings every pair | Fixed in 3.28: pre-tokenize each finding once |
   | MEDIUM | dedupe.go | AmbiguousCluster.Line arbitrary (pair may span ±3) | Documented: Line = lower-indexed finding's; per-finding lines in Findings |
   | LOW | dedupe.go | float threshold boundary at 0.7/0.4 undefended | Fixed in 3.28: integer cross-multiplication (inter*10 vs union*7/4) + boundary tests |
   | LOW | dedupe.go | empty==empty problems scored 0 not identical | Fixed in 3.28: both-empty → merge (1.0) + test |
   | LOW | cluster.go | negative lines co-mingled into file-level | Documented: parser never emits negatives (Line 0 for missing) |

   **Action Required:** No CRITICAL/HIGH. Determinism hardened (integer thresholds), perf improved (pre-tokenize), comments corrected, with boundary + empty-problem tests. Remaining items documented.

### 3.28 [x] **[Clustering + Jaccard dedupe + ambiguous sidecar - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.27 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.29 [x] **[Merge rules + confidence + disagreement + emit - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-05 Reconciliation Pipeline](plan/acceptance-criteria/01-05-reconciliation-pipeline.md)
   1. Analyze AC, identify testable units
   2. Write tests: REVIEWERS comma-joined deduplicated; SEVERITY = max with `disagreement: <lo> vs <hi>` preserved; PROBLEM/FIX = longest; CATEGORY = modal; EST_MINUTES = max; CONFIDENCE categorical (HIGH = 2+ distinct reviewers, MEDIUM = single, LOW = untrusted); emits findings.txt (9-col), findings.json, report.md, summary.json
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.30 [x] **[Merge rules + confidence + disagreement + emit - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 2 hours

### 3.31 [x] **[Merge rules + confidence + disagreement + emit - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/reconcile/merge.go, reconcile.go, emit.go (+ tests)
   Fresh subagent (description: `Adversarial review: 3.30`) reviewed the unit. Determinism independently confirmed (modalCategory tiebreak verified across 200k randomized runs; map JSON key-sorting; reviewer-comma/pipe/newline defended in stream writer before escaping).

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | emit.go esc | html.EscapeString leaves newlines → markdown structure injection in report.md (forged headings) | Fixed in 3.32: esc flattens CR/LF before escaping + injection test |
   | MEDIUM | merge.go Merge | empty group → group[0] panic (latent; unreachable via Reconcile) | Fixed in 3.32: empty-group guard + test |
   | MEDIUM | emit.go Emit | per-file atomic but not set-atomic → partial artifact set on render error | Fixed in 3.32: render-all-then-write |
   | LOW | merge.go modalCategory | empty-string category hijacks alpha tiebreak | Fixed in 3.32: sorted keys + non-empty preference + test |
   | LOW | reconcile.go/merge.go | unknown merged severity ranks 0 | Moot: extraction regex only admits CRITICAL/HIGH/MEDIUM/LOW |
   | LOW | emit.go grid | default confidence bucket labels unknown as LOW | Kept: confidence always HIGH/MEDIUM in flow |

   **Action Required:** 1 HIGH (markdown newline injection) + 2 MEDIUM (panic guard, set-atomic emit) + 1 LOW (modal tiebreak) fixed in 3.32 with tests. Remaining items moot/kept (validated severities, harmless default bucket).

### 3.32 [x] **[Merge rules + confidence + disagreement + emit - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.31 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.33 [x] **[atcr reconcile --fail-on + one-shot review --fail-on - RED](plan/user-stories/03-ci-integration.md)**
   **AC:** [03-01 Fail-on Severity Threshold](plan/acceptance-criteria/03-01-fail-on-severity-threshold.md), [03-02 CI One-Shot Mode](plan/acceptance-criteria/03-02-ci-one-shot-and-example.md)
   1. Analyze ACs, identify testable units
   2. Write tests: exit 1 when findings at/above SEVERITY threshold survive, 0 below, 2 on usage/config errors; threshold validated against enum before any I/O; one-shot `atcr review --fail-on` runs review + reconcile + gate in-process; exit-code mapping centralized in main()
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 3.34 [x] **[atcr reconcile --fail-on + one-shot review --fail-on - GREEN](plan/user-stories/03-ci-integration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 3.35 [x] **[atcr reconcile --fail-on + one-shot review --fail-on - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-ci-integration.md)**
   **Changed Files:** internal/reconcile/gate.go, cmd/atcr/reconcile.go, cmd/atcr/anchor.go, cmd/atcr/review.go (+ tests)
   Fresh subagent (description: `Adversarial review: 3.34`) reviewed the unit.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | reconcile.go gate | RunReconcile I/O failure mapped to exit 1 (gate code), inconsistent with one-shot's exit 2 | Fixed in 3.36: usageError wrap (exit 2) |
   | HIGH | anchor.go id branch | bare ".." id escaped .atcr/reviews/ (skipped validation) | Fixed in 3.36: fanout.ValidateReviewID on id branch + traversal-id test |
   | MEDIUM | anchor.go | ".." misclassified as id (no separator) | Fixed by same id-branch validation |
   | LOW | discover.go/gate.go | unreadable source silently dropped → gate could pass | Deferred → TD-020 (stderr-warns; v1 favors resilience) |
   | LOW | gate.go CountAtOrAbove | unknown severity rank 0; non-canonical threshold counts all | Non-issue: all call sites validate threshold first; parser only emits valid severities |

   **Note:** verbatim path-anchor branch (absolute/relative path) is intentionally permissive — the user may point at a review dir anywhere on their own machine.

   **Action Required:** 2 HIGH (exit-code consistency, traversal-id) + 1 MEDIUM fixed in 3.36 with tests. 1 LOW deferred (TD-020), 1 LOW non-issue.

### 3.36 [x] **[atcr reconcile --fail-on + one-shot review --fail-on - REFACTOR](plan/user-stories/03-ci-integration.md)**
   1. Fix CRITICAL/HIGH issues from 3.35 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 3.37 [x] **[Report renderers + atcr report - RED](plan/user-stories/01-cli-review-workflow.md)**
   **AC:** [01-06 Report Rendering](plan/acceptance-criteria/01-06-report-rendering.md)
   1. Analyze AC, identify testable units
   2. Write tests (golden files): md/json/checklist from the same findings.json; zero-findings message; markdown/HTML special chars escaped in md output; --output routing; invalid format error
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 3.38 [x] **[Report renderers + atcr report - GREEN](plan/user-stories/01-cli-review-workflow.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 3.39 [x] **[Report renderers + atcr report - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-cli-review-workflow.md)**
   **Changed Files:** internal/report/render.go, internal/report/render_test.go, cmd/atcr/report.go (+ cmd test)
   Fresh subagent (description: `Adversarial review: 3.38`) reviewed the unit.

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | render.go codeSpan | a backtick in a file path closes the code span → HTML/markdown injection (bypasses esc) | Fixed in 3.40: backtick/newline paths fall back to esc(); byte-identical preserved for normal paths + breakout test |
   | HIGH | report.go --output | os.WriteFile to user path (traversal/symlink/overwrite) | Deferred → TD-021 (intended CLI behavior, like shell redirection; user's own path) |
   | MEDIUM | report.go readReconciledFindings | empty/null findings.json silently rendered as "No findings" | Fixed in 3.40: empty file → parse error (exit 2) + test |
   | MEDIUM | render.go truncate | runes[:n-3] underflows/panics when n<3 | Fixed in 3.40: n<3 guard + test |
   | LOW | render.go grid | unknown severity dropped from grid but rendered with blank heading | Non-issue: pipeline only emits valid severities |
   | LOW | render_test.go | coverage gaps (backtick, boundary, n<3) | Fixed in 3.40: added all suggested adversarial tests |

   **Action Required:** 1 CRITICAL (backtick code-span injection) fixed in 3.40 with a breakout test, plus 2 MEDIUM (empty-file, truncate panic) with tests. HIGH --output deferred as intended behavior (TD-021). LOW grid non-issue (validated severities).

### 3.40 [x] **[Report renderers + atcr report - REFACTOR](plan/user-stories/01-cli-review-workflow.md)**
   1. Fix CRITICAL/HIGH issues from 3.39 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 3.41 [x] **Phase 3 DoD Validation**
   1. Run `go test ./...` - all passing
   2. Run `go vet ./...` - clean
   3. Run `golangci-lint run` - clean
   4. Verify end-to-end: `atcr review` → `atcr reconcile` → `atcr report` on a fixture repo with httptest provider
   5. Verify `--fail-on` exit codes (0/1/2) against fixture findings
   6. Update metadata.md with Phase 3 completion metrics
   **Duration:** 30 min

### 3.42 [x] **Phase 3 - GATE: Integration & Exit Review (subagent)**
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

   **Files changed during Phase 3:** internal/fanout/{engine,outcome,artifacts,reviewdir,review,status}.go, internal/reconcile/{discover,cluster,dedupe,merge,reconcile,emit,gate}.go, internal/report/render.go, internal/stream/parser.go (ParseModelOutput), internal/gitrange/resolver.go (CurrentBranch), cmd/atcr/{review,reconcile,report,anchor}.go

   **Gate findings (first run):** 1 HIGH — config-level `fail_on` resolved through precedence but never consumed (both commands read only the `--fail-on` flag, so a project's configured gate was silently ignored, contradicting AC 03-02). LOWs: dead `AsReconciled`/`ParseReconciled` helpers + one-way disagreement folding → TD-022.

   **Fix + re-run:** `resolveGateThreshold` now consumes the project config's `fail_on`. The re-run confirmed the fix correct but flagged a deeper HIGH (2-tier flag>project precedence bypassed the documented registry tier) + a MEDIUM (broken project config silently disabled the gate). **Second fix:** full flag > project > registry precedence with the embedded default excluded (opt-in preserved), enum-validation of the config value, loud exit-2 on a broken project config, best-effort on a broken registry; HOME-isolated tests added.

   **Re-run verdict: GATE: PASS** — fresh subagent confirmed the fix correct, opt-in preserved, enum-validated, one-shot review path correctly flag-only; reconcile/report/fanout contracts sound (deterministic ordering, atomic writes, escaped markdown, consistent on-disk schema, correct Partial threading); no remaining CRITICAL/HIGH. Verified: tests race-clean, vet+lint clean, coverage 84.4%.

   **Action Required:** None remaining — 2 HIGH + 1 MEDIUM fixed across two gate rounds with tests; LOWs → TD-022. Phase gate passed.
   **Duration:** 15-30 min

---

**Phase 3 Complete. Stop and await human review before proceeding to Phase 4.**

---

## Phase 4: Integration (Days 9-10)

**Focus:** MCP server, Skill definition, end-to-end orchestration validation.

### 4.1 [x] **[MCP stdio server + stderr discipline - RED](plan/user-stories/04-mcp-integration.md)**
   **AC:** [04-01 MCP Stdio Server](plan/acceptance-criteria/04-01-mcp-stdio-server.md)
   1. Analyze AC, identify testable units
   2. Write tests (InMemoryTransport): initialize handshake; slog writes to stderr only (stdout owned by protocol); stdin close → in-flight requests complete, clean exit 0; malformed JSON-RPC → protocol error response, no crash
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 4.2 [x] **[MCP stdio server + stderr discipline - GREEN](plan/user-stories/04-mcp-integration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 4.3 [x] **[MCP stdio server + stderr discipline - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-mcp-integration.md)**
   **Changed Files:** internal/mcp/{server.go, tools.go, handlers.go}, cmd/atcr/{serve.go, status.go}, internal/fanout/{review.go, status.go, reviewdir.go}, internal/reconcile/gate.go (holistic review of the whole MCP-server unit — covers 4.3, 4.7, 4.11, 4.15).

   Fresh subagent (description: `Adversarial review: MCP server unit`) reviewed the whole unit (SECURITY / EDGE CASES / ERROR HANDLING / PERFORMANCE).

   **Subagent findings table:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | handlers.go background goroutine | Fan-out goroutine had no `recover()`; a panic in ExecuteReview would crash the whole MCP server process | Fixed in 4.4: `defer recover()` logs the panic, server survives |
   | HIGH | serve.go / handlers.go | Detached fan-out goroutines untracked; client disconnect → process exit kills in-flight reviews mid-write (orphaned, stuck in_progress) | Fixed in 4.4: engine `sync.WaitGroup` + `mcp.Serve` bounded drain (5s) on shutdown |
   | HIGH | handlers.go handleReview | `ErrEmptyRange` not mapped → empty range scaffolds a no-op review, fires the pool at empty payloads, repoints latest | Fixed in 4.4: empty range returns "nothing to review" before PrepareReview (test: TestReviewHandler_EmptyRangeErrors) |
   | MEDIUM | status.go ReadReviewStatus | manifest + summary read non-atomically vs. the background writer | Deferred → TD-023 (eventually-consistent; atomic writes + Partial-from-summary make it non-torn) |
   | MEDIUM | gate.go AtOrAbove | exported helper trusts a canonical threshold; unknown threshold ranks 0 → fail-all footgun | Fixed in 4.4: unknown threshold returns false |
   | LOW | handlers.go / anchor.go | `readManifestPartial` duplicated in MCP + CLI | Fixed in 4.4: extracted `fanout.ReadManifestPartial`, both call it |

   **Action Required:** 1 CRITICAL + 3 HIGH fixed inline in 4.4 (panic recover, shutdown drain, empty-range guard); 2 cheap MEDIUM/LOW also fixed (AtOrAbove guard, manifest-reader dedup); 1 MEDIUM deferred → TD-023.

### 4.4 [x] **[MCP stdio server + stderr discipline - REFACTOR](plan/user-stories/04-mcp-integration.md)**
   1. Fix CRITICAL/HIGH issues from 4.3 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 4.5 [x] **[Tool registration + typed schemas - RED](plan/user-stories/04-mcp-integration.md)**
   **AC:** [04-02 Tool Registration and Schemas](plan/acceptance-criteria/04-02-tool-registration-schemas.md)
   1. Analyze AC, identify testable units
   2. Write tests: exactly 5 tools (atcr_review, atcr_reconcile, atcr_report, atcr_range, atcr_status); schemas inferred from typed args/result structs via generic mcp.AddTool; unknown/extra arg fields rejected; duplicate registration → error at startup (no panic)
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 30 min

### 4.6 [x] **[Tool registration + typed schemas - GREEN](plan/user-stories/04-mcp-integration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1 hour

### 4.7 [x] **[Tool registration + typed schemas - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-mcp-integration.md)**
   **Changed Files:** internal/mcp/{server.go, tools.go} (tool registration + typed schemas). Reviewed as part of the holistic MCP-server adversarial review in 4.3.

   **Action Required:** Covered by the 4.3 holistic review — tool registration (duplicate-name guard + panic-recover in `registerTool`) and the report format-enum schema were in scope. No CRITICAL/HIGH specific to registration. Adversarial review passed.

### 4.8 [x] **[Tool registration + typed schemas - REFACTOR](plan/user-stories/04-mcp-integration.md)**
   1. Fix CRITICAL/HIGH issues from 4.7 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 20 min

### 4.9 [x] **[atcr_review + atcr_reconcile handlers - RED](plan/user-stories/04-mcp-integration.md)**
   **AC:** [04-03 Review and Reconcile Handlers](plan/acceptance-criteria/04-03-review-reconcile-handlers.md)
   1. Analyze AC, identify testable units
   2. Write tests (InMemoryTransport): handlers are thin wrappers over the same engine as the CLI; atcr_review returns immediately with {review_id, review_path, status: "running", agent_count} while fan-out continues (completion polled via atcr_status); atcr_reconcile defaults to .atcr/latest, fail_on filters by SEVERITY; invalid fail_on → error before execution; path containment under .atcr/reviews/
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 4.10 [x] **[atcr_review + atcr_reconcile handlers - GREEN](plan/user-stories/04-mcp-integration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 4.11 [x] **[atcr_review + atcr_reconcile handlers - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-mcp-integration.md)**
   **Changed Files:** internal/mcp/handlers.go (handleReview/handleReconcile), internal/fanout/review.go (Prepare/Execute split). Reviewed as part of the holistic MCP-server adversarial review in 4.3.

   **Action Required:** Covered by the 4.3 holistic review — the review handler's background goroutine yielded the CRITICAL (panic recover) + two HIGH (shutdown drain, empty-range guard), all fixed in 4.4; reconcile's fail_on path and no-results error were in scope. Adversarial review passed after fixes.

### 4.12 [x] **[atcr_review + atcr_reconcile handlers - REFACTOR](plan/user-stories/04-mcp-integration.md)**
   1. Fix CRITICAL/HIGH issues from 4.11 (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** 30 min

### 4.13 [x] **[atcr_report / atcr_range / atcr_status handlers - RED](plan/user-stories/04-mcp-integration.md)**
   **AC:** [04-04 Report, Range, and Status Handlers](plan/acceptance-criteria/04-04-report-range-status-handlers.md)
   1. Analyze AC, identify testable units
   2. Write tests (InMemoryTransport): atcr_report renders md/json/checklist; invalid format rejected by schema enum (handler enum check as defense in depth); atcr_range returns Resolution JSON; atcr_status reads manifest (corrupt manifest → structured error); git ops via exec.Command argument arrays only
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** 45 min

### 4.14 [x] **[atcr_report / atcr_range / atcr_status handlers - GREEN](plan/user-stories/04-mcp-integration.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** 1.5 hours

### 4.15 [x] **[atcr_report / atcr_range / atcr_status handlers - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-mcp-integration.md)**
   **Changed Files:** internal/mcp/handlers.go (handleReport/handleRange/handleStatus + resolveReviewDir containment), internal/fanout/status.go (ReadReviewStatus), internal/reconcile/gate.go (AtOrAbove). Reviewed as part of the holistic MCP-server adversarial review in 4.3.

   **Action Required:** Covered by the 4.3 holistic review — path containment (resolveReviewDir), the empty-diff range result, corrupt-manifest structured error, and report format-enum defense were in scope; yielded the AtOrAbove footgun (fixed in 4.4) and the status-read race (deferred → TD-023). Adversarial review passed.

### 4.16 [x] **[atcr_report / atcr_range / atcr_status handlers - REFACTOR](plan/user-stories/04-mcp-integration.md)**
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
