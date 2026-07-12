# Sprint 20.1: Public TD Resolve Skill

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 20.1 step-by-step. Complete each step, check off work immediately. This sprint runs in **gated** mode — stop at each phase-boundary gate task (`N.LAST`) instead of running continuously.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What This Sprint Builds

A local, `.atcr/`-scoped technical-debt store and an autonomous `/atcr debt resolve` skill route that give standalone/public `atcr` users the review-and-fix loop the private `.planning/technical-debt/` + `/resolve-td` pipeline already provides. Reconciled findings persist across review runs into a durable JSONL backlog, and a new dispatcher route resolves them via a `.planning/`-free RED→GREEN→ADVERSARIAL→REFACTOR cycle.

### Why This Matters

Without this, "public atcr" is a review-only tool while "private atcr" reviews *and* fixes — undercutting the goal of shipping atcr as a complete, standalone product. This is the first public-side consumer of Epic 18.3's `justification`/back-reference finding enrichment.

### Key Deliverables

- New `internal/localdebt` append-only JSONL store package (`Record`, `Append`, `ReadRecords`, `ReadAll`), structurally modeled on `internal/scorecard`.
- `atcr reconcile` persistence hook that appends reconciled findings across runs, with write-time dedup by `FindingID` and a `--no-local-debt` opt-out.
- New `atcr debt resolve` CLI subcommand + `skill/debt-resolve/SKILL.md` documenting the four-stage resolution cycle.
- Shared `skill/CONVENTIONS.md` extracted from `skill/SKILL.md` (second-public-skill trigger, per Epic 20.0's addendum).
- New capability documented in `docs/skill-usage.md`.

### Success Criteria

- Local TD store format defined, documented, and covered by unit tests (AC1).
- Reconciled findings persist across multiple review runs with dedup, not just one review's directory (AC2).
- `skill/SKILL.md` dispatcher extended; `atcr debt resolve` autonomously resolves store items, consuming `justification`/`SourceReport` when present (AC3).
- Zero `.planning/` references leak into any new public-facing code path.
- `go test ./...` passes at ≥80% coverage; lint/vet clean.

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Default TDD Mode:** Moderate 🔄 (derived from complexity 9/12 — COMPLEX)

Each story runs a **Moderate** cycle: `RED` (write failing tests) → `GREEN` (minimal implementation) → `ADVERSARIAL` (fresh-subagent review) → `REFACTOR` (fix inline findings + clean up).

**Adversarial Review:** ENABLED 🎯 — every story's implementation is reviewed by a **fresh subagent** (no memory of the implementation) to avoid "I wrote it, it's good" bias.
- **Inline-fix (block until fixed):** `CRITICAL/HIGH`
- **Defer to tech debt:** `MEDIUM/LOW` → append to `clarifications/tech-debt-captured.md`

**Execution Mode:** Gated 🚧 — after each phase's DoD, a Phase-Boundary Gate (`N.LAST`) runs a fresh-subagent integration review. `/execute-sprint` stops at each phase boundary.

**E2E note:** Story 3's E2E ACs (03-04, 03-05) describe agent-driven Markdown behavior in `skill/debt-resolve/SKILL.md` that `go test` cannot exercise. `skill/skill_test.go` covers structural/content assertions only (stage names present, dispatcher documented); the actual RED→GREEN→ADVERSARIAL→REFACTOR walkthrough is validated in Phase 5 as a live fixture-repo scenario run — budget it as a distinct verification activity, not "tests pass."

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [user-stories/](plan/user-stories/) | Feature requirements |
| [acceptance-criteria/](plan/acceptance-criteria/) | Validation requirements with DoD |
| [documentation/](plan/documentation/) | Grounded package/pattern docs |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./internal/localdebt/ -run TestName` |
| T2: Module | After completing an element | `go test ./internal/localdebt/...` or `go test ./cmd/atcr/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

**PRIMARY_TEST_LOCATION:** Co-located `*_test.go` files alongside source (Go convention).

### DoD Verification Checklist
1. Tests (T3): `go test ./...` all passing
2. Coverage: ≥80% (`go test -coverprofile=coverage.out ./...`)
3. Lint: `golangci-lint run` clean; `go vet ./...` clean
4. Build: `go build ./...` succeeds
5. Docs: Updated (package doc comments, `docs/skill-usage.md` where applicable)

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

**Architecture (implementation-standards.md):** Black-box interfaces; replaceable components; primitive-first design. `internal/localdebt` is a structural sibling of `internal/scorecard`, importing `internal/history` only for `FindingID` — never its `.planning/`-scoped logic.

**Coding (coding-standards.md, Go):**
- Packages lowercase single-word; exported `PascalCase`, unexported `camelCase`; files snake_case.
- Errors returned last, never ignored, wrapped with `fmt.Errorf("action: %w", err)`. No `panic` for normal conditions.
- `defer` resource cleanup (file descriptors, readers). Return concrete types from constructors, accept interfaces.
- Table-driven tests; `testify/require`; test files co-located as `*_test.go`.
- Format with `goimports`; pass `golangci-lint run` and `go vet ./...`.

**Git (git-strategy.md):** GitHub Flow. Branch `feature/20.1_public_td_resolve_skill` from `main`. Small atomic Conventional Commits (`type(scope): description`). Squash-and-merge via PR; CI must pass.

---

## External Resources

From [plan/documentation/README.md](plan/documentation/README.md):
- **[CRITICAL]** [Agent Skills Format & Progressive Disclosure](plan/documentation/agent-skills-format.md) — SKILL.md frontmatter, Level 1/2/3 progressive disclosure for `skill/debt-resolve/SKILL.md` + `skill/CONVENTIONS.md`.
- **[CRITICAL]** [Append-Only JSONL Store Pattern](plan/documentation/append-only-store-pattern.md) — `internal/scorecard`/`internal/history` atomic-append precedents to copy.
- **[CRITICAL]** [Local TD Store Schema](plan/documentation/local-td-store-schema.md) — v1 record schema, file layout, identity/dedup rules, CLI contract (AC1).
- **[IMPORTANT]** [CLI Integration Points](plan/documentation/cli-integration-points.md) — `atcr reconcile` hook, `atcr debt` family, live `justification`/`SourceReport` fields, symbol-anchor contract.
- **[IMPORTANT]** [Skill Dispatcher & CONVENTIONS.md Extraction](plan/documentation/skill-dispatcher-conventions.md) — dispatcher extension + boilerplate extraction shape.

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation

**Goal:** Build the append-only `internal/localdebt` package that every other story depends on. Lock the store contract (schema, dedup key, concurrency guarantee, tolerant read) before anything downstream starts.
**Story:** [Story 1 — Local TD Store Persistence](plan/user-stories/01-local-td-store-persistence.md)

### 1.1 [ ] **[Local TD Store — RED](plan/user-stories/01-local-td-store-persistence.md)**
   **Mode:** Moderate | **ACs:** [01-01](plan/acceptance-criteria/01-01-package-structure-and-store-operations.md), [01-02](plan/acceptance-criteria/01-02-record-identity-via-findingid-reuse.md), [01-03](plan/acceptance-criteria/01-03-tolerant-read-path.md), [01-04](plan/acceptance-criteria/01-04-concurrency-guarantee-and-package-documentation.md)
   1. Analyze ACs, identify testable units for `internal/localdebt`.
   2. Write failing tests: `Append` byte-identical round-trip; `Record` identity via `history.FindingID` reuse; tolerant read (malformed line skipped w/ warning, forward-incompatible `schema_version` skipped, missing dir → `(nil, nil)`); `ReadAll` across month shards.
   3. Verify tests fail correctly (package/functions not yet implemented).
   **Files:** `internal/localdebt/store_test.go`, `internal/localdebt/record_test.go` | **Duration:** ~0.5d

### 1.2 [ ] **[Local TD Store — GREEN](plan/user-stories/01-local-td-store-persistence.md)**
   Minimal implementation to pass (T1 after each change), verify all pass (T2), COMMIT.
   - `Record` struct (required: `schema_version`, `id`, `run_id`, `ts`, `severity`, `file`, `line`, `problem`, `fix`, `category`, `est_minutes`, `evidence`, `reviewers`, `confidence`; optional: `justification`, `source_report.{path,line,section}`, `status`, `resolved_at`).
   - `Append(dir string, rec Record) error` — lazy `0700` dir / `0600` file, one `os.Write` per record, month-rotated `YYYY-MM.jsonl`.
   - `ReadRecords(path string, opts ReadOpts)` + `ReadAll(dir string, opts ReadOpts)` — `bufio` streaming, tolerant skip.
   - Identity/dedup key via `history.FindingID(file, line, problem)` (reused verbatim, not reimplemented).
   3. COMMIT: `git commit -m "feat(localdebt): implement append-only local TD store (green)"`
   **Files:** `internal/localdebt/store.go`, `internal/localdebt/record.go` | **Duration:** ~0.75d

### 1.2.A [ ] **[Local TD Store — ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-local-td-store-persistence.md)**
   **Changed Files:** `internal/localdebt/store.go`, `internal/localdebt/record.go`, `internal/localdebt/store_test.go`, `internal/localdebt/record_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the four files above
     - Checklist (pass verbatim):
       - SECURITY: File/dir permissions (`0700`/`0600`), path traversal outside `.atcr/debt/`, code-snippet exposure in world-readable files?
       - EDGE CASES: Empty/missing dir, malformed JSONL line, forward-incompatible `schema_version`, month boundary, concurrent append tear (TD-004)?
       - ERROR HANDLING: Swallowed errors, unclosed readers/files, partial-write on error?
       - PERFORMANCE: Full-file buffering vs. streaming; O(n) dedup scan documented?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 1.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.3 [ ] **[Local TD Store — REFACTOR](plan/user-stories/01-local-td-store-persistence.md)**
   1. Fix CRITICAL/HIGH issues from 1.2.A (if any).
   2. Improve code and tests (T1); ensure concurrency guarantee (one `Append` = one `os.Write`, TD-004 won't-fix referenced explicitly) is documented in package doc comments (AC 01-04).
   3. Validate all tests still pass (T3).
   4. COMMIT: `git commit -m "refactor(localdebt): address review + document concurrency guarantee"`
   **Duration:** ~0.25d

### 1.4 [ ] **Story 1 — Definition of Done**
   Run DoD verification checklist. Confirm ACs 01-01..01-04 satisfied. Emit DoD report.
   - [ ] T3 tests passing
   - [ ] Coverage ≥80% for `internal/localdebt`
   - [ ] Lint/vet clean
   - [ ] Package doc comments present (concurrency guarantee + differing-audience note vs. Epic 19.4's `.atcr/findings-history.jsonl`)

### 1.5 [ ] **Phase 1 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 1 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. No memory of the phase's implementation — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 1 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `Append`/`ReadRecords`/`ReadAll` signatures + `Record` shape stable for downstream stories?
       - CONFIG SURFACE: File layout (`.atcr/debt/YYYY-MM.jsonl`), permissions defaulted, back-compat via `schema_version`?
       - INTEGRATION: `internal/history` imported for `FindingID` only, no `.planning/` coupling?
       - PHASE-EXIT CONTRACT: Story 2 can call `Append`/`ReadAll` and Story 3 can `ReadAll` without rework?
       - REGRESSION: No change to `internal/scorecard`/`internal/history` behavior?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 1 complete. Stop here. Await go-ahead before Phase 2.

---

## Phase 2: Core Items

**Goal:** Wire `atcr reconcile` into the Phase 1 store (Story 2) and extract shared skill conventions (Story 4). Story 4 is independent of Stories 1-2 and runs in parallel so Story 3 later references a finished `CONVENTIONS.md`.
**Stories:** [Story 2 — Reconcile-Time Persistence Hook](plan/user-stories/02-reconcile-time-persistence-hook.md), [Story 4 — Shared Skill Conventions Extraction](plan/user-stories/04-shared-skill-conventions-extraction.md)

### 2.1 [ ] **[Reconcile Persistence Hook — RED](plan/user-stories/02-reconcile-time-persistence-hook.md)**
   **Mode:** Moderate | **ACs:** [02-01](plan/acceptance-criteria/02-01-persist-reconciled-findings.md), [02-02](plan/acceptance-criteria/02-02-no-local-debt-opt-out-flag.md), [02-03](plan/acceptance-criteria/02-03-cross-run-accumulation-and-dedup.md)
   1. Analyze ACs. Write failing integration tests against a shared temp `.atcr/debt/` (`t.TempDir()`, no mocking): `TestRunReconcile_LocalDebtAccumulatesAcrossRuns`, `TestRunReconcile_LocalDebtDedupsSameFinding`, `--no-local-debt` opt-out skips persistence, dedup scoped to full history (`ReadAll`), fail-open on dedup-read error.
   2. Verify tests fail correctly.
   **Files:** `cmd/atcr/reconcile_test.go` (extended) | **Duration:** ~0.5d

### 2.2 [ ] **[Reconcile Persistence Hook — GREEN](plan/user-stories/02-reconcile-time-persistence-hook.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT.
   - After the scorecard emit block in `runReconcile`, persist run findings (with `Justification`/`SourceReport` when present) via `localdebt.Append`.
   - Write-time dedup by `FindingID` against `ReadAll` (full history, not just current-month shard); fail-open (log to `cmd.ErrOrStderr()`, non-fatal) on dedup-read error.
   - `--no-local-debt` flag mirroring `--no-scorecard`'s shape and best-effort/non-fatal contract.
   3. COMMIT: `git commit -m "feat(reconcile): persist reconciled findings to local TD store (green)"`
   **Files:** `cmd/atcr/reconcile.go` | **Duration:** ~0.75d

### 2.2.A [ ] **[Reconcile Persistence Hook — ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-reconcile-time-persistence-hook.md)**
   **Changed Files:** `cmd/atcr/reconcile.go`, `cmd/atcr/reconcile_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 2.2 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the two files above
     - Checklist (pass verbatim):
       - SECURITY: Any `.planning/` leak; findings written outside `.atcr/debt/`?
       - EDGE CASES: Empty findings set, dedup across month boundary, same finding twice in one run, dedup-read error?
       - ERROR HANDLING: Persistence failure must NOT fail `runReconcile` exit code or roll back scorecard; best-effort/non-fatal honored?
       - PERFORMANCE: `ReadAll` full-history dedup scan under ~100ms at documented scale; streaming read?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.3 [ ] **[Reconcile Persistence Hook — REFACTOR](plan/user-stories/02-reconcile-time-persistence-hook.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any).
   2. Improve code and tests (T1); confirm `--no-local-debt` help text matches `--no-scorecard` conventions.
   3. Validate all tests pass (T3).
   4. COMMIT: `git commit -m "refactor(reconcile): address review + tidy persistence hook"`
   **Duration:** ~0.25d

### 2.4 [ ] **[Shared Skill Conventions — RED](plan/user-stories/04-shared-skill-conventions-extraction.md)**
   **Mode:** Moderate | **ACs:** [04-01](plan/acceptance-criteria/04-01-conventions-md-creation.md), [04-02](plan/acceptance-criteria/04-02-skill-md-prerequisites-pointer.md), [04-03](plan/acceptance-criteria/04-03-go-embed-and-test-coverage.md)
   1. Analyze ACs. Extend `skill/skill_test.go` with failing assertions: `ConventionsMD` embedded and non-empty; contains binary-on-PATH check, git-worktree check, `.atcr/` path-safety rules; no `.claude`/no-absolute-path (added to existing test list); `skill/SKILL.md` Prerequisites section reduced to a pointer with no coverage lost.
   2. Verify tests fail correctly.
   **Files:** `skill/skill_test.go` (extended) | **Duration:** ~0.25d

### 2.5 [ ] **[Shared Skill Conventions — GREEN](plan/user-stories/04-shared-skill-conventions-extraction.md)**
   Minimal code to pass (T1), verify (T2), COMMIT.
   - Create `skill/CONVENTIONS.md` (binary-on-PATH check, git-worktree check, `.atcr/` path-safety rules — moved verbatim from `skill/SKILL.md`).
   - Rewrite `skill/SKILL.md` Prerequisites section to a pointer at `CONVENTIONS.md`, no coverage lost.
   - Embed `ConventionsMD` in `skill/skill.go` (following the `host-review.md` embed pattern).
   3. COMMIT: `git commit -m "refactor(skill): extract shared conventions to CONVENTIONS.md (green)"`
   **Files:** `skill/CONVENTIONS.md`, `skill/SKILL.md`, `skill/skill.go` | **Duration:** ~0.25d

### 2.5.A [ ] **[Shared Skill Conventions — ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-shared-skill-conventions-extraction.md)**
   **Changed Files:** `skill/CONVENTIONS.md`, `skill/SKILL.md`, `skill/skill.go`, `skill/skill_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 2.5 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the four files above
     - Checklist (pass verbatim):
       - SECURITY: `.atcr/` path-safety rules preserved intact in the move; no absolute paths / `.claude` references introduced?
       - EDGE CASES: Any Prerequisites content dropped in extraction (coverage-lost)? SKILL.md still within its ~500-line budget?
       - ERROR HANDLING: `//go:embed` path correct, build not broken?
       - PERFORMANCE: N/A (static content)
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.6, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.6 [ ] **[Shared Skill Conventions — REFACTOR](plan/user-stories/04-shared-skill-conventions-extraction.md)**
   1. Fix CRITICAL/HIGH issues from 2.5.A (if any).
   2. Improve wording/consistency (T1); confirm both `skill/SKILL.md` and (later) `skill/debt-resolve/SKILL.md` can point at `CONVENTIONS.md`.
   3. Validate all tests pass (T3).
   4. COMMIT: `git commit -m "refactor(skill): address review + tidy CONVENTIONS pointer"`
   **Duration:** ~0.25d

### 2.7 [ ] **Phase 2 — Definition of Done**
   Run DoD verification checklist. Confirm ACs 02-01..02-03 and 04-01..04-03 satisfied. Emit DoD reports for Story 2 and Story 4.
   - [ ] T3 tests passing (incl. new reconcile integration tests + skill embed tests)
   - [ ] Coverage ≥80%
   - [ ] Lint/vet clean; `go build ./...` succeeds
   - [ ] `CONVENTIONS.md` complete; Story 3 has a finished file to reference

### 2.8 [ ] **Phase 2 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 2 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `--no-local-debt` flag + persistence hook stable; `ConventionsMD` embed stable?
       - CONFIG SURFACE: New flag documented/defaulted/back-compat; `CONVENTIONS.md` referenced by SKILL.md?
       - INTEGRATION: `runReconcile` uses `localdebt.Append`/`ReadAll` correctly; no scorecard regression?
       - PHASE-EXIT CONTRACT: Story 3 can consume the store AND reference a finished `CONVENTIONS.md` without rework?
       - REGRESSION: Existing `atcr reconcile` / `atcr debt` behavior intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 2 complete. Stop here. Await go-ahead before Phase 3.

---

## Phase 3: Advanced

**Goal:** The centerpiece — `atcr debt resolve` CLI subcommand + `skill/debt-resolve/SKILL.md` documenting the four-stage RED→GREEN→ADVERSARIAL→REFACTOR cycle adapted from `/resolve-td`. Gated on Phases 1-2 (needs a real store to read and a stable `CONVENTIONS.md`).
**Story:** [Story 3 — `/atcr debt resolve` Skill Route](plan/user-stories/03-atcr-debt-resolve-skill-route.md)

### 3.1 [ ] **[Debt Resolve Route — RED](plan/user-stories/03-atcr-debt-resolve-skill-route.md)**
   **Mode:** Moderate | **ACs:** [03-01](plan/acceptance-criteria/03-01-skill-md-dispatcher-documentation.md), [03-02](plan/acceptance-criteria/03-02-debt-resolve-cli-subcommand.md), [03-03](plan/acceptance-criteria/03-03-item-selection-and-justification-consumption.md), [03-06](plan/acceptance-criteria/03-06-go-embed-wiring-and-test-coverage.md) (compiled-testable subset)
   1. Analyze ACs. Write failing tests:
      - `cmd/atcr/debt_resolve_test.go` — flag parsing, empty-store → "no items"/exit 0, JSON/table output, reads via `localdebt.ReadAll`, never touches `.planning/`, discoverable via `atcr debt --help`.
      - Item selection consumes `justification`/`SourceReport` when present; deterministic selection rule; CLI-subcommand-only access.
      - `skill/skill_test.go` — asserts all four stage names (RED/GREEN/ADVERSARIAL/REFACTOR) present in embedded `debt-resolve/SKILL.md`; `skill/SKILL.md` `atcr debt` row documents the route (no invented subcommand names).
   2. Verify tests fail correctly.
   **Files:** `cmd/atcr/debt_resolve_test.go` (new), `skill/skill_test.go` (extended) | **Duration:** ~1d

### 3.2 [ ] **[Debt Resolve Route — GREEN](plan/user-stories/03-atcr-debt-resolve-skill-route.md)**
   Minimal code to pass (T1), verify (T2), COMMIT.
   - `atcr debt resolve` subcommand extending `newDebtCmd()` (alongside `list`/`add`/`dashboard`): read path via `localdebt.ReadAll`; deterministic item selection consuming `justification`/`SourceReport`; append-only mark-resolved write path; validated flag enums (`--severity`, etc.); store path rooted under `.atcr/debt/`.
   - `skill/debt-resolve/SKILL.md` — documents the four-stage cycle adapted from `/resolve-td` (incl. non-overridable `llm_support_diff_smell` hard verdict, symbol-anchor relocation for drifted findings, cumulative adversarial pass), branch-safety (`debt-resolve/<date>` only from default branch), `NEEDS_REVIEW` never marked resolved. Points at `skill/CONVENTIONS.md`.
   - Embed new file(s) in `skill/skill.go`; update `skill/SKILL.md` `atcr debt` dispatcher row.
   3. COMMIT: `git commit -m "feat(debt): add atcr debt resolve subcommand + debt-resolve skill (green)"`
   **Files:** `cmd/atcr/debt.go`, `cmd/atcr/debt_resolve.go` (new), `skill/debt-resolve/SKILL.md` (new), `skill/skill.go`, `skill/SKILL.md` | **Duration:** ~1.5d

### 3.2.A [ ] **[Debt Resolve Route — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-atcr-debt-resolve-skill-route.md)**
   **Changed Files:** `cmd/atcr/debt.go`, `cmd/atcr/debt_resolve.go`, `cmd/atcr/debt_resolve_test.go`, `skill/debt-resolve/SKILL.md`, `skill/skill.go`, `skill/SKILL.md`, `skill/skill_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 3.2 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the seven files above
     - Checklist (pass verbatim):
       - SECURITY: Ref-injection via finding text into branch name (must use fixed `debt-resolve/<date>` template only); `--severity`/path-flag traversal escaping `.atcr/debt/`; autonomous edits writing outside CWD-rooted repo; `problem`/`fix`/`justification` treated as data, never executed?
       - EDGE CASES: Empty store, drifted finding location (symbol-anchor relocation, `NEEDS_REVIEW` on ambiguous), `llm_support_diff_smell` unavailable (skip gate, proceed to checklist), existing `debt-resolve/*` branch, non-default branch (resolve in place)?
       - ERROR HANDLING: Mark-resolved write best-effort/non-fatal; `NEEDS_REVIEW` items never marked resolved; no `.planning/` reference?
       - PERFORMANCE: `ReadAll` streaming for up to several-thousand-record list under 1s?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 3.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.3 [ ] **[Debt Resolve Route — REFACTOR](plan/user-stories/03-atcr-debt-resolve-skill-route.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any).
   2. Improve code/skill wording (T1); verify `skill/debt-resolve/SKILL.md` grounds each stage against `/resolve-td` semantics and references the finished `CONVENTIONS.md`.
   3. Validate all tests pass (T3).
   4. COMMIT: `git commit -m "refactor(debt): address review + tighten debt-resolve cycle doc"`
   **Duration:** ~0.5d

### 3.4 [ ] **Story 3 — Definition of Done**
   Run DoD verification checklist. Confirm compiled-testable ACs (03-01, 03-02, 03-03, 03-06) satisfied. E2E ACs 03-04, 03-05 deferred to Phase 5 live scenario walkthrough (noted, not skipped).
   - [ ] T3 tests passing (CLI + embed/stage-name assertions)
   - [ ] Coverage ≥80%
   - [ ] Lint/vet clean; `go build ./...` succeeds
   - [ ] `atcr debt resolve --help` discoverable; zero `.planning/` references in new code paths

### 3.5 [ ] **Phase 3 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `atcr debt resolve` flags/output stable; embedded skill files stage names stable?
       - CONFIG SURFACE: New subcommand documented in `--help` and `skill/SKILL.md` dispatcher row; no invented subcommand names?
       - INTEGRATION: Reads store only via `localdebt.ReadAll`; references finished `CONVENTIONS.md`; branch-safety template correct?
       - PHASE-EXIT CONTRACT: Story 5 docs can describe the shipped behavior without drift?
       - REGRESSION: `list`/`add`/`dashboard` debt subcommands + reconcile hook intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 3 complete. Stop here. Await go-ahead before Phase 4.

---

## Phase 4: Integration & Documentation

**Goal:** Document the new capability in `docs/skill-usage.md` and run a cross-cutting dispatcher-table/consistency check now that Stories 1-4 have landed.
**Story:** [Story 5 — Document Debt-Resolve in skill-usage.md](plan/user-stories/05-document-debt-resolve-in-skill-usage.md)

### 4.1 [ ] **[Skill-Usage Docs — RED](plan/user-stories/05-document-debt-resolve-in-skill-usage.md)**
   **Mode:** Moderate | **ACs:** [05-01](plan/acceptance-criteria/05-01-debt-resolve-route-documentation.md), [05-02](plan/acceptance-criteria/05-02-local-td-store-storage-section.md), [05-03](plan/acceptance-criteria/05-03-public-private-debt-disambiguation.md)
   1. Analyze ACs. Write failing doc-presence assertions (mirroring `internal/scorecard`'s doc test pattern where applicable): `docs/skill-usage.md` contains the `/atcr debt resolve` route section (purpose, invocation, behavior); a local `.atcr/`-scoped Storage section (location, population, `--no-local-debt`) mirroring `docs/scorecard.md`; and the explicit public/local-vs-private-`.planning/` disambiguation cross-linked to `docs/technical-debt.md`.
   2. Verify assertions fail correctly.
   **Files:** doc-presence test (co-located as appropriate) | **Duration:** ~0.25d

### 4.2 [ ] **[Skill-Usage Docs — GREEN](plan/user-stories/05-document-debt-resolve-in-skill-usage.md)**
   Minimal edits to pass (T1), verify (T2), COMMIT.
   - Extend `docs/skill-usage.md`: `/atcr debt resolve` route (purpose/invocation/behavior); Storage/CLI-Usage/Privacy-Model sections mirroring `docs/scorecard.md`; explicit public/local vs. private `.planning/`-scoped `atcr debt` disambiguation, cross-linked to `docs/technical-debt.md`.
   3. COMMIT: `git commit -m "docs(skill-usage): document atcr debt resolve + local TD store (green)"`
   **Files:** `docs/skill-usage.md` | **Duration:** ~0.25d

### 4.2.A [ ] **[Skill-Usage Docs — ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-document-debt-resolve-in-skill-usage.md)**
   **Changed Files:** `docs/skill-usage.md`, doc-presence test file

   **Spawn a fresh subagent** via the Agent tool. No memory of 4.2 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the files above
     - Checklist (pass verbatim):
       - SECURITY: Docs describe `.atcr/`-scoped (public) behavior only, no `.planning/` conflation that could mislead a standalone user?
       - EDGE CASES: `--no-local-debt` documented; empty-store behavior described; disambiguation unambiguous?
       - ERROR HANDLING: Documented fallbacks (missing store, malformed line) match Phase 1-3 actual behavior?
       - PERFORMANCE: N/A (docs)
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 4.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.3 [ ] **[Skill-Usage Docs — REFACTOR + Consistency Check](plan/user-stories/05-document-debt-resolve-in-skill-usage.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A (if any).
   2. Cross-cutting consistency check: verify `skill/SKILL.md`'s `atcr debt` row, `skill/skill_test.go`'s structural assertions, and the `CONVENTIONS.md` references across both skill files are internally consistent now that Stories 1-4 have landed. Reconcile any drift.
   3. Validate all tests pass (T3).
   4. COMMIT: `git commit -m "docs(skill-usage): address review + dispatcher consistency check"`
   **Duration:** ~0.5d

### 4.4 [ ] **Phase 4 — Definition of Done**
   Run DoD verification checklist. Confirm ACs 05-01..05-03 satisfied and dispatcher/skill consistency verified.
   - [ ] T3 tests passing
   - [ ] Docs complete and cross-linked
   - [ ] Lint/vet clean
   - [ ] `skill/SKILL.md` ↔ `skill/debt-resolve/SKILL.md` ↔ `CONVENTIONS.md` consistent

### 4.5 [ ] **Phase 4 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Docs match shipped CLI/skill behavior exactly (no aspirational claims)?
       - CONFIG SURFACE: `--no-local-debt`, store location, privacy model all documented?
       - INTEGRATION: Cross-links to `docs/technical-debt.md` / `docs/scorecard.md` valid?
       - PHASE-EXIT CONTRACT: Nothing left for Phase 5 but validation?
       - REGRESSION: No broken links or stale references introduced?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md`
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 4 complete. Stop here. Await go-ahead before Phase 5.

---

## Final Phase: Validation (Phase 5)

**Goal:** Cumulative adversarial review across the full diff, full test + coverage run, quality gates, DoD per AC, and confirmation that zero `.planning/` references leaked into any new public-facing code path. Includes the Story 3 E2E scenario walkthrough deferred from Phase 3.

### 5.1 [ ] **Cumulative Adversarial Review (subagent, full diff)**
   Per `/resolve-td`'s Phase 2 Step 6 precedent this plan grounds against. **Spawn a fresh subagent** to review the entire sprint diff (all phases) as a whole — catches cross-story integration issues single-story reviews miss.
   - subagent_type: `general-purpose`
   - description: `Cumulative adversarial review: sprint 20.1`
   - prompt: full-diff file list + the same SECURITY/EDGE/ERROR/PERFORMANCE checklist, plus: "Does any new public code path reference `.planning/`? Do the four resolution stages match `/resolve-td` semantics?"
   - CRITICAL/HIGH → fix now; MEDIUM/LOW → `tech-debt-captured.md`.

### 5.2 [ ] **Story 3 E2E Scenario Walkthrough (live, not `go test`)**
   Agent-driven walkthrough against a fixture repo with a seeded `.atcr/debt/` record and a deliberately reproducible bug (ACs 03-04, 03-05). Exercise the full RED→GREEN→ADVERSARIAL→REFACTOR cycle via `skill/debt-resolve/SKILL.md`:
   - [ ] Item selected; `justification`/`SourceReport` consumed when present
   - [ ] `debt-resolve/<date>` branch created only when starting from default branch; resolves in place on a non-default branch
   - [ ] `llm_support_diff_smell` hard verdict non-overridable; symbol-anchor relocation on drifted finding; `NEEDS_REVIEW` never marked resolved
   - [ ] Resolution outcome persisted via append-only mark-resolved record

### 5.3 [ ] **Validation Checklist**
   - [ ] All tests passing (T3): `go test ./...`
   - [ ] Coverage meets threshold: `go test -coverprofile=coverage.out ./...` ≥80%
   - [ ] Lint/format clean: `golangci-lint run`, `goimports`, `go vet ./...`
   - [ ] Build succeeds: `go build ./...`
   - [ ] Zero `.planning/` references in `internal/localdebt`, `cmd/atcr/debt_resolve.go`, `skill/debt-resolve/SKILL.md`

### 5.4 [ ] **Optional: Targeted Mutation Testing**
   MUTATION_TOOL = **UNAVAILABLE** (no Go mutation tool detected: no `stryker-mutator`, `mutmut`, or `cargo-mutants`). Skip — no mutation run available for this Go toolchain.
   **WARNING:** Do NOT run full-codebase mutation even if a tool is later added — it can take hours. Target only changed high-risk files (`internal/localdebt/*`).

### 5.5 [ ] **Drift Analysis**
   Compare the delivered sprint against [plan/original-requirements.md](plan/original-requirements.md):
   - [ ] AC1 — local TD store format defined + documented (`.atcr/`-scoped, no `.planning/`)
   - [ ] AC2 — reconciled findings persist across multiple runs with dedup
   - [ ] AC3 — `skill/SKILL.md` extended; `atcr debt resolve` autonomously resolves, consuming `justification`/back-reference
   - [ ] AC4 — capability documented in `docs/skill-usage.md`
   - [ ] AC5 (refinement) — shared boilerplate extracted to `skill/CONVENTIONS.md`; both SKILL.md files point at it
   - [ ] No scope added beyond the original request; Out-of-Scope items (private pipeline, packaging, multi-repo aggregation) untouched

**🚧 GATED STOP:** Sprint 20.1 complete. Await go-ahead before `/execute-code-review` / `/finalize-sprint`.
