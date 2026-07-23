# Sprint 33.0: final documentation sweep

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 33.0 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

A final launch-readiness gate for `atcr` before its codebase and full git history go public (Epic 33.2): a comprehensive code review (atcr's own multi-agent reviewer dogfooded against itself, plus a manual adversarial/security pass) followed by a documentation sweep confirming `README.md`, `docs/`, inline CLI help, and schemas are accurate against the finalized code, free of legacy persona slugs (`sentinel`, `tracer`, `idiomatic`), and ready for `atcr.dev` import.

### Why This Matters

Two risks must close before the repo and its history are exposed publicly: latent correctness/security/quality issues shipping without a final review gate, and stale documentation misleading users or the future `atcr.dev` website. Review and fix the code first, then document it against the finalized state.

### Key Deliverables

- Reconciled multi-agent + adversarial code-review findings report
- Every CRITICAL/HIGH finding fixed in the codebase via RED → GREEN → ADVERSARIAL → REFACTOR
- MEDIUM/LOW findings sharded into `.planning/technical-debt/README.md`
- `README.md`, `docs/` (29 files), `skill/SKILL.md`, and CLI help verified accurate against finalized code
- Zero legacy persona-slug references (`sentinel`, `tracer`, `idiomatic`) in docs/help output
- `docs/` validated as clean, self-contained, and ready for `atcr.dev` import
- Fresh end-to-end guard run (tests, vet, lint, `reconcile/` submodule) confirming AC1-AC5

### Success Criteria

- AC1: Comprehensive code review executed; all CRITICAL/HIGH findings fixed; MEDIUM/LOW captured as technical debt.
- AC2: No secrets, credentials, or embarrassing artifacts remain in the codebase or git history.
- AC3: No legacy persona names (`sentinel`, `tracer`, `idiomatic`) remain in documentation or command help screens.
- AC4: All features up to Epic 23.0 are fully and accurately documented.
- AC5: Documentation files are validated and ready to be imported into `atcr.dev`.

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

This is a **Technical Debt** plan (task-based, not user-story/TDD-element based). Task 3 (Findings Triage) is the sprint's sole TDD-disciplined element: every CRITICAL/HIGH finding is fixed via a per-finding RED (failing reproduction test) → GREEN (minimal fix) → ADVERSARIAL (non-overridable self-check for test-only changes, weakened assertions, lint suppressions, stubbed bodies) → REFACTOR (cleanup, re-verify green) cycle, applied directly against the production files the review surfaces. All other tasks are review/audit/verification tasks (Type: Add/Fix, Effort: S/M/L) validated via reproducible commands and manual checklists rather than new unit tests. Gated mode is enabled: each phase below ends with a Phase-Boundary Gate review task, and `/execute-sprint` stops at each phase boundary for confirmation before continuing.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [tasks/](plan/tasks/) | The 8 task definitions this sprint executes |
| [documentation/](plan/documentation/) | Grounding docs: multi-agent review workflow, TD triage/resolution, persona naming accuracy |

---

## Clarifications

### Phase 1 Clarifications (recorded 2026-07-22)

**Key Decisions:**
- **Task 1 review range:** Single full-tree `atcr review`, `--base 4b825dc642cb6eb9a060e54bf8d69288fbee4904` (git empty-tree SHA) `--head HEAD`, with `--byte-budget 0` on that one invocation only (NOT a `.atcr/config.yaml` change). Rationale: the default 512 KB payload budget (`internal/payload/budget.go:46`) would drop ~90% of the ~6.1 MB `cmd/`+`internal/`+`reconcile/`+`skill/` tree before any reviewer sees it. A single review id keeps Task 3's single-artifact handoff intact.
- **Do NOT split by commit range** — fragmenting across multiple review IDs would break Task 3's single reconciled-artifact handoff.
- **Cost:** Full dogfood run over the 11-persona panel is explicitly authorized by the maintainer.
- **Secret-in-history (Task 2):** Detect-and-ESCALATE only. Report any committed-secret finding and STOP for user approval. Never rewrite history (`git filter-repo`/BFG out of scope).

**Scope Boundaries:**
- IS in scope: full-tree review, fix CRITICAL/HIGH via RED→GREEN→ADVERSARIAL→REFACTOR, route MEDIUM/LOW to TD, docs audit, persona verification, `atcr.dev` docs validation, final guard run.
- NOT in scope: git-history secret remediation, the `atcr.dev` website repo itself, Epic 33.1/33.2 launch content.

**Technical Approach:**
- `atcr` binary builds green; 11-persona panel + provider API keys configured; `golangci-lint` installed at `/opt/homebrew/bin/golangci-lint`. Both prior unvalidated assumptions resolved empirically before execution.

### Phase 2 Clarifications (recorded 2026-07-22)

**Key Decisions:**
- **Findings inventory:** Merged stream = 25 reconciled rows (Task 1) + Task 2 CLEAN (0 rows). 2 rows are the hallucinated `legacy.go:7` duplicates already grounded-out as TD-002/TD-003 → dropped. 23 real findings remain.
- **Severity re-verification (task-03 Step 2 mandate):** Only 2 rows carry HIGH in the SEVERITY column; the rest of the "HIGH" values sit in the CONFIDENCE column, not severity. Both HIGH-severity rows re-classify DOWN against actual impact:
  - `internal/tools/open_other.go:19` (labeled HIGH/correctness): **REFUTED.** The symlink-swap TOCTOU claim misreads the code — `preStat` is captured by `os.Lstat` BEFORE `os.OpenFile`, so a swap in the window yields `preStat != postStat` and `os.SameFile` rejects it. The `mira` sub-claim "preStat never used" is factually wrong (used at line 27). Code is correct as written. A single residual LOW nit (directory paths not explicitly rejected) routes to TD.
  - `internal/atomicwrite/atomicwrite_test.go:1` (labeled HIGH/testing): re-classify to MEDIUM — a missing test case, not a production correctness/security bug.
- **Net result:** ZERO genuine CRITICAL/HIGH production-code findings. No RED→GREEN production fix required in Phase 2; every finding routes to `code-review/triaged-findings-medium-low.md`. This is the documented outcome of Task 3 Step 2 (re-verify against actual impact), not scope reduction.

**Scope Boundaries:**
- Every re-classification logged with reasoning in `triage-summary.md`. Fresh-context Phase 2 adversarial (2.1.A) + gate (2.3) subagents independently re-check the refutations.

**Technical Approach:**
- Merge = direct concatenation (shared 9-column `atcr-findings/v1` shape). Dedupe already effectively done (only the `legacy.go:7` pair duplicated; dropped). Baseline `go test ./...`/`go vet`/`golangci-lint` verified green before and after.

### Phase 3 Clarifications (recorded 2026-07-22)

**Key Decisions:**
- **Audit baseline:** Phase 2 gate (2.3) confirmed ZERO production `.go` files changed, so Task 4 audits `README.md`, `docs/` (29 files), `skill/SKILL.md`, and CLI help against a finalized codebase byte-identical to the Phase 1-reviewed state. `go run ./cmd/atcr --help` is the authoritative CLI surface.
- **Persona slug mapping (Task 5):** `sentinel`→`sasha`, `tracer`→`penny`, `idiomatic`→`ingrid` (never `ian`). Every hit classified against `plan/documentation/persona-naming-doc-accuracy.md`'s false-positive table before edit — legitimate Go sentinel-error idioms, sentinel-delimiter lines, and the "idiomatic" adjective stay untouched. `TestNoRetiredSlugs` is the authoritative gate.
- **TD sharding (Task 7):** Input = `code-review/triaged-findings-medium-low.md` (23 rows, verified present). Append one new dated section `### [2026-07-22] From Review: 33.0_final_documentation_sweep` to `.planning/technical-debt/README.md`; new-section row count MUST equal 23. Commit with a pathspec (never `git add -A`) — shared file, concurrent sessions possible.

**Scope Boundaries:**
- IN scope: docs accuracy audit, persona verification, MEDIUM/LOW TD sharding.
- NOT in scope: the `atcr.dev` website repo (that's Task 6/Phase 4), Epic 33.1/33.2 launch content, git-history secret remediation. A doc error tracing to a genuine missed code bug is a NEW finding routed to TD, not a silent inline fix.

**Technical Approach:**
- `atcr` builds green; `golangci-lint` at `/opt/homebrew/bin`; provider keys configured (validated in Phase 1). Task 4/Task 5 both edit shared files (`README.md`, `skill/SKILL.md`) — Phase 3 gate (3.5) checks for non-conflicting overlap.

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./<package>/...` |
| T2: Module | After completing a task element | `go test ./<module>/...` |
| T3: Full | DoD validation, pre-commit | `go test -race ./...`, `(cd reconcile && go test ./...)` |

### DoD Verification Checklist
1. Tests (T3): All passing (`go test -race ./...`, `(cd reconcile && go test ./...)`)
2. Coverage: ≥80% (`go test -coverprofile=coverage.out ./...`)
3. Lint: `golangci-lint run` clean, `go vet ./...` clean
4. Build: Succeeds
5. Docs: Updated

### DoD Report Template
```
Phase-N DoD Complete
Auto: {X}/5 | Task-Specific: {Y}/{Z}
Manual Review: [ ] Code reviewed
```

### Commit Process
Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

---

## Development Standards

### Coding Standards (Go)
- Naming: packages lowercase single-word; exported `PascalCase`; unexported `camelCase`; files snake_case or lowercase.
- Imports grouped: stdlib → third-party → internal (`github.com/samestrin/atcr/...`), arranged via `goimports`.
- Receiver names: 1-3 letters, no `this`/`self`.
- Error handling: `error` last return value, never ignored, wrapped with `fmt.Errorf("doing action: %w", err)`. No `panic` for normal error conditions.
- Context: `context.Context` as first param for I/O/long-running operations; respect cancellation.
- Interfaces small; constructors return concrete types, params accept interfaces.
- Testing: table-driven tests, `*_test.go` co-located with code under test, integration tests behind `//go:build integration`.
- Tooling gates: `go fmt`/`goimports` before check-in, `golangci-lint run` (staticcheck + errcheck), `go vet ./...` on every commit/sprint.

### Implementation Standards
- Black-box module interfaces; hide implementation details.
- Replaceable components — any module rewritable from its interface alone.
- Single-responsibility modules; primitive-first design.
- Go/MCP specifics: goroutine panic safety, `defer` for resource cleanup, return concrete types from constructors, robust JSON-RPC input validation.

### Git Strategy
- GitHub Flow / trunk-based: `main` always deployable, `feature/` short-lived branches.
- Conventional Commits: `type(scope): description` (`feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`).
- One logical change per PR, squash-merge, delete branch after merge.
- CI gates PRs on `Go CI` workflow (format, vet, lint, unit tests) before merge.

---

## External Resources

- **[CRITICAL]** [multi-agent-review-workflow.md](plan/documentation/multi-agent-review-workflow.md) — atcr's `/atcr <command>` dispatcher orchestration (range → review → poll → host review → reconcile → report → output path); grounds Task 1.
- **[CRITICAL]** [technical-debt-triage-resolution.md](plan/documentation/technical-debt-triage-resolution.md) — RED → GREEN → ADVERSARIAL → REFACTOR per-item resolution cycle and the sharded TD README store; grounds Tasks 3 and 7.
- **[IMPORTANT]** [persona-naming-doc-accuracy.md](plan/documentation/persona-naming-doc-accuracy.md) — false-positive table distinguishing legitimate "sentinel"/"idiomatic" technical usage from stale persona-slug references, plus the docs/ website-export requirement; grounds Tasks 4-6.
- **[REFERENCE]** [source.md](plan/documentation/source.md) — index of `skill/SKILL.md` and `skill/debt-resolve/SKILL.md` as grounding sources.

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation — Code Review Execution

**Duration:** 1 day | **Items:** Task 1, Task 2 (run in parallel — no dependency between them)

### 1.1 [x] **🔧 Task 01: Multi-Agent Code Review — Dogfood atcr Against Its Own Production Codebase** — review id `33.0-dogfood`, deliverable `.atcr/reviews/33.0-dogfood/` (partial:true, 9/11 agents + host; 25 reconciled findings)
   **Task:** Run the `/atcr` skill's 7-step review orchestration (range → review → poll status → host review → reconcile → report → path output) against `cmd/`, `internal/`, `reconcile/`, `skill/`, dogfooding atcr's own multi-agent reviewer.
   **Priority:** P1 | **Effort:** M
   1. Resolve the review range (`atcr range`), covering the full current state of the four target directories.
   2. Start `atcr review` in the background; capture the review id.
   3. Poll `atcr status <id>` every 10s (up to 60x); accept `partial: true` if ≥1 source succeeds.
   4. Run the host (+1) review pass per `skill/host-review.md`'s anti-hallucination rules, focused on security/secrets/dead-code/unsafe patterns.
   5. `atcr reconcile <id>` then `atcr report <id> --format md`.
   6. Record `.atcr/reviews/<id>/` as the deliverable for Task 3.
   7. Run `go test ./personas/... ./internal/personas/...` (informational AC3 confirmation).
   **Success Criteria:** (see [task-01](plan/tasks/task-01-multi-agent-code-review.md))
   **Files:** `.atcr/reviews/<id>/sources/host/findings.txt`, `.atcr/reviews/<id>/reconciled/`, `.atcr/reviews/<id>/report.md` | **Duration:** 1 day

### 1.1.A [x] **Adversarial Review (subagent): Task 1**
   **Changed Files:** `.atcr/reviews/33.0-dogfood/sources/host/findings.txt`, `.atcr/reviews/33.0-dogfood/reconciled/`, `.atcr/reviews/33.0-dogfood/report.md`

   **Subagent findings (fresh context, 2026-07-22):** COVERAGE ✅ (diff = exactly cmd/101 + internal/571 + reconcile/46 + skill/9, zero files outside the four dirs), GROUNDING/host ✅ (both host findings cite real code), RECONCILE INTEGRITY ✅ (ran against this id, sources_scanned=[host,pool], report.md 277 lines), COMPLETENESS ✅ (partial:true; the 2 failed agents kai/ronin recorded status:failed with error strings, not silently clean). **2 MEDIUM defects** (both = `archer` scraping a golden-testdata fixture fence):
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | reconciled/findings.txt:4 | Hallucinated `legacy.go:7` finding (file does not exist) scraped by the pool parser from a fenced golden-testdata example block in archer/review.md; carried into reconciled output as HIGH, inflating HIGH to 4 | Parser: ignore fenced example blocks; drop findings whose cited file is absent from head tree → **TD-002** |
   | MEDIUM | reconciled/findings.txt:4-5 | Dedup failure: the `legacy.go:7` row appears twice byte-identical; reconcile FILE:LINE±3 clustering did not collapse the identical pair, inflating total_findings to 25 | Collapse byte-identical/same-FILE:LINE findings in reconcile clustering → **TD-003** |

   **Result:** ✅ No CRITICAL/HIGH. 2 MEDIUM routed to `tech-debt-captured.md` (TD-002, TD-003). Proceed.

   **Spawn a fresh subagent** via the Agent tool. The subagent has no memory of Task 1's execution — this is intentional, to avoid "I ran it, it's fine" bias.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: Task 1`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the review directory `.atcr/reviews/<id>/` (host findings, reconciled artifacts, report.md)
     - Checklist (pass verbatim):
       - COVERAGE: Does the review actually cover `cmd/`, `internal/`, `reconcile/`, `skill/`, or is the range wrong/empty?
       - GROUNDING: Does every host-review finding cite concrete file:line evidence (no hallucinated findings)?
       - RECONCILE INTEGRITY: Did reconcile/report actually run, or was a stale `.atcr/latest` used instead of the explicit review id?
       - COMPLETENESS: Was partial failure (if any) correctly noted rather than silently dropped?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues, fix before proceeding to Task 3
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.2 [x] **🔧 Task 02: Adversarial/Security Pass — Manual Review for Secrets, Dead Code, Unsafe Patterns, TODO/FIXME**
   **Task:** Manual, repo-wide adversarial security sweep (broader scope than Task 1) across secrets/credentials, tracked sensitive files, git history, TODO/FIXME/HACK/XXX, dead code, and unsafe command/path-handling patterns.
   **Priority:** P1 | **Effort:** M
   1. Grep repo-wide for hardcoded secrets/credentials (AWS keys, API tokens, private key blocks, credential variable assignments).
   2. Check `git ls-files` for accidentally-tracked sensitive file types.
   3. Scan `git log --all -p` for the same secret patterns (detect-and-escalate only; no history rewrite).
   4. Repo-wide TODO/FIXME/HACK/XXX sweep, all file types.
   5. `go vet ./...` / `golangci-lint run` + manual dead-code grep.
   6. Grep for command-injection/path-traversal-prone constructs; confirm untrusted-path call sites route through `internal/security/pathguard.go`.
   7. Compile all findings into `.planning/sprints/active/33.0_final_documentation_sweep/code-review/adversarial-findings.txt` in reconciled `atcr-findings/v1` 9-column format (`REVIEWERS=adversarial-pass`, `CONFIDENCE=HIGH`).
   8. Filter "sentinel"/"idiomatic" hits against legitimate-usage examples before logging (avoid duplicating Task 5's scope).
   **Success Criteria:** (see [task-02](plan/tasks/task-02-adversarial-security-pass.md))
   **Files:** `.planning/sprints/active/33.0_final_documentation_sweep/code-review/adversarial-findings.txt` | **Duration:** 1 day

### 1.2.A [x] **Adversarial Review (subagent): Task 2**
   **Changed Files:** `.planning/sprints/active/33.0_final_documentation_sweep/code-review/adversarial-findings.txt`

   **Spawn a fresh subagent** via the Agent tool. No memory of Task 2's execution.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: Task 2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `.planning/sprints/active/33.0_final_documentation_sweep/code-review/adversarial-findings.txt`, plus a spot-check of the 6 sweep commands' actual reproducibility
     - Checklist (pass verbatim):
       - SECURITY: Any secret/credential hit left unescalated or unlogged?
       - COMPLETENESS: All 6 sweep categories actually executed, not assumed clean?
       - FALSE POSITIVES: Any "sentinel"/"idiomatic" false positive incorrectly logged as a finding?
       - FORMAT: Findings log actually in valid 9-column `atcr-findings/v1` format, mergeable with Task 1's output?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh context, re-ran all 6 sweep commands independently — 2026-07-22):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | _(none)_ | | | |

   Independently verified: security (tree+history) clean — AWS EXAMPLE key + `glpat-abcDEF123` + `ghp_SECRETTOKEN...` + `super-secret-api-key-xyz` all confirmed test fixtures; TODO/FIXME/HACK/XXX=0; `go vet` (root+reconcile)=0; `golangci-lint`=0 issues; no tracked secret files; `nolint`=0; no false positives logged; format valid + mergeable.

   **Result:** ✅ Adversarial review passed (no CRITICAL/HIGH/MEDIUM/LOW). Proceed.

   **Action Required:**
   - CRITICAL/HIGH found -> List issues, fix before proceeding to Task 3
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.3 [x] **Phase 1 DoD Check**
   1. ✅ Both artifacts exist & non-empty: `.atcr/reviews/33.0-dogfood/reconciled/findings.txt` (25 findings) + `code-review/adversarial-findings.txt` (documented clean outcome).
   2. ✅ `go test ./personas/... ./internal/personas/...` passes (both packages `ok`).
   3. ✅ No unescalated secret findings — Task 2 + 1.2.A confirmed zero real secrets (only EXAMPLE/synthetic fixtures).
   **Report:**
   ```
   Phase-1 DoD Complete
   Auto: 5/5 (persona/vet/lint/build green; coverage+docs N/A — review-only phase, no code changed) | Task-Specific: 2/2
   Manual Review: [ ] Code reviewed  (→ /execute-code-review)
   ```

### 1.4 [x] **Phase 1 - GATE: Integration & Exit Review (subagent)** — ✅ Phase gate passed (0 findings)

   **Gate result (fresh hostile-integrator subagent, 2026-07-22):** CONTRACT EXIT ✅ (stream A = 25 rows × 9 cols valid; stream B = valid CLEAN outcome; both concatenation-mergeable), CONFIG SURFACE ✅ (`.atcr/config.yaml` untouched), INTEGRATION ✅ (stream B zero findings → no file:line conflict), PHASE-EXIT CONTRACT ✅ (`legacy.go:7` hallucination grounded-out via TD-002/TD-003, both artifacts parser-clean & directly consumable by Task 3), REGRESSION ✅ (`git status`/`git diff --stat` show only `.planning/` + review artifacts — zero production `.go` files changed). No CRITICAL/HIGH/MEDIUM/LOW. Proceed to phase stop.

   **Scope:** All files/artifacts changed during Phase 1 (`.atcr/reviews/33.0-dogfood/`, `.planning/sprints/active/33.0_final_documentation_sweep/code-review/adversarial-findings.txt`)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. No memory of Phase 1's implementation.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 1 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Both findings streams conform to the same 9-column `atcr-findings/v1` shape, mergeable by concatenation?
       - CONFIG SURFACE: N/A (no new config surface introduced)
       - INTEGRATION: Task 1 and Task 2 findings streams don't silently disagree on the same file:line without reconciliation?
       - PHASE-EXIT CONTRACT: Can Task 3 consume both artifacts directly without rework?
       - REGRESSION: No production code was modified in Phase 1 (review-only phase)?
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

## Phase 2: Core Items — Findings Triage & Remediation

**Duration:** 3 days | **Items:** Task 3

### 2.1 [x] **🔧 Task 03: Findings Triage — Classify, Fix CRITICAL/HIGH, Route MEDIUM/LOW to Technical Debt** — 23 findings triaged (2 hallucinated `legacy.go:7` dropped); 0 CRITICAL/HIGH after re-verification (both HIGH rows re-classified down, `open_other.go` symlink-swap claim REFUTED); 20 MEDIUM + 3 LOW routed to `code-review/triaged-findings-medium-low.md`; `triage-summary.md` written; full gate green, zero production `.go` changes
   **Task:** Merge and deduplicate Phase 1's two findings streams, re-verify severity, and run the RED → GREEN → ADVERSARIAL → REFACTOR cycle against every CRITICAL/HIGH finding directly in the production codebase.
   **Priority:** P1 | **Effort:** L
   1. Merge Task 1 + Task 2 findings streams; dedupe by FILE:LINE ±3 lines and PROBLEM theme.
   2. Re-verify/re-classify severity against actual impact (not just the reviewer's raw label); log re-classifications.
   3. Per CRITICAL/HIGH finding: (0) pre-fix evaluation, (1) RED — failing test/repro in co-located `*_test.go`, (2) GREEN — minimal fix, (3) ADVERSARIAL — non-overridable self-check (test-only changes, weakened assertions, lint suppressions, stubbed bodies), (4) REFACTOR — cleanup, re-verify green.
   4. Apply coding-standards.md to every fix; run `golangci-lint run` + `go vet ./...` after each fix/batch.
   5. Write every MEDIUM/LOW finding to `.planning/sprints/active/33.0_final_documentation_sweep/code-review/triaged-findings-medium-low.md` (9-column format + GROUP label) — do not fix inline.
   6. Write `.planning/sprints/active/33.0_final_documentation_sweep/code-review/triage-summary.md` (counts, re-classifications, CRITICAL/HIGH fix evidence, MEDIUM/LOW routing count).
   7. Run `go test ./...`, `golangci-lint run`, `go vet ./...` across the repo.
   **Success Criteria:** (see [task-03](plan/tasks/task-03-findings-triage.md))
   **Files:** `cmd/**`, `internal/**`, `reconcile/**`, `skill/**` (CRITICAL/HIGH fix targets, not enumerable ahead of the review run), co-located `*_test.go` files, `.planning/sprints/active/33.0_final_documentation_sweep/code-review/triaged-findings-medium-low.md`, `.planning/sprints/active/33.0_final_documentation_sweep/code-review/triage-summary.md` | **Duration:** 3 days (includes 1-day risk buffer for unknown finding volume)

### 2.1.A [x] **Adversarial Review (subagent): Task 3** — ✅ Passed (0 findings)
   **Changed Files:** (no CRITICAL/HIGH fix files — zero fixed) `.planning/sprints/active/33.0_final_documentation_sweep/code-review/triaged-findings-medium-low.md`, `.planning/sprints/active/33.0_final_documentation_sweep/code-review/triage-summary.md`

   **Subagent findings (fresh context, independently read `open_other.go` + `atomicwrite.go` + git status — 2026-07-22):** Triage confirmed legitimate, NOT a reward-hack. (1) `open_other.go` symlink-swap HIGH correctly REFUTED — `preStat` is a frozen `os.Lstat` snapshot taken before `OpenFile` and never re-evaluated; `SameFile(pre-open Lstat, post-open Fstat)` rejects all four swap cases; the `mira` "preStat never used" sub-claim is factually false (used at line 27); unix build additionally uses `O_NOFOLLOW`; residual directory-not-rejected LOW nit fairly characterized. (2) `atomicwrite_test.go` HIGH→MEDIUM correct — pure test-coverage gap; production error path handled at `atomicwrite.go:36-38`. (3) Zero `.go` files changed (git status verified). (4) Dropped `legacy.go:7` rows justified (no such file exists anywhere; 25−2=23 row math checks out). (5) No mis-routed HIGH in the MEDIUM list; `root.go:19` `.git`-symlink + severity-map rows genuinely low-blast-radius. Verdict: **legitimate triage, not a reward-hack.**

   **Spawn a fresh subagent** via the Agent tool. No memory of Task 3's implementation — this is intentional given Task 3's own internal ADVERSARIAL stage is already non-overridable per-finding; this is the cumulative cross-finding review.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: Task 3`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST all CRITICAL/HIGH fix files + the two handoff artifacts]
     - Checklist (pass verbatim):
       - SECURITY: Any fix introduce a new auth bypass, injection, or data exposure?
       - EDGE CASES: Do the RED tests actually cover null/empty/boundary/concurrent-access cases, or just the happy path?
       - ERROR HANDLING: Any error silently swallowed by a "minimal fix"?
       - REGRESSION: Does any fix in one package plausibly break an adjacent package not covered by its own test?
       - SCOPE: Any MEDIUM/LOW finding fixed inline instead of routed to the handoff artifact (scope creep)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings table (none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | _(none)_ | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.1.R, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed
   → **Adversarial review passed** (0 findings). Proceed.

### 2.1.R [x] **Task 03 - REFACTOR: Address Cumulative Review Findings** — no 2.1.A findings; nothing to fix. Gate re-confirmed green (`go test`/`vet`/`golangci-lint` all pass). Committed as `a1ead65c` (`docs(33.0): Phase 2 findings triage …`) — honest message: no code fixed, so not `fix(review)`.
   1. Fix CRITICAL/HIGH issues from 2.1.A (if any)
   2. Re-run `go test ./...`, `golangci-lint run`, `go vet ./...` to confirm still green
   3. COMMIT: `git commit -m "fix(review): address cumulative adversarial findings from Task 3"`
   **Duration:** (estimate, dependent on 2.1.A findings volume)

### 2.2 [x] **Phase 2 DoD Check**
   1. ✅ `go test ./...` (root exit 0), `(cd reconcile && go test ./...)` (exit 0), `golangci-lint run` (0 issues), `go vet ./...` (clean) — zero failures.
   2. ✅ 0 CRITICAL/HIGH findings after re-verification (vacuously satisfied); no NEEDS_REVIEW-flagged item unresolved.
   3. ✅ `code-review/triaged-findings-medium-low.md` (23 rows) + `code-review/triage-summary.md` exist.
   **Report:**
   ```
   Phase-2 DoD Complete
   Auto: 5/5 (test/vet/lint/build green; coverage+docs N/A — triage-only phase, no production code changed) | Task-Specific: 3/3
   Manual Review: [ ] Code reviewed  (→ /execute-code-review)
   ```

### 2.3 [x] **Phase 2 - GATE: Integration & Exit Review (subagent)** — ✅ Phase gate passed (0 findings)

   **Gate result (fresh hostile-integrator subagent, 2026-07-22):** CONTRACT EXIT ✅ (exactly 23 data rows, each 10 columns with GROUP trailing, no broken pipes; 25−2 hallucinated = 23 math sound; format matches reconciled `atcr-findings/v1` + GROUP that Task 07 consumes). CONFIG SURFACE ✅ (no `.atcr/config.yaml`/schema/config-key change; zero `.go` touched). INTEGRATION ✅ (no production code changed → codebase Phase 3 documents is byte-identical to Phase 1's reviewed state). PHASE-EXIT CONTRACT ✅ (no genuine deferred CRITICAL/HIGH; both HIGH re-classifications grounded — `open_other.go` symlink-swap REFUTED, `atomicwrite` HIGH→MEDIUM test gap; Task 02 stream confirmed genuinely CLEAN). REGRESSION ✅ (independently ran `go test -race ./...` → exit 0, no FAIL/DATA RACE/panic; zero production `.go` files changed). No CRITICAL/HIGH/MEDIUM/LOW.

   **Scope:** All files changed during Phase 2 (`code-review/triaged-findings-medium-low.md`, `code-review/triage-summary.md`, `sprint-plan.md`; zero production `.go` fixes)

   **Spawn a fresh subagent** via the Agent tool. No memory of Phase 2's implementation.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 2 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Are `.planning/sprints/active/33.0_final_documentation_sweep/code-review/triaged-findings-medium-low.md`'s rows in the exact format Task 7 expects?
       - CONFIG SURFACE: No new config keys introduced without documentation?
       - INTEGRATION: Do the CRITICAL/HIGH fixes touch any file Phase 3's docs audit (Task 4) will need to describe differently?
       - PHASE-EXIT CONTRACT: Can Phase 3 (docs audit) proceed against a truly finalized codebase, or are there loose ends?
       - REGRESSION: `go test -race ./...` still green after all Phase 2 fixes?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings table (none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | _(none)_ | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as N.X.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   → **Phase gate passed** (0 findings). Proceed to phase stop.
   **Duration:** 15-30 min

---

## Phase 3: Advanced — Documentation & Persona Audit

**Duration:** 2 days | **Items:** Task 4, Task 5, Task 7 (Task 4/Task 5 run in parallel; Task 7 depends only on Task 3 and can run concurrently with both)

### 3.1 [x] **🔧 Task 04: Code-to-Docs Accuracy Audit** — audited `README.md`, `docs/` (29 files, index complete/0 orphans), `skill/SKILL.md`, and full `--help` surface (24 subcommands) against finalized code. 5 staleness fixes (no code-traceable findings — Phase 2 changed 0 `.go`): README +`config` +`quality-report` rows, `report --format` → +`sarif`,`axi` (×2 spots), quickstart +`--offline`; SKILL.md `config` "Read/update"→"Update" (matches mutation-only Short). Persona docs accurate; zero stale command refs across all docs.
   **Task:** Audit `README.md`, `docs/` (29 files), `skill/SKILL.md`, and CLI help text against the finalized (post-Task-3) codebase.
   **Priority:** P1 | **Effort:** M
   1. Confirm Task 3's CRITICAL/HIGH fixes are merged before starting.
   2. Enumerate current CLI surface via `go run ./cmd/atcr --help` (+ subcommands).
   3. Audit `README.md` top to bottom against captured `--help` output and behavior.
   4. Audit `docs/README.md`'s 29-file index for completeness/orphans.
   5. Walk remaining `docs/*.md` files, prioritizing persona/CLI-usage/config-schema docs.
   6. Audit `skill/SKILL.md` frontmatter/body against the finalized CLI surface.
   7. Cross-check documented config/output schemas against producing Go structs.
   8. Note every correction and its traceability to a Task 1-3 finding vs. plain staleness.
   9. Re-run `--help` sweep after edits to confirm zero remaining diffs.
   **Success Criteria:** (see [task-04](plan/tasks/task-04-code-to-docs-audit.md))
   **Files:** `README.md`, `docs/README.md`, `docs/personas-authoring.md`, `docs/personas-install.md`, `skill/SKILL.md`, `cmd/atcr/root.go` (only if help-string drift found), `docs/*.md` (remaining 27, spot-audit) | **Duration:** 1 day

### 3.1.A [x] **Adversarial Review (subagent): Task 4** — 2 MEDIUM/LOW found, both FIXED inline (in-scope SKILL.md accuracy)
   **Changed Files:** `README.md`, `skill/SKILL.md`

   **Subagent findings (fresh context, independently built + ran `--help`, 2026-07-22):** All 4 claimed edits verified accurate against the real CLI; config schema (telemetry, quality_signal) matches; real subcommand set diffs clean against both README + SKILL.md tables (only cobra `completion`/`help` absent, conventional). NO CRITICAL/HIGH. Found 2 residual same-class drifts in `skill/SKILL.md` that the audit fixed in README but did not carry over:
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | skill/SKILL.md:63 | `atcr report` row read "Render md, json, or checklist views" — real short help is "md, json, checklist, sarif, axi" (same drift fixed in README) | ✅ FIXED → "Render md, json, checklist, sarif, axi views over reconciled findings" |
   | LOW | skill/SKILL.md:81 | `atcr config` row mentioned only the `telemetry` key; real `config set` supports `telemetry`+`quality_signal` (README documents both) | ✅ FIXED → "`atcr config set <telemetry\|quality_signal> <true\|false>`" |

   **Action Taken:** Both are one-line doc-accuracy fixes in `skill/SKILL.md` — the exact in-scope file Task 4 audits — so fixed inline rather than deferred to TD (deferring known-wrong public-facing doc lines would defeat the launch-readiness purpose). SKILL.md `report` row now byte-matches root `--help`. No scope bleed (no persona/link/formatting edits). Proceed.

### 3.2 [x] **🔧 Task 05: Persona Reference Verification — Confirm sasha/penny/ingrid Consistency, No Legacy Slugs Remain** — `TestNoRetiredSlugs` gate PASS (fresh, uncached); slug grep across `README.md`/`docs/`/`skill/SKILL.md`/`cmd/atcr/` = all hits legitimate (sentinel-delimiter lines in payload-modes/cross-examination, `idiomatic Go` adjective in registry.md, Go sentinel-error/value idioms + a `performance/tracer` community-persona *test fixture* — none are stale built-in persona identifiers); rendered `--help` (24 subcommands) = 0 retired slugs; 0 bare `ian`. ZERO stale references → zero edits. AC3 satisfied.
   **Task:** Run the authoritative `TestNoRetiredSlugs` gate, then a targeted prose sweep for stale `sentinel`/`tracer`/`idiomatic` persona references, replacing confirmed hits with `sasha`/`penny`/`ingrid` (never `ian`).
   **Priority:** P2 | **Effort:** S
   1. Run `go test ./personas/... ./internal/personas/...`; confirm `TestNoRetiredSlugs` passes.
   2. Grep `README.md`, `docs/`, `skill/SKILL.md`, `cmd/atcr/root.go` for `sentinel`, `tracer`, `idiomatic` (case-insensitive).
   3. Classify every hit against the false-positive table (`documentation/persona-naming-doc-accuracy.md`).
   4. Replace confirmed stale references: `sentinel`→`sasha`, `tracer`→`penny`, `idiomatic`→`ingrid` (never `ian`).
   5. Cross-check naming consistency against `personas/_base.md` and persona docs.
   6. Run `go run ./cmd/atcr --help` and grep rendered output for retired slugs (catches template drift source-only grep would miss).
   7. Re-run `go test ./personas/... ./internal/personas/...` after edits.
   **Success Criteria:** (see [task-05](plan/tasks/task-05-persona-reference-verification.md))
   **Files:** `README.md`, `docs/personas-authoring.md`, `docs/personas-install.md`, `skill/SKILL.md`, `cmd/atcr/root.go` (verify/fix only) | **Duration:** 0.5 day

### 3.2.A [x] **Adversarial Review (subagent): Task 5** — ✅ Passed (0 findings)
   **Changed Files:** none (Task 5 made zero edits — pure verification)

   **Subagent findings (fresh context, independently re-ran gate + grep + rendered help, 2026-07-22):** `go test ./personas/... ./internal/personas/...` PASS (TestNoRetiredSlugs green). Built-ins = `bruce, dax, greta, ingrid, kai, mira, otto, penny, sasha` — no sentinel/tracer/idiomatic files; sasha/penny/ingrid consistent across `docs/personas-install.md` + `docs/personas-authoring.md`. Rendered CLI help (every subcommand) = zero retired-slug hits. `grep -rInw 'ian'` = zero fabricated persona. Every grep hit independently classified as a legitimate false-positive (sentinel-delimiter lines, "idiomatic Go" adjective, Go sentinel-error/value idioms, `performance/tracer` community-persona mock fixture). No legitimate idiom wrongly flagged; no genuine stale identifier wrongly dismissed.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | _(none)_ | | | |

   **Result:** ✅ Adversarial review passed. AC3 independently confirmed. Proceed.

### 3.3 [x] **🔧 Task 07: Technical Debt Capture — Shard MEDIUM/LOW Findings into `.planning/technical-debt/README.md`** — 23 rows (20 MEDIUM + 3 LOW) sharded into new `### [2026-07-22] From Review: 33.0_final_documentation_sweep` section (hand-formatted byte-consistent with the 32.4 11-column section; `group_td` unusable — dropped all cells + emitted outdated 8-col "From Sprint" shape). `(symbolName)` anchors preserved verbatim, none fabricated. Stats regenerated via `td_stats` (Total 116→139, Open 1→24). `td_validate`: 24/24 open items valid (0 file_missing/symbol errors). Row-count reconciled 23==23.
   **Task:** Read Task 3's `.planning/sprints/active/33.0_final_documentation_sweep/code-review/triaged-findings-medium-low.md` handoff artifact and shard it into `.planning/technical-debt/README.md`'s existing dated-section format.
   **Priority:** P1 | **Effort:** S
   1. Read Task 3's handoff artifact (9-column format + GROUP label).
   2. Map fields onto the TD README's table columns; `Status` starts `[ ]` for every new row; preserve any pre-existing `(symbolName)` anchor verbatim.
   3. Append a new dated section `### [2026-07-22] From Review: 33.0_final_documentation_sweep` at the top of the dated-sections list.
   4. Update the Stats table and summary counts (Open/Deferred/Resolved, Last Modified, Total Items).
   5. Validate via `llm_support_td_validate` (or manual diff against `docs/technical-debt-format.md`).
   6. Row-count reconciliation: new-section row count must equal Task 3 artifact finding count.
   **Success Criteria:** (see [task-07](plan/tasks/task-07-technical-debt-capture.md))
   **Files:** `.planning/technical-debt/README.md` | **Duration:** 0.5 day

### 3.3.A [x] **Adversarial Review (subagent): Task 7** — ✅ Passed (0 findings)
   **Changed Files:** `.planning/technical-debt/README.md`

   **Subagent findings (fresh context, independently counted + field-diffed all rows, 2026-07-22):** Source handoff = 23 findings (20 MEDIUM + 3 LOW); new README section = 23 rows (20 MEDIUM + 3 LOW), every source finding maps 1:1 (no drops/dupes). FORMAT: header + delimiter byte-identical to adjacent 32.4 section; no stray `|` in any cell. ANCHORS: all 18 rows with `(...)` anchors preserved exactly, 5 rows correctly have none — no fabrication or stripping. STATS: MEDIUM open=20, LOW open=4 (3 new + 1 pre-existing), Open Items=24, all internally consistent. Every new row open `[ ]`, Source=literal `code-review`, EVIDENCE column correctly dropped, Reviewers/Confidence/Est match source. Group sharding consistent (cmd/atcr→1, personas→2, tools→3, reconcile→4, singletons→U).
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | _(none)_ | | | |

   **Result:** ✅ Adversarial review passed. Proceed.

### 3.4 [x] **Phase 3 DoD Check**
   1. ✅ `README.md` (5 fixes), `skill/SKILL.md` (3 fixes) verified accurate against finalized `--help`; `docs/README.md` index complete (28 links, 0 orphans); persona docs accurate. SKILL.md `report` row byte-matches root `--help`.
   2. ✅ `go test ./personas/... ./internal/personas/...` passes (fresh); zero stale persona-slug references (all grep hits legitimate false-positives); rendered help 0 retired slugs.
   3. ✅ `.planning/technical-debt/README.md` updated — 23 rows sharded, `td_validate` 24/24 valid, row-count reconciliation 23==23 confirmed, stats regenerated (Total 116→139).
   **Report:**
   ```
   Phase-3 DoD Complete
   Auto: 5/5 (build/vet/golangci-lint[0 issues]/persona-gate green; coverage+full-docs N/A — docs-only phase, zero production .go changed) | Task-Specific: 3/3
   Manual Review: [ ] Code reviewed  (→ /execute-code-review)
   ```

### 3.5 [x] **Phase 3 - GATE: Integration & Exit Review (subagent)** — ✅ Phase gate passed (0 findings)

   **Gate result (fresh hostile-integrator subagent, 2026-07-22):** CONTRACT EXIT ✅ (README.md + skill/SKILL.md edits coherent & non-overlapping; Task 5 zero edits — no clobbering). CONFIG SURFACE ✅ (`telemetry`+`quality_signal` = exact allowlist in `cmd/atcr/config.go:81`; described consistently across README/SKILL/`config set --help`; SKILL correctly dropped "Read/" — mutation-only). INTEGRATION ✅ (Phase 3 touched only README.md, skill/SKILL.md, TD README; `docs/` untouched → stable for Phase 4 Task 6; all README doc links resolve). PHASE-EXIT CONTRACT ✅ (new 33.0 TD section = 23 rows/20 MED+3 LOW, byte-matches handoff FILE:LINE set, no drops/dupes, no header collision with same-date 32.4; Stats Open 24/Deferred 80/Resolved 35/Total 139 all verified). REGRESSION ✅ (`go test ./personas/... ./internal/personas/...` green; `git diff main...HEAD -- '*.go'` empty — zero production `.go` modified; binary builds; help confirms `report ...|sarif|axi`, `quickstart --offline`, `quality-report`). No CRITICAL/HIGH/MEDIUM/LOW.

   **Scope:** All files changed during Phase 3 (`README.md`, `skill/SKILL.md`, `.planning/technical-debt/README.md`)

   **Spawn a fresh subagent** via the Agent tool. No memory of Phase 3's implementation.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Are Tasks 4/5's fixes to the same files (e.g. `README.md`, `skill/SKILL.md`) non-conflicting?
       - CONFIG SURFACE: No new config keys documented incompletely?
       - INTEGRATION: Does Task 6 (Phase 4) have a stable, finalized `docs/` state to validate against?
       - PHASE-EXIT CONTRACT: Is the TD README (Task 7) internally consistent with Task 3's MEDIUM/LOW handoff?
       - REGRESSION: `go test ./personas/... ./internal/personas/...` still green?
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

## Phase 4: Integration — Website Compatibility Check

**Duration:** 1 day | **Items:** Task 6

### 4.1 [x] **🔧 Task 06: Website Compatibility Check — Validate `docs/` for Clean, Self-Contained `atcr.dev` Import** — 29 files validated; 6 broken relative links fixed (archived sprint 19.7 `active/`→`completed/`: `personas-install.md` ×4, `personas-authoring.md` ×2); zero leading-`/` repo-root paths; all `../` cross-repo links intentional and resolving; code fences balanced + heading hierarchy clean (0 skips); index complete both directions (28=28, 0 orphans, 0 dups); two `http://localhost` config examples untouched. AC5 satisfied.
   **Task:** Validate all 29 `docs/` files are link-correct, self-contained, and cleanly formatted for `atcr.dev` import, run only after Tasks 4/5 have landed.
   **Priority:** P2 | **Effort:** S
   1. Confirm Task 4 and Task 5 are merged before starting.
   2. Enumerate `docs/*.md` file set; cross-check against `docs/README.md`'s index for orphans.
   3. Extract every Markdown link from all 29 files; classify in-docs / cross-repo / external.
   4. Fix any broken relative link found.
   5. Grep for repo-root-relative (leading `/`) assumptions; correct or reword.
   6. Spot-check Markdown formatting (heading hierarchy, code fences, tables/lists); leave the two known legitimate `http://localhost` examples untouched.
   7. Confirm `docs/README.md`'s categorized index still groups every file sensibly.
   8. Re-run the link-resolution sweep once more to confirm zero remaining broken links.
   **Success Criteria:** (see [task-06](plan/tasks/task-06-website-compatibility-check.md))
   **Files:** `docs/README.md`, `docs/*.md` (remaining 28) | **Duration:** 1 day

### 4.1.A [x] **Adversarial Review (subagent): Task 6** — ✅ Passed (no CRITICAL/HIGH); 1 MEDIUM self-containment finding routed to TD-004
   **Changed Files:** `docs/personas-install.md`, `docs/personas-authoring.md`

   **Subagent findings (fresh context, independently re-ran full link sweep of all 29 docs + `git diff`, 2026-07-22):** LINK INTEGRITY ✅ (209 links / 177 relative-file links, 0 broken, 0 leading-slash). SCOPE ✅ (`git diff` shows ONLY the 6 `active/`→`completed/` path fixes — zero content/persona-naming bleed). FALSE FLAG ✅ (both `http://localhost` examples at `personas-install.md:331`, `providers.md:21` untouched). Raised MEDIUM + LOW that ~30 `../`-escaping links (the 6 `.planning/` refs + ~24 to `README/skill/examples/internal/.github/CHANGELOG/reconcile`) won't survive a standalone `atcr.dev` docs-only import.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | personas-install.md:225,231; personas-authoring.md:240 | 6 fixed links resolve on disk but point via `../` into internal `.planning/` planning artifacts — dead once `docs/` imported standalone into `atcr.dev`; reference non-public material | Website-import step rewrites/prunes; on-disk-in-atcr correct → **TD-004** |
   | LOW | release-process.md, registry.md, skill-usage.md, external-migration.md, scorecard.md, logging.md, ci-integration.md, hermes-maintenance-agents.md, payload-modes.md | ~24 further `../` links escape docs/ to repo internals; resolve in atcr, break under standalone import; pre-existing | Website-import owner confirms link rewrite/prune → **TD-004** |

   **Disposition:** No CRITICAL/HIGH. The `../` cross-repo links are the category Task 06 explicitly classifies as INTENTIONAL (task-06 step 3b + Risk Mitigation; `docs/README.md` states the website build consumes the index and handles these separately) — so the LOW is accepted-by-design, and the MEDIUM's remediation (rewriting/pruning links) is a content/website-build decision out of Task 06's link-integrity scope. Both consolidated into **TD-004** (website-import self-containment) per the 4.1.A MEDIUM/LOW→TD rule, rather than an out-of-scope content edit. Proceed.

   **Spawn a fresh subagent** via the Agent tool. No memory of Task 6's implementation.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: Task 6`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST]
     - Checklist (pass verbatim):
       - LINK INTEGRITY: Any relative link still broken after the fix pass?
       - SELF-CONTAINMENT: Any repo-root-relative assumption missed?
       - SCOPE: Any content-accuracy or persona-naming edit made here that bled in from Task 4/5's scope?
       - FALSE FLAG: Were the two legitimate `http://localhost` examples (`docs/personas-install.md:331`, `docs/providers.md:21`) left untouched?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues, fix before proceeding to Phase 4 DoD
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.2 [x] **Phase 4 DoD Check**
   1. ✅ All 29 `docs/` files indexed with zero orphans in either direction (28 indexed = 28 on disk excluding the `README.md` index itself; 0 dead index links, 0 duplicates).
   2. ✅ Zero broken relative links across all 29 files (independent re-sweep after fix; 6 archived-sprint links repaired).
   3. ✅ Zero repo-root-relative (leading `/`) link paths remain; the two known `http://localhost` config examples untouched.
   **Report:**
   ```
   Phase-4 DoD Complete
   Auto: 5/5 (test/vet/lint/build green from Phase 3 — zero production .go changed this phase; coverage N/A — docs-only) | Task-Specific: 3/3
   Manual Review: [ ] Code reviewed  (→ /execute-code-review)
   ```

### 4.3 [x] **Phase 4 - GATE: Integration & Exit Review (subagent)** — ✅ Phase gate passed (0 findings)

   **Gate result (fresh hostile-integrator subagent, 2026-07-22):** CONTRACT EXIT ✅ (all in-`docs/` relative links resolve; the only external dependency is the accepted `../` cross-repo category, documented + captured in TD-004 — nothing outside that category left uncaptured). CONFIG SURFACE ✅ (N/A). INTEGRATION ✅ (`docs/` is in a stable finalized state for Phase 5 AC5 validation). PHASE-EXIT CONTRACT ✅ (all 29 files reachable, zero orphans either direction). REGRESSION ✅ (`git diff` shows only `active/`→`completed/` path-segment changes — zero content/persona-naming regression). No CRITICAL/HIGH/MEDIUM/LOW.

   **Scope:** All files changed during Phase 4 (`docs/personas-install.md`, `docs/personas-authoring.md`, `sprint-plan.md`, `tech-debt-captured.md`)

   **Spawn a fresh subagent** via the Agent tool. No memory of Phase 4's implementation.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `docs/` is genuinely self-contained enough for a standalone `atcr.dev` import (no repo-context dependency remains)?
       - CONFIG SURFACE: N/A
       - INTEGRATION: Does Task 8 (Phase 5) have a stable final `docs/` state to validate AC5 against?
       - PHASE-EXIT CONTRACT: All 29 files reachable and none orphaned?
       - REGRESSION: No content-accuracy or persona-naming regression introduced by link fixes?
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

## Phase 5: Validation — Final Verification Pass

**Duration:** 1 day | **Items:** Task 8

### 5.1 [ ] **🔧 Task 08: Final Verification Pass — Re-run Automated Guards End-to-End (Plan Definition-of-Done Gate)**
   **Task:** Re-run every automated guard fresh against the final Tasks 1-7 state, then explicitly walk AC1-AC5.
   **Priority:** P1 | **Effort:** S
   1. Confirm the working tree reflects the final state of Tasks 1-7.
   2. `go test ./personas/... ./internal/personas/...` (AC3 guard).
   3. `go vet ./...`.
   4. `golangci-lint run` (install if not present locally — never silently skip).
   5. `go test -race ./...` (full root-module regression).
   6. `(cd reconcile && go test ./...)` (separate `go.mod`, not covered by root).
   7. Any guard failure -> stop, record command/output/location as a blocking finding; do NOT patch inline.
   8. Walk AC1-AC5 explicitly against current repo/docs/TD-README state, citing evidence source for each.
   9. Write a guard-by-guard and AC-by-AC pass/fail summary.
   **Success Criteria:** (see [task-08](plan/tasks/task-08-final-verification-pass.md))
   **Files:** None (verification-only; any regression found is a new finding, not a fix) | **Duration:** 0.5 day

### 5.1.A [ ] **Adversarial Review (subagent): Task 8**
   **Changed Files:** None (verification-only task — review the verification record itself)

   **Spawn a fresh subagent** via the Agent tool. No memory of Task 8's implementation.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: Task 8`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): Task 8's pass/fail summary record, plus a fresh independent re-run of `go test -race ./...` and `(cd reconcile && go test ./...)` if feasible
     - Checklist (pass verbatim):
       - FRESHNESS: Was each guard actually re-run fresh, or was a prior task's self-report trusted instead?
       - AC EVIDENCE: Does every AC1-AC5 claim cite a concrete evidence source (task record, file, command output) rather than an assumption?
       - SILENT SKIP: Was `golangci-lint` actually run, or silently skipped because it wasn't installed?
       - SUBMODULE COVERAGE: Was `reconcile/`'s separate `go.mod` test suite actually run, not just the root suite?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues, fix (re-run the missing/skipped guard) before proceeding to Phase 5 DoD
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 5.2 [ ] **Phase 5 DoD Check**
   1. All 5 guards (persona/AC3, `go vet`, `golangci-lint`, `go test -race ./...`, `reconcile` submodule) pass with zero failures.
   2. AC1-AC5 each explicitly confirmed with a cited evidence source.
   3. Guard-by-guard and AC-by-AC pass/fail summary recorded.
   **Report:**
   ```
   Phase-5 DoD Complete
   Auto: {X}/5 | Task-Specific: {Y}/3
   Manual Review: [ ] Code reviewed
   ```

### 5.3 [ ] **Phase 5 - GATE: Integration & Exit Review (subagent)**
   **Scope:** The entire sprint's cumulative diff (Tasks 1-8)

   **Spawn a fresh subagent** via the Agent tool. No memory of the sprint's implementation.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 5 gate review`
   - prompt: Self-contained brief including:
     - Files changed across the entire sprint (absolute paths): [LIST — full cumulative diff]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All 5 acceptance criteria (AC1-AC5) genuinely satisfied with cited evidence, not assumed?
       - CONFIG SURFACE: No new config keys left undocumented anywhere in the sprint's changes?
       - INTEGRATION: No cross-task regression slipped through (e.g., a Task 3 fix breaking a Task 5/8 guard)?
       - PHASE-EXIT CONTRACT: Is the repository genuinely ready for Epic 33.1/33.2 to proceed against it?
       - REGRESSION: Full guard suite (Task 8) green as the closing state?
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

## Final Phase: Validation

### Validation Checklist
- [ ] All tests passing (T3): `go test -race ./...`, `(cd reconcile && go test ./...)`
- [ ] Coverage meets threshold: `go test -coverprofile=coverage.out ./...` (≥80%)
- [ ] Lint/format clean: `golangci-lint run`, `go vet ./...`
- [ ] Build succeeds

### Optional: Targeted Mutation Testing
No mutation testing tool detected in this environment (`stryker-mutator` not in `package.json`; neither `mutmut` nor `cargo-mutants` available on PATH). Skip this step. **WARNING:** Do NOT run full codebase mutation - it can take hours. If a mutation tool becomes available later, target only the specific files fixed in Task 3 (CRITICAL/HIGH remediations), never the full codebase.

### Drift Analysis
Compare the final repository state against `plan/original-requirements.md`:
- AC1-AC5 must each trace to a specific task's Definition of Done (Task 1/2/3 → AC1/AC2; Task 5 → AC3; Task 4 → AC4; Task 6 → AC5).
- Task 8's Phase 5 pass/fail summary is the authoritative drift-analysis evidence — if any AC lacks a cited evidence source there, treat the sprint as incomplete rather than marking it done.
- No task in this sprint should introduce scope beyond the original request (review + fix CRITICAL/HIGH, route MEDIUM/LOW to TD, then audit docs) — flag any drift found for user confirmation before proceeding.
