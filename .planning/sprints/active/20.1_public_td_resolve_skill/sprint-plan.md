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

## Clarifications

### Phase 3 Clarifications (recorded 2026-07-12)

No blocking questions surfaced during the Phase 3 safety check — all decisions were resolvable from the acceptance criteria and existing code. Key decisions recorded for traceability:

**Key Decisions:**
- The Go `atcr debt resolve` subcommand is a thin store surface (list candidates + append-only mark-resolved write). The actual RED→GREEN→ADVERSARIAL→REFACTOR code-fixing cycle is agent-driven Markdown in `skill/debt-resolve/SKILL.md`, not compiled Go (grounded in AC 03-02 Sc.3 and AC 03-03: "agent-executed, not compiled Go").
- "Open" item selection = `status != resolved/deferred` (empty status counts as open), because the reconcile hook (`cmd/atcr/reconcile.go:181-196`) persists records with no `status` field set.
- Default selection rule stated explicitly in the skill: open items, sort `severity` DESC (HIGH>MEDIUM>LOW) then `ts` ASC (oldest first), cap `N=10` (mirrors `/resolve-td --max=10`).

**Scope Boundaries:**
- Compiled-testable ACs for Phase 3: 03-01, 03-02, 03-03, 03-06. E2E ACs 03-04/03-05 (full cycle walkthrough, branch-safety) deferred to Phase 5 live scenario run per the sprint plan.
- Mark-resolved write path (task 3.2 GREEN bullet) shipped minimal + unit-tested this phase; its E2E validation (03-05) is Phase 5.

**Technical Approach:**
- Store access via `localdebt.ReadAll(localdebt.DefaultDir("."), ...)`, rooted at `.atcr/debt/`; zero `.planning/` references; never invokes `defaultTDReadme`/`defaultTDItems`.
- New subcommand file `cmd/atcr/debt_resolve.go` (one-file-per-subcommand convention); registered in `newDebtCmd()`. `--severity` validated against `CRITICAL|HIGH|MEDIUM|LOW`; `--json` for machine output; `--list` default preview.
- `skill/skill.go` gains `DebtResolveMD` via `//go:embed debt-resolve/SKILL.md`, added to `TestSkill_NoAbsoluteOrClaudePaths`' slice; `skill/SKILL.md`'s `atcr debt` row (line 77) extended — no new dispatcher row.

### Phase 4 Clarifications (recorded 2026-07-12)

No blocking questions surfaced during the Phase 4 safety check — Story 5 is documentation-only and all three ACs (05-01/05-02/05-03) are fully specified. Decisions recorded for traceability:

**Key Decisions:**
- Phase 4 edits `docs/skill-usage.md` additively (new `## Technical Debt Resolution` section after the existing `## Output`), mirroring `docs/scorecard.md`'s Storage/CLI-Usage/Privacy shape. No existing content removed.
- The RED artifact is a doc-presence test mirroring `internal/scorecard/docs_test.go` (substring/section-presence assertions), NOT a behavioral test. `docs/` is not a Go package, so the test co-locates in `skill/docs_test.go` (skill/ owns skill-usage.md's subject; reuses a `repoRoot(t)` walk-up helper).
- Documented facts are verified against landed source before sign-off, not story drafts: store path `.atcr/debt/`, shard `YYYY-MM.jsonl`, perms `0700`/`0600`, `--no-local-debt` flag + single-run suppression, selection rule (severity DESC, ts ASC, N=10), empty-store → exit 0 no-op, write-time dedup by `FindingID` over full-history `ReadAll`.

**Scope Boundaries:**
- IN scope: `docs/skill-usage.md` edits + one doc-presence test file + task 4.3 cross-cutting dispatcher/skill consistency check. TD-003 (sibling `debt` subcommands span two backlogs) surfaced here per the Phase 2 gate note.
- NOT in scope: `docs/technical-debt.md` edits (read-only cross-link target); any product-code change; Phase 5 validation (gated stop before it).

**Technical Approach:**
- Disambiguation callout (AC 05-03) is a visually distinct block near the top of the new section, contrasting `.atcr/debt/` (public/standalone) vs `.planning/technical-debt/` (private pipeline), sharing the `atcr debt` verb but separate non-overlapping stores, cross-linked `[technical-debt.md](technical-debt.md)` via the doc's existing relative-link convention.

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation

**Goal:** Build the append-only `internal/localdebt` package that every other story depends on. Lock the store contract (schema, dedup key, concurrency guarantee, tolerant read) before anything downstream starts.
**Story:** [Story 1 — Local TD Store Persistence](plan/user-stories/01-local-td-store-persistence.md)

### 1.1 [x] **[Local TD Store — RED](plan/user-stories/01-local-td-store-persistence.md)**
   **Mode:** Moderate | **ACs:** [01-01](plan/acceptance-criteria/01-01-package-structure-and-store-operations.md), [01-02](plan/acceptance-criteria/01-02-record-identity-via-findingid-reuse.md), [01-03](plan/acceptance-criteria/01-03-tolerant-read-path.md), [01-04](plan/acceptance-criteria/01-04-concurrency-guarantee-and-package-documentation.md)
   1. Analyze ACs, identify testable units for `internal/localdebt`.
   2. Write failing tests: `Append` byte-identical round-trip; `Record` identity via `history.FindingID` reuse; tolerant read (malformed line skipped w/ warning, forward-incompatible `schema_version` skipped, missing dir → `(nil, nil)`); `ReadAll` across month shards.
   3. Verify tests fail correctly (package/functions not yet implemented).
   **Files:** `internal/localdebt/store_test.go`, `internal/localdebt/record_test.go` | **Duration:** ~0.5d

### 1.2 [x] **[Local TD Store — GREEN](plan/user-stories/01-local-td-store-persistence.md)**
   Minimal implementation to pass (T1 after each change), verify all pass (T2), COMMIT.
   - `Record` struct (required: `schema_version`, `id`, `run_id`, `ts`, `severity`, `file`, `line`, `problem`, `fix`, `category`, `est_minutes`, `evidence`, `reviewers`, `confidence`; optional: `justification`, `source_report.{path,line,section}`, `status`, `resolved_at`).
   - `Append(dir string, rec Record) error` — lazy `0700` dir / `0600` file, one `os.Write` per record, month-rotated `YYYY-MM.jsonl`.
   - `ReadRecords(path string, opts ReadOpts)` + `ReadAll(dir string, opts ReadOpts)` — `bufio` streaming, tolerant skip.
   - Identity/dedup key via `history.FindingID(file, line, problem)` (reused verbatim, not reimplemented).
   3. COMMIT: `git commit -m "feat(localdebt): implement append-only local TD store (green)"`
   **Files:** `internal/localdebt/store.go`, `internal/localdebt/record.go` | **Duration:** ~0.75d

### 1.2.A [x] **[Local TD Store — ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-local-td-store-persistence.md)**
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

   **Subagent findings (fresh-context general-purpose review, ran under -race):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | store.go read-path returns (open/read/ReadDir) | Read-path error returns not passed through `basePathErr`, unlike the write path; an absolute `dir` could leak `/Users/<name>/…` in a `*os.PathError`. `ReadOpts` doc also dropped the scorecard `SECURITY:` caveat. | Wrap read-path errors with `basePathErr` (os.IsNotExist still works on the clone); restore the SECURITY note on `ReadOpts.Writer`. |

   No CRITICAL/HIGH/MEDIUM. Verified correct: no path traversal (`monthRe` blocks `../`), no fd leaks, one-write-per-record (no hidden buffering), single-pass streaming read, `StampID` reuses `history.FindingID` verbatim, zero `.planning/` coupling.

   **Action taken:** LOW is a trivial precedent-mirroring security-hardening fix → addressed inline in 1.3 REFACTOR (not deferred), since REFACTOR immediately follows and its purpose is code improvement.

### 1.3 [x] **[Local TD Store — REFACTOR](plan/user-stories/01-local-td-store-persistence.md)**
   1. Fix CRITICAL/HIGH issues from 1.2.A (if any).
   2. Improve code and tests (T1); ensure concurrency guarantee (one `Append` = one `os.Write`, TD-004 won't-fix referenced explicitly) is documented in package doc comments (AC 01-04).
   3. Validate all tests still pass (T3).
   4. COMMIT: `git commit -m "refactor(localdebt): address review + document concurrency guarantee"`
   **Duration:** ~0.25d

### 1.4 [x] **Story 1 — Definition of Done**
   Run DoD verification checklist. Confirm ACs 01-01..01-04 satisfied. Emit DoD report.
   - [x] T3 tests passing (`go test ./...` green)
   - [x] Coverage ≥80% for `internal/localdebt` (84.1%)
   - [x] Lint/vet clean (`golangci-lint` 0 issues; `go vet` clean; `gofmt` clean)
   - [x] Package doc comments present (concurrency guarantee + differing-audience note vs. Epic 19.4's `.atcr/findings-history.jsonl`)

   **Story-1 DoD Complete** — Auto: 5/5 | Story-Specific ACs 01-01..01-04: satisfied | Manual Review: [ ] (deferred to /execute-code-review)

### 1.5 [x] **Phase 1 — GATE: Integration & Exit Review (subagent)**
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

   **Gate findings (fresh-context hostile-integrator review):** _(none — empty table)_
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|

   **Phase gate passed.** Verified: `Append`/`ReadRecords`/`ReadAll` signatures + `Record` shape (incl. `SourceReport`, optional fields, `StampID`) stable and sufficient for Story 2 (Append + ReadAll dedup) and Story 3 (ReadAll backlog); `DefaultDir(root)` → `<root>/.atcr/debt` matches reconcile.go's `Root: "."`; permissions 0700/0600; `schema_version` forward-incompatible-skip in place; `history` imported for `FindingID` only (allowlist `"localdebt": {"history"}` minimal, no cycle); zero `internal/scorecard`/`internal/history` regression. Dedup helper intentionally deferred to the Story 2 hook per doc.go contract — `ReadAll` + `Status`/`ResolvedAt` fields already present, so no rework needed.

**🚧 GATED STOP:** Phase 1 complete. Stop here. Await go-ahead before Phase 2.

---

## Phase 2: Core Items

**Goal:** Wire `atcr reconcile` into the Phase 1 store (Story 2) and extract shared skill conventions (Story 4). Story 4 is independent of Stories 1-2 and runs in parallel so Story 3 later references a finished `CONVENTIONS.md`.
**Stories:** [Story 2 — Reconcile-Time Persistence Hook](plan/user-stories/02-reconcile-time-persistence-hook.md), [Story 4 — Shared Skill Conventions Extraction](plan/user-stories/04-shared-skill-conventions-extraction.md)

### 2.1 [x] **[Reconcile Persistence Hook — RED](plan/user-stories/02-reconcile-time-persistence-hook.md)**
   **Mode:** Moderate | **ACs:** [02-01](plan/acceptance-criteria/02-01-persist-reconciled-findings.md), [02-02](plan/acceptance-criteria/02-02-no-local-debt-opt-out-flag.md), [02-03](plan/acceptance-criteria/02-03-cross-run-accumulation-and-dedup.md)
   1. Analyze ACs. Write failing integration tests against a shared temp `.atcr/debt/` (`t.TempDir()`, no mocking): `TestRunReconcile_LocalDebtAccumulatesAcrossRuns`, `TestRunReconcile_LocalDebtDedupsSameFinding`, `--no-local-debt` opt-out skips persistence, dedup scoped to full history (`ReadAll`), fail-open on dedup-read error.
   2. Verify tests fail correctly.
   **Files:** `cmd/atcr/reconcile_test.go` (extended) | **Duration:** ~0.5d

### 2.2 [x] **[Reconcile Persistence Hook — GREEN](plan/user-stories/02-reconcile-time-persistence-hook.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT.
   - After the scorecard emit block in `runReconcile`, persist run findings (with `Justification`/`SourceReport` when present) via `localdebt.Append`.
   - Write-time dedup by `FindingID` against `ReadAll` (full history, not just current-month shard); fail-open (log to `cmd.ErrOrStderr()`, non-fatal) on dedup-read error.
   - `--no-local-debt` flag mirroring `--no-scorecard`'s shape and best-effort/non-fatal contract.
   3. COMMIT: `git commit -m "feat(reconcile): persist reconciled findings to local TD store (green)"`
   **Files:** `cmd/atcr/reconcile.go` | **Duration:** ~0.75d

### 2.2.A [x] **[Reconcile Persistence Hook — ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-reconcile-time-persistence-hook.md)**
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

   **Subagent findings (fresh-context general-purpose hostile review):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | cmd/atcr/reconcile.go:204-207 (via record.go:61-63) | Dedup id = `FindingID(file,line,problem)` excludes severity, and the append-only store skips an already-seen id. A finding first persisted at a low severity that later re-reconciles at a higher severity is dropped by dedup — the backlog freezes first-seen severity. Documented-as-intended (stable id across re-settled severity), surfaced for explicit confirmation. | Either compare severity on a matched id and append/annotate on escalation, or document that the backlog intentionally keeps first-seen severity. |

   No CRITICAL/HIGH/MEDIUM. Verified correct: no `.planning/` leak; writes confined to `DefaultDir(".")` = `./.atcr/debt` with `monthRe`-guarded shard (no traversal); errors path-scrubbed by `basePathErr`; empty-findings early return (no dir created); in-run + cross-month dedup via full-history `ReadAll`; dedup-read failure fails open to at-least-once append; `persistLocalDebt` is void/non-fatal, runs after scorecard emit and before `gateFindings` so it cannot change exit code or roll back scorecard; streaming `bufio` read.

   **Action taken:** Single LOW (a request to confirm an already-documented design decision) → deferred to `tech-debt-captured.md` as TD-002. No CRITICAL/HIGH to fix in 2.3.

### 2.3 [x] **[Reconcile Persistence Hook — REFACTOR](plan/user-stories/02-reconcile-time-persistence-hook.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any).
   2. Improve code and tests (T1); confirm `--no-local-debt` help text matches `--no-scorecard` conventions.
   3. Validate all tests pass (T3).
   4. COMMIT: `git commit -m "refactor(reconcile): address review + tidy persistence hook"`
   **Duration:** ~0.25d

   **Outcome:** No CRITICAL/HIGH from 2.2.A (single LOW deferred to TD-001). `--no-local-debt` help text ("skip writing reconciled findings to the local TD store") already mirrors `--no-scorecard`'s register. Implementation reviewed — clean, no refactor changes warranted. T3 `go test ./...` green. No empty commit created (nothing changed).

### 2.4 [x] **[Shared Skill Conventions — RED](plan/user-stories/04-shared-skill-conventions-extraction.md)**
   **Mode:** Moderate | **ACs:** [04-01](plan/acceptance-criteria/04-01-conventions-md-creation.md), [04-02](plan/acceptance-criteria/04-02-skill-md-prerequisites-pointer.md), [04-03](plan/acceptance-criteria/04-03-go-embed-and-test-coverage.md)
   1. Analyze ACs. Extend `skill/skill_test.go` with failing assertions: `ConventionsMD` embedded and non-empty; contains binary-on-PATH check, git-worktree check, `.atcr/` path-safety rules; no `.claude`/no-absolute-path (added to existing test list); `skill/SKILL.md` Prerequisites section reduced to a pointer with no coverage lost.
   2. Verify tests fail correctly.
   **Files:** `skill/skill_test.go` (extended) | **Duration:** ~0.25d

### 2.5 [x] **[Shared Skill Conventions — GREEN](plan/user-stories/04-shared-skill-conventions-extraction.md)**
   Minimal code to pass (T1), verify (T2), COMMIT.
   - Create `skill/CONVENTIONS.md` (binary-on-PATH check, git-worktree check, `.atcr/` path-safety rules — moved verbatim from `skill/SKILL.md`).
   - Rewrite `skill/SKILL.md` Prerequisites section to a pointer at `CONVENTIONS.md`, no coverage lost.
   - Embed `ConventionsMD` in `skill/skill.go` (following the `host-review.md` embed pattern).
   3. COMMIT: `git commit -m "refactor(skill): extract shared conventions to CONVENTIONS.md (green)"`
   **Files:** `skill/CONVENTIONS.md`, `skill/SKILL.md`, `skill/skill.go` | **Duration:** ~0.25d

### 2.5.A [x] **[Shared Skill Conventions — ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-shared-skill-conventions-extraction.md)**
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

   **Subagent findings (fresh-context general-purpose hostile review):** _(none — empty table)_
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|

   **Adversarial review passed.** Reviewer diffed the pre-extraction SKILL.md (commit `b08e9991`) against `CONVENTIONS.md` and confirmed all three Prerequisites checks moved byte-identically (binary-on-PATH halt, git-worktree halt, `gh` CLI PR-resolution note incl. "authenticated" + `--base`/`--head` fallback). `.atcr/` path-safety section: no `.claude`/absolute paths, cited paths match real code (`localdebt/paths.go` `debtSubdir=".atcr/debt"`), correctly forbids `.planning/`. `//go:embed` resolves, build+tests pass, SKILL.md 98 lines (well under 500-line budget). No CRITICAL/HIGH/MEDIUM/LOW.

### 2.6 [x] **[Shared Skill Conventions — REFACTOR](plan/user-stories/04-shared-skill-conventions-extraction.md)**
   1. Fix CRITICAL/HIGH issues from 2.5.A (if any).
   2. Improve wording/consistency (T1); confirm both `skill/SKILL.md` and (later) `skill/debt-resolve/SKILL.md` can point at `CONVENTIONS.md`.
   3. Validate all tests pass (T3).
   4. COMMIT: `git commit -m "refactor(skill): address review + tidy CONVENTIONS pointer"`
   **Duration:** ~0.25d

   **Outcome:** No CRITICAL/HIGH from 2.5.A (adversarial review passed clean). `CONVENTIONS.md` is a self-contained `skill/` sibling explicitly designed for multiple `SKILL.md` files to point at ("Each skill's `SKILL.md` points here…"), so Story 3's `skill/debt-resolve/SKILL.md` can reference it without rework. Wording already consistent — no refactor changes warranted. T3 `go test ./...` green. No empty commit created.

### 2.7 [x] **Phase 2 — Definition of Done**
   Run DoD verification checklist. Confirm ACs 02-01..02-03 and 04-01..04-03 satisfied. Emit DoD reports for Story 2 and Story 4.
   - [x] T3 tests passing (incl. new reconcile integration tests + skill embed tests) — `go test ./...` green
   - [x] Coverage ≥80% — cmd/atcr 84.6%, internal/localdebt 84.1% (skill is Markdown/string-only, no statements)
   - [x] Lint/vet clean; `go build ./...` succeeds — golangci-lint 0 issues, `go vet` clean, `gofmt` clean
   - [x] `CONVENTIONS.md` complete; Story 3 has a finished file to reference

   **Story-2 DoD Complete** — Auto: 3/3 (tests/lint/build) | Story-Specific ACs 02-01..02-03: satisfied (persist-per-finding after scorecard emit; Justification/SourceReport carried when present & omitted when absent; zero-finding no-op; --no-local-debt suppression independent of --no-scorecard; cross-run accumulation; write-time dedup by FindingID over full-history ReadAll; fail-open + in-run-dedup implemented and adversarially verified) | Manual Review: [ ] (deferred to /execute-code-review). Note: fail-open-on-dedup-read-error and in-run duplicate-id collapse are covered by implementation + fresh-subagent review rather than dedicated CLI integration tests (both are defensive paths not reachable through the normal reconcile pipeline, which dedups upstream).

   **Story-4 DoD Complete** — Auto: 3/3 | Story-Specific ACs 04-01..04-03: satisfied (CONVENTIONS.md with three relocated checks byte-identical + new .atcr/ path-safety section; SKILL.md Prerequisites reduced to a pointer, no duplicated halt text; ConventionsMD embedded, non-empty, added to no-.claude/no-abs-path list; dispatcherCommands untouched) | Manual Review: [ ] (deferred to /execute-code-review).

### 2.8 [x] **Phase 2 — GATE: Integration & Exit Review (subagent)**
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

   **Gate findings (fresh-context hostile-integrator review):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | internal/mcp/handlers.go:344 | Local-debt persistence is CLI-only; the MCP `atcr_reconcile` handler emits scorecard via the shared bridge but never calls `persistLocalDebt`, so MCP-driven reconciles write zero `.atcr/debt/` records. | Deferred → TD-002. Out of Story 2's CLI-scoped surface (AC 02-01 scopes `runReconcile`); MCP parity is a follow-up decision. |
   | LOW | cmd/atcr/debt.go:18 | Within one `atcr debt` group, `add`/`dashboard` read `.planning/technical-debt/` while the reconcile hook + Story 3 `debt resolve` use `.atcr/debt/` — sibling subcommands span two backlogs. | Deferred → TD-003. By-design (`localdebt/doc.go`); surface in `debt --help` + docs (fold into Story 5 AC 05-03). |

   No CRITICAL/HIGH. Verified stable for downstream: `Append`/`ReadRecords`/`ReadAll`/`Record`/`StampID`/`DefaultDir` exported and stable; `ConventionsMD` embedded + file present + referenced by SKILL.md; `--no-local-debt` registered/defaulted/help-listed; no scorecard regression; build/vet/tests green. Story 3 can consume the store and reference the finished `CONVENTIONS.md` without rework.

   **Phase gate passed.** Both findings are MEDIUM/LOW, out of Story 2's scoped surface or by-design → captured as TD-002/TD-003 for explicit decision; no blocking fixes required before the boundary.
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 2 complete. Stop here. Await go-ahead before Phase 3.

---

## Phase 3: Advanced

**Goal:** The centerpiece — `atcr debt resolve` CLI subcommand + `skill/debt-resolve/SKILL.md` documenting the four-stage RED→GREEN→ADVERSARIAL→REFACTOR cycle adapted from `/resolve-td`. Gated on Phases 1-2 (needs a real store to read and a stable `CONVENTIONS.md`).
**Story:** [Story 3 — `/atcr debt resolve` Skill Route](plan/user-stories/03-atcr-debt-resolve-skill-route.md)

### 3.1 [x] **[Debt Resolve Route — RED](plan/user-stories/03-atcr-debt-resolve-skill-route.md)**
   **Mode:** Moderate | **ACs:** [03-01](plan/acceptance-criteria/03-01-skill-md-dispatcher-documentation.md), [03-02](plan/acceptance-criteria/03-02-debt-resolve-cli-subcommand.md), [03-03](plan/acceptance-criteria/03-03-item-selection-and-justification-consumption.md), [03-06](plan/acceptance-criteria/03-06-go-embed-wiring-and-test-coverage.md) (compiled-testable subset)
   1. Analyze ACs. Write failing tests:
      - `cmd/atcr/debt_resolve_test.go` — flag parsing, empty-store → "no items"/exit 0, JSON/table output, reads via `localdebt.ReadAll`, never touches `.planning/`, discoverable via `atcr debt --help`.
      - Item selection consumes `justification`/`SourceReport` when present; deterministic selection rule; CLI-subcommand-only access.
      - `skill/skill_test.go` — asserts all four stage names (RED/GREEN/ADVERSARIAL/REFACTOR) present in embedded `debt-resolve/SKILL.md`; `skill/SKILL.md` `atcr debt` row documents the route (no invented subcommand names).
   2. Verify tests fail correctly.
   **Files:** `cmd/atcr/debt_resolve_test.go` (new), `skill/skill_test.go` (extended) | **Duration:** ~1d

### 3.2 [x] **[Debt Resolve Route — GREEN](plan/user-stories/03-atcr-debt-resolve-skill-route.md)**
   Minimal code to pass (T1), verify (T2), COMMIT.
   - `atcr debt resolve` subcommand extending `newDebtCmd()` (alongside `list`/`add`/`dashboard`): read path via `localdebt.ReadAll`; deterministic item selection consuming `justification`/`SourceReport`; append-only mark-resolved write path; validated flag enums (`--severity`, etc.); store path rooted under `.atcr/debt/`.
   - `skill/debt-resolve/SKILL.md` — documents the four-stage cycle adapted from `/resolve-td` (incl. non-overridable `llm_support_diff_smell` hard verdict, symbol-anchor relocation for drifted findings, cumulative adversarial pass), branch-safety (`debt-resolve/<date>` only from default branch), `NEEDS_REVIEW` never marked resolved. Points at `skill/CONVENTIONS.md`.
   - Embed new file(s) in `skill/skill.go`; update `skill/SKILL.md` `atcr debt` dispatcher row.
   3. COMMIT: `git commit -m "feat(debt): add atcr debt resolve subcommand + debt-resolve skill (green)"`
   **Files:** `cmd/atcr/debt.go`, `cmd/atcr/debt_resolve.go` (new), `skill/debt-resolve/SKILL.md` (new), `skill/skill.go`, `skill/SKILL.md` | **Duration:** ~1.5d

### 3.2.A [x] **[Debt Resolve Route — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-atcr-debt-resolve-skill-route.md)**
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

   **Subagent findings (fresh-context general-purpose hostile review):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | cmd/atcr/debt_resolve.go:203-211 | `--resolve <id>` on an already-resolved id is not idempotent: the original open record (empty status) is append-only and still matches the scan, so `markDebtResolved` appends a second `resolved` record and prints success instead of a no-op/notice. Folds still correct (no crash / no wrong open list), but repeated runs bloat the store with duplicate resolution records. | Before appending, scan for an existing terminal (`resolved`/`deferred`) record for the id; if one exists, report "already resolved" and skip the append. |
   | LOW | skill/debt-resolve/SKILL.md Branch Safety | Branch Safety mandates `debt-resolve/<date>` on the default branch but gives no guidance for the "existing `debt-resolve/*` branch" edge case — a second same-day run's `git checkout -b` collides and the skill does not say to reuse/suffix/halt. | Add a sentence: if `debt-resolve/<date>` already exists, resolve in place on it (or append a disambiguating suffix) rather than failing the checkout. |

   No CRITICAL/HIGH/MEDIUM. Verified clean: RFC3339+"-resolve" RunID always yields a valid YYYY-MM prefix so `Append` never rejects it; `renderResolveJSON` emits `[]` not `null` on empty; fold drops resolved items correctly (O(n) + O(k log k) stable sort matching the skill's documented severity-DESC/ts-ASC rule); empty/missing store → "no items", exit 0; `--severity` enum-validated (usage error, exit 2); `--max 0` = no cap; unknown id errors; mark-resolved write failures surface (not swallowed); zero `.planning/` reference in the resolve path (the `.planning/` constants belong only to sibling list/add/dashboard); fixed branch template with no finding-text interpolation; `justification`/`source_report` framed untrusted; diff_smell-unavailable fallback documented; `NEEDS_REVIEW` never marked resolved; both SKILL.md files carry valid name+description frontmatter; skill_test assertions substantive (ordering/content anchors/format), not tautological.

   **Action taken:** No CRITICAL/HIGH → nothing blocks the boundary. Both LOWs are trivial (<30 min) and directly improve the shipped code/skill → addressed inline in 3.3 REFACTOR (per the Phase 1 task 1.2.A precedent, since REFACTOR immediately follows and its purpose is code improvement), not deferred.

### 3.3 [x] **[Debt Resolve Route — REFACTOR](plan/user-stories/03-atcr-debt-resolve-skill-route.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any).
   2. Improve code/skill wording (T1); verify `skill/debt-resolve/SKILL.md` grounds each stage against `/resolve-td` semantics and references the finished `CONVENTIONS.md`.
   3. Validate all tests pass (T3).
   4. COMMIT: `git commit -m "refactor(debt): address review + tighten debt-resolve cycle doc"`
   **Duration:** ~0.5d

### 3.4 [x] **Story 3 — Definition of Done**
   Run DoD verification checklist. Confirm compiled-testable ACs (03-01, 03-02, 03-03, 03-06) satisfied. E2E ACs 03-04, 03-05 deferred to Phase 5 live scenario walkthrough (noted, not skipped).
   - [x] T3 tests passing (CLI + embed/stage-name assertions) — `go test ./...` green (41 packages ok)
   - [x] Coverage ≥80% — cmd/atcr 84.9% (skill is Markdown/embed-only, no statements)
   - [x] Lint/vet clean; `go build ./...` succeeds — golangci-lint 0 issues, `go vet` clean, build OK
   - [x] `atcr debt resolve --help` discoverable; zero `.planning/` references in new code paths — the only `.planning/` mentions in the new path are intentional prose negations ("no `.planning/`", "zero `.planning/` dependency") in comments/docs, not functional path references

   **Story-3 DoD Complete** — Auto: 3/3 (tests/lint/build) | Story-Specific compiled ACs 03-01/03-02/03-03/03-06: satisfied (resolve subcommand registered + `--help`-discoverable, reads `.atcr/debt/` via `localdebt.ReadAll` only and never the `.planning/` source flags, empty/missing-store no-op exit 0, JSON `[]`-on-empty, severity-DESC/ts-ASC/N=10 deterministic selection, `--severity` enum-validated, append-only idempotent mark-resolved fold; `skill/debt-resolve/SKILL.md` embedded as `DebtResolveMD`, documents all four cycle stages + selection rule + untrusted-data framing + symbol-anchor preference + CLI-only access + `CONVENTIONS.md` reference, added to `TestSkill_NoAbsoluteOrClaudePaths`; SKILL.md `atcr debt` row extended) | E2E ACs 03-04/03-05 deferred to Phase 5 live walkthrough | Manual Review: [ ] (deferred to /execute-code-review).

### 3.5 [x] **Phase 3 — GATE: Integration & Exit Review (subagent)**
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

   **Gate findings (fresh-context hostile-integrator review):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | cmd/atcr/debt_resolve.go:95 (with reconcile.go:205) | "Resolved wins forever": `selectOpenDebt` folds an id closed if any record ever carried a terminal status, and the Phase-2 reconcile dedup counts resolved records in `seen`, so a resolved-then-re-detected finding never re-opens. No re-open path. | Cross-phase re-open design; captured → TD-004 (see below). |
   | LOW | skill/SKILL.md:77 | Dispatcher row attributed "autonomously fixes" to the CLI subcommand, which only lists/marks-resolved; the *route* fixes. Mild drift vs. debt-resolve/SKILL.md's "never a code editor." | Fixed inline: reworded the `atcr debt` row to attribute fixing to the route, not the CLI. |

   No CRITICAL/HIGH. Verified stable for downstream: `atcr debt resolve` flags (`--dir --list --json --severity --max --resolve`) + output shapes stable; embedded stage names (RED/GREEN/ADVERSARIAL/REFACTOR) stable; discoverable via `atcr debt --help` and documented in SKILL.md's dispatcher row (no invented subcommand names); reads store ONLY via `localdebt.ReadAll`, never raw file / never `.planning/`; `debt-resolve/SKILL.md` references the finished `CONVENTIONS.md`; fixed `debt-resolve/<date>` branch template with no finding-text interpolation; selection rule (severity DESC, ts ASC, N=10) matches implementation exactly; sibling `list`/`add`/`dashboard` + reconcile hook behavior unchanged; other embedded skill files intact. Story 5 (Phase 4 docs) can describe the shipped behavior accurately — with TD-004's terminal-resolution note folded in.

   **Action taken:** No CRITICAL/HIGH → boundary not blocked. LOW (skill/SKILL.md wording) fixed inline (a real doc/code inaccuracy, cheaper to correct now than ship). MEDIUM (resolved-wins-forever) is a cross-phase design decision outside Story 3's scope — `selectOpenDebt` correctly reflects the append-only store's current semantics, and a proper re-open path requires changing Phase-2 reviewed `persistLocalDebt` too → captured as TD-004, with a note for Story 5 to document the terminal-resolution behavior.

   **Phase gate passed.**
   **Duration:** 15-30 min

**🚧 GATED STOP:** Phase 3 complete. Stop here. Await go-ahead before Phase 4.

---

## Phase 4: Integration & Documentation

**Goal:** Document the new capability in `docs/skill-usage.md` and run a cross-cutting dispatcher-table/consistency check now that Stories 1-4 have landed.
**Story:** [Story 5 — Document Debt-Resolve in skill-usage.md](plan/user-stories/05-document-debt-resolve-in-skill-usage.md)

### 4.1 [x] **[Skill-Usage Docs — RED](plan/user-stories/05-document-debt-resolve-in-skill-usage.md)**
   **Mode:** Moderate | **ACs:** [05-01](plan/acceptance-criteria/05-01-debt-resolve-route-documentation.md), [05-02](plan/acceptance-criteria/05-02-local-td-store-storage-section.md), [05-03](plan/acceptance-criteria/05-03-public-private-debt-disambiguation.md)
   1. Analyze ACs. Write failing doc-presence assertions (mirroring `internal/scorecard`'s doc test pattern where applicable): `docs/skill-usage.md` contains the `/atcr debt resolve` route section (purpose, invocation, behavior); a local `.atcr/`-scoped Storage section (location, population, `--no-local-debt`) mirroring `docs/scorecard.md`; and the explicit public/local-vs-private-`.planning/` disambiguation cross-linked to `docs/technical-debt.md`.
   2. Verify assertions fail correctly.
   **Files:** doc-presence test (co-located as appropriate) | **Duration:** ~0.25d

### 4.2 [x] **[Skill-Usage Docs — GREEN](plan/user-stories/05-document-debt-resolve-in-skill-usage.md)**
   Minimal edits to pass (T1), verify (T2), COMMIT.
   - Extend `docs/skill-usage.md`: `/atcr debt resolve` route (purpose/invocation/behavior); Storage/CLI-Usage/Privacy-Model sections mirroring `docs/scorecard.md`; explicit public/local vs. private `.planning/`-scoped `atcr debt` disambiguation, cross-linked to `docs/technical-debt.md`.
   3. COMMIT: `git commit -m "docs(skill-usage): document atcr debt resolve + local TD store (green)"`
   **Files:** `docs/skill-usage.md` | **Duration:** ~0.25d

### 4.2.A [x] **[Skill-Usage Docs — ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-document-debt-resolve-in-skill-usage.md)**
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

   **Subagent findings (fresh-context general-purpose hostile review; verified every doc claim against ground-truth code/skill files):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | docs/skill-usage.md (Storage) | Shard filename is derived from the record's `run_id` month prefix (`monthFromRunID` in `internal/localdebt/paths.go`, called by `Append` on `rec.RunID`), not from the `ts` field as the doc's "named from each record's timestamp" states. They coincide today (run_id prefix == ReconciledAt == ts) but the causal claim is imprecise. | Reword to "named from each record's `run_id` month prefix". |
   | LOW | docs/skill-usage.md (route) | Doc quotes the empty-store output as `"no items to resolve"`; the actual CLI literal (`cmd/atcr/debt_resolve.go:177`) is `"No items to resolve (the local TD store has no open items)."` (capital N + qualifier). Behaviorally correct; only the quoted wording differs. | Drop the quotes (paraphrase) so it doesn't imply a verbatim string. |

   No CRITICAL/HIGH/MEDIUM. Verified correct against source: `.atcr/debt/YYYY-MM.jsonl` path/shard, `0600`/`0700` perms + lazy dir creation, `--no-local-debt` opt-out (no exit-code/output effect), severity-DESC + oldest-first + cap-10 (`--max` override) selection, `FindingID` dedup (severity excluded), empty/absent store exits 0, `debt-resolve/<date>` branch safety, `.planning/` disambiguation + `(technical-debt.md)` cross-link, `#technical-debt-resolution` anchor. No `.planning/` conflation. All 11 doc-presence assertions map to landed required content (non-tautological).

   **Action taken:** No CRITICAL/HIGH → boundary not blocked. Both LOWs are trivial (<30 min) doc-precision corrections that directly improve the shipped doc → addressed inline in 4.3 REFACTOR (per the Phase 1 task 1.2.A / Phase 3 task 3.2.A precedent, since REFACTOR immediately follows and its purpose is exactly this cleanup), not deferred to `tech-debt-captured.md`.

### 4.3 [x] **[Skill-Usage Docs — REFACTOR + Consistency Check](plan/user-stories/05-document-debt-resolve-in-skill-usage.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A (if any).
   2. Cross-cutting consistency check: verify `skill/SKILL.md`'s `atcr debt` row, `skill/skill_test.go`'s structural assertions, and the `CONVENTIONS.md` references across both skill files are internally consistent now that Stories 1-4 have landed. Reconcile any drift.
   3. Validate all tests pass (T3).
   4. COMMIT: `git commit -m "docs(skill-usage): address review + dispatcher consistency check"`
   **Duration:** ~0.5d

### 4.4 [x] **Phase 4 — Definition of Done**
   Run DoD verification checklist. Confirm ACs 05-01..05-03 satisfied and dispatcher/skill consistency verified.
   - [x] T3 tests passing (`go test ./...` green, incl. new `skill/docs_test.go` doc-presence test — 11 assertions)
   - [x] Docs complete and cross-linked (`## Technical Debt Resolution` section: route + Storage + disambiguation callout, `[technical-debt.md](technical-debt.md)` cross-link resolves)
   - [x] Lint/vet clean (`golangci-lint` 0 issues, `go vet ./...` clean, `gofmt` clean, `go build ./...` ok)
   - [x] `skill/SKILL.md` ↔ `skill/debt-resolve/SKILL.md` ↔ `CONVENTIONS.md` consistent (skill_test.go structural assertions green; consistency check reconciled a real install-doc drift — `cp skill/*.md` → `cp -R skill/.` so the nested `debt-resolve/` route is actually installed, and the stale "three sibling files / all four" intro updated to the current file set)

   **Story-5 DoD Complete** — Auto: 3/3 (tests/lint/build) | Story-Specific ACs 05-01/05-02/05-03: satisfied (`/atcr debt resolve` route documented — purpose/invocation/RED→GREEN→ADVERSARIAL→REFACTOR behavior/empty-store no-op; local `.atcr/debt/YYYY-MM.jsonl` Storage section mirroring `docs/scorecard.md` — path/rotation/`0700`-`0600` perms/`FindingID` dedup/`--no-local-debt` opt-out/"do not commit" callout; unmissable public-vs-private disambiguation blockquote near the section top with a working `technical-debt.md` cross-link; content verified against landed `cmd/atcr/debt_resolve.go`, `cmd/atcr/reconcile.go`, `skill/debt-resolve/SKILL.md` by the 4.2.A fresh-subagent review, not story drafts) | Manual Review: [ ] (deferred to /execute-code-review).

### 4.5 [x] **Phase 4 — GATE: Integration & Exit Review (subagent)**
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

   **Gate findings (fresh-context hostile-integrator review; every doc claim verified against ground-truth code):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | docs/skill-usage.md (Installation) | The `cp -R skill/.` install (introduced in 4.3 to capture the nested `debt-resolve/` route) also copies the package's Go files (`skill.go`, `skill_test.go`, `docs_test.go`) into `.claude/skills/atcr/` — harmless (the `go` tool ignores dot-dirs; the agent loads only `.md`) but a mild tension with the doc's "None contain executable code." | Copy only markdown, including the subdir: `cp skill/*.md …` + `cp skill/debt-resolve/*.md …/debt-resolve/`. |

   No CRITICAL/HIGH/MEDIUM. Verified stable/accurate for the phase exit: store path `.atcr/debt/YYYY-MM.jsonl`, `0700`/`0600` perms + lazy dir, `FindingID` dedup, `--no-local-debt` opt-out, selection rule (severity DESC / oldest-first / cap-10), empty-store exit 0 no-op, `debt-resolve/<date>` branch safety, `justification`/`source_report` untrusted framing, private `list`/`add`/`dashboard` → `.planning/technical-debt/`, and both cross-links (`technical-debt.md`, `scorecard.md`) exist with the correct relative-path convention. Nothing left for Phase 5 but validation.

   **Action taken:** No CRITICAL/HIGH → boundary not blocked. The single LOW is a trivial (<5 min) doc-only correctness improvement to the artifact this very phase produced — fixed inline (copy only `.md`, keep the `debt-resolve/` subdir) rather than filing a TD note, keeping "None contain executable code" true for the installed dir. Doc-presence test re-verified green after the fix; committed as `b03bd8b3`.

   **Phase gate passed.**
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
