# Sprint 20.0: Standalone Skill Release

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 20.0 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting — EXCEPT at each phase-boundary GATE task, where gated mode stops for review.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

A single dispatcher skill (`/atcr <command>`) that lets anyone run atcr multi-agent code reviews on any repository without the private `.planning/` workflow, plus the supporting install script and documentation. `skill/SKILL.md` is rewritten from a linear orchestration script into a lightweight router that maps user intent to `atcr` CLI commands and loads deep instructions from on-demand secondary markdown files.

### Why This Matters

The atcr engine is more robust than the legacy review system, but its public UX is fragmented and its private `.planning/` backward-compatibility contract is only informally protected. This sprint gives the OSS product a clean, discoverable entrypoint while locking the private-skill backend contract behind a repo-local regression test.

### Key Deliverables

- Dispatcher rewrite of `skill/SKILL.md` (router + on-demand secondary files: `host-review.md`, `ambiguity-adjudication.md`, `findings-format.md`) within frontmatter/line budgets.
- A repo-local backward-compatibility test locking the documented `--output-dir` + `atcr reconcile` output-tree contract.
- `install.sh` thin wrapper over `go install`, documented in the README Quickstart.
- Documentation accuracy pass across `docs/skill-usage.md`, `docs/code-review-backend.md`, `README.md`.
- `docs/external-migration.md` recording the `claude-prompts` migration as a manual operator follow-up.

### Success Criteria

- Dispatcher routing table matches every entry in `newRootCmd` (`cmd/atcr/main.go:185-208`) with zero invented/drifted command names.
- The documented backend output tree is asserted by an automated hermetic test (no real network, no shell interpolation).
- `install.sh` succeeds with a Go toolchain present, fails clearly without it, warns (non-fatal) on PATH gaps.
- All 17 acceptance criteria closed; coverage baseline 80% held; no writes to any external repository.

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** Moderate 🔄 (auto-selected from complexity 8/12). Each story follows RED → GREEN → ADVERSARIAL REVIEW → REFACTOR.

**Adversarial Review:** ENABLED 🎯 — a fresh subagent (no memory of the implementation) reviews each story's changed files and returns a findings table. Inline-fix bar: **CRITICAL/HIGH** (fixed in the story's REFACTOR task before proceeding). Deferred: **MEDIUM/LOW** (appended to `clarifications/tech-debt-captured.md`).

**Execution Mode:** Gated 🚧 — `/execute-sprint` stops at each phase-boundary GATE (`N.LAST`) for review instead of running continuously.

**Story 3 test-mechanism decision (resolved at /create-sprint):** The install-script Integration ACs (AC03-01/03-02) are covered by a Go test at `cmd/atcr/install_script_test.go` that shells out to `install.sh` via `os/exec` with a temp `GOPATH`/`PATH`, gated behind `//go:build integration`. This matches the repo's existing subprocess-testing convention (`cmd/atcr/*_test.go`, `newRootCmd()`/`ExecuteContext`) rather than introducing a net-new shell-test harness or leaving the ACs manual-only.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [user-stories/](plan/user-stories/) | Feature requirements |
| [acceptance-criteria/](plan/acceptance-criteria/) | Validation requirements with DoD |
| [documentation/](plan/documentation/) | Grounded package + design docs for this plan |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./cmd/atcr/ -run <TestName>` / `go test ./skill/ -run <TestName>` |
| T2: Module | After completing an element | `go test ./cmd/atcr/...` / `go test ./skill/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` (+ `go test -tags=integration ./...` where applicable) |

### DoD Verification Checklist

1. Tests (T3): All passing
2. Coverage: ≥80%
3. Lint: `golangci-lint run` + `go vet ./...` clean
4. Build: `go build ./...` succeeds
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

**Implementation (black-box, replaceable modules):** `skill/SKILL.md` invokes the `atcr` binary only as a subprocess — it must never reach into engine internals. Secondary skill markdown files load on demand and evolve independently of the routing table. `install.sh` only wraps `go install`; it must not duplicate `atcr doctor`'s self-test role. The contract test treats `internal/fanout/reviewdir.go` as read-only and asserts only the externally-documented contract surface.

**Coding (Go):** Packages lowercase; exported `PascalCase`, unexported `camelCase`; errors returned last and wrapped with `fmt.Errorf("...: %w", err)`; `context.Context` first param for I/O; table-driven tests with `t.Run` subtests; `testing` + `testify` (`assert` for multi-check output-tree assertions, `require` for fail-fast setup); integration tests behind `//go:build integration`; files snake_case.

**Git:** GitHub Flow. Branch `feature/20.0_standalone_skill_release` off `main`. Conventional Commits (`type(scope): description`). Small atomic commits. Squash-and-merge via PR; CI (fmt, vet, lint, tests) must pass.

---

## External Resources

From [plan/documentation/](plan/documentation/):

- **[CRITICAL]** `documentation/cli-dispatcher-conventions.md` — existing cobra command/subcommand structure the dispatcher must mirror.
- **[CRITICAL]** `documentation/agent-skill-format.md` — SKILL.md frontmatter, three-level loading model, secondary-file conventions for the ≤~500-line rewrite.
- **[IMPORTANT]** `documentation/backward-compat-test-patterns.md` — Go/testify conventions + reconcile id-or-path resolution contract for the AC3 test.
- **[IMPORTANT]** `documentation/install-script-conventions.md` — requirements/style for the net-new `install.sh`.
- **[REFERENCE]** `documentation/external-migration-descope.md` — why the `claude-prompts` migration is a manual operator follow-up.

**Ground-truth command surface:** `cmd/atcr/main.go:185-208` (`newRootCmd`) — sole source of truth for routed command names (NOT the stale `cobra.md` snapshot).

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation — Dispatcher Rewrite

**Goal:** Rewrite `skill/SKILL.md` into a router + on-demand secondary-file model. Highest-risk item, sequenced first (Stories 4 & 5 depend on its output). Story 1 (Effort: L). ACs 01-01…01-05.

### 1.1 [ ] **[Dispatcher Skill Rewrite - RED](plan/user-stories/01-dispatcher-skill-rewrite.md)**
   **Mode:** Moderate | **AC:** [01-01](plan/acceptance-criteria/01-01-dispatcher-command-routing-table.md) · [01-02](plan/acceptance-criteria/01-02-review-flow-preserved-through-dispatcher.md) · [01-03](plan/acceptance-criteria/01-03-secondary-files-verbatim-split.md) · [01-04](plan/acceptance-criteria/01-04-frontmatter-and-line-budget-constraints.md)
   1. Analyze ACs, identify testable units against the embedded `SkillMD` string in `skill/skill_test.go`.
   2. Extend `skill/skill_test.go` with failing assertions for: (a) routing table lists exactly the command names registered in `newRootCmd` (`cmd/atcr/main.go:185-208`), zero invented/drifted names (AC01-01); (b) the review→reconcile→report path remains reachable as one routable command path (AC01-02); (c) secondary files `skill/host-review.md`, `skill/ambiguity-adjudication.md`, `skill/findings-format.md` exist and contain the byte-for-byte-moved sections (AC01-03); (d) frontmatter `name` ≤64 chars (lowercase/numbers/hyphens, no reserved words), `description` ≤1024 chars, SKILL.md body ≤~500 lines (AC01-04).
   3. Verify all new assertions FAIL for the right reason (missing files / stale linear-script content), not compile errors.
   **Files:** `skill/skill_test.go` | **Duration:** ~0.5 day

### 1.2 [ ] **[Dispatcher Skill Rewrite - GREEN](plan/user-stories/01-dispatcher-skill-rewrite.md)**
   Perform the minimal rewrite/relocation to pass 1.1:
   1. Move Host Review Instructions → `skill/host-review.md`, Ambiguity Adjudication → `skill/ambiguity-adjudication.md`, Findings Format Reference → `skill/findings-format.md` (byte-for-byte verbatim).
   2. Rewrite `skill/SKILL.md` body into a dispatcher routing table mapping user intent → registered `atcr` commands, with on-demand references to the three secondary files.
   3. Keep frontmatter within budget; keep body ≤~500 lines. Run T1 after each change, verify all pass (T2: `go test ./skill/...`).
   4. COMMIT: `git add skill/SKILL.md skill/host-review.md skill/ambiguity-adjudication.md skill/findings-format.md skill/skill_test.go && git commit -m "feat(skill): rewrite SKILL.md as /atcr dispatcher with secondary files (green)"`
   **Files:** `skill/SKILL.md`, `skill/host-review.md`, `skill/ambiguity-adjudication.md`, `skill/findings-format.md` | **Duration:** ~1.5 days

### 1.2.A [ ] **[Dispatcher Skill Rewrite - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-dispatcher-skill-rewrite.md)**
   **Changed Files:** `skill/SKILL.md`, `skill/host-review.md`, `skill/ambiguity-adjudication.md`, `skill/findings-format.md`, `skill/skill_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): all Changed Files above under `/Users/samestrin/Documents/GitHub/atcr/`
     - Checklist (pass verbatim):
       - ROUTING FIDELITY: Does the routing table introduce any command name NOT present in `cmd/atcr/main.go:185-208`? Any registered command silently dropped, widening/narrowing the CLI surface?
       - VERBATIM SPLIT: Are the three secondary files byte-for-byte identical to the original SKILL.md sections (no paraphrase, no dropped lines)?
       - CONTRACT DRIFT: Does `docs/skill-usage.md`'s `.atcr/reviews/<id>/` claim still hold post-rewrite?
       - EDGE CASES: Unregistered/typo'd command intent, ambiguous natural-language intent — does the router route only to exact registered names and clarify rather than guess?
       - FRONTMATTER/BUDGET: `name`/`description`/body-line limits actually satisfied?
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

### 1.3 [ ] **[Dispatcher Skill Rewrite - REFACTOR](plan/user-stories/01-dispatcher-skill-rewrite.md)**
   1. Fix CRITICAL/HIGH issues from 1.2.A (if any).
   2. Tighten each per-command routing entry to one line without losing routing accuracy; improve secondary-file cross-links (T1).
   3. Validate all tests still pass (T3: `go test ./...`).
   4. COMMIT: `git add skill/ && git commit -m "refactor(skill): address review + tighten routing entries"`
   **Duration:** ~0.5 day

### 1.4 [ ] **Phase 1 DoD — Story 1**
   1. Run DoD checklist (Tests T3, Coverage ≥80% for touched code, Lint `golangci-lint run` + `go vet ./...`, Build, Docs).
   2. Manually verify AC01-05: `docs/skill-usage.md` still accurately describes install/usage and the `.atcr/reviews/<id>/` output post-rewrite (mark note; full doc pass is Phase 3 Story 4).
   3. Emit DoD Report (5 ACs: 01-01…01-05).

### 1.5 [ ] **Phase 1 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 1 (integration-level, not TDD cadence).

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 1 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Routing table is the authoritative command surface Stories 4/5 will document — is every routed name exactly a `newRootCmd` entry?
       - CONFIG SURFACE: Secondary-file references resolve; frontmatter valid for Claude Code skill loading?
       - INTEGRATION: Does the dispatcher preserve the review→reconcile→report orchestration current adopters rely on?
       - PHASE-EXIT CONTRACT: Can Phase 3 doc pass consume the new SKILL.md/secondary-file structure without rework?
       - REGRESSION: `go test ./...` green; no unrelated behavior changed?
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

## Phase 2: Independent Verification Work

**Goal:** Add a backward-compat regression lock (Story 2) and the install script (Story 3). Neither depends on Story 1 nor on each other; run in either order. Stories 2 & 3 (Effort: S each). ACs 02-01…03, 03-01…03.

### 2.1 [ ] **[Backend Contract Test - RED](plan/user-stories/02-backend-contract-backward-compatibility-test.md)**
   **Mode:** Moderate | **AC:** [02-01](plan/acceptance-criteria/02-01-output-tree-contract-assertion.md) · [02-02](plan/acceptance-criteria/02-02-id-or-path-resolution-table.md) · [02-03](plan/acceptance-criteria/02-03-hermetic-provider-and-git-fixtures.md)
   1. Create `cmd/atcr/backend_contract_test.go` (new).
   2. Write failing table-driven assertions: (a) `atcr review --output-dir` + `atcr reconcile` produce the exact documented tree — `manifest.json`, `payload/`, `sources/pool/findings.txt`, `sources/pool/summary.json`, `reconciled/findings.txt`, `reconciled/findings.json`, `reconciled/report.md`, `reconciled/summary.json` (AC02-01, prefer `assert` so all missing files surface in one run); (b) all three id-or-path resolution branches (bare id → `.atcr/reviews/<id>/`, explicit path, omitted → `.atcr/latest`) as `t.Run` subtests (AC02-02); (c) provider mocked via `httptest.NewServer` (zero real network), git fixtures via `os/exec` into `t.TempDir()` — never shell string interpolation (AC02-03).
   3. Verify assertions fail against a fresh fixture (missing wiring), not against real engine behavior.
   **Files:** `cmd/atcr/backend_contract_test.go` | **Duration:** ~0.5 day

### 2.2 [ ] **[Backend Contract Test - GREEN](plan/user-stories/02-backend-contract-backward-compatibility-test.md)**
   Wire the fixture + assertions correctly so the contract test passes in-process via `newRootCmd()`/`ExecuteContext` (matching `reconcile_test.go`'s `execCmd` pattern). Do NOT modify `internal/fanout/reviewdir.go` — assert only the documented surface. Run T1/T2, verify pass.
   COMMIT: `git add cmd/atcr/backend_contract_test.go && git commit -m "test(cmd): lock documented --output-dir + reconcile backend contract (green)"`
   **Files:** `cmd/atcr/backend_contract_test.go` (+ shared fixture helpers if extracted) | **Duration:** ~0.5 day

### 2.2.A [ ] **[Backend Contract Test - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-backend-contract-backward-compatibility-test.md)**
   **Changed Files:** `cmd/atcr/backend_contract_test.go` (+ any fixture helper files)

   **Spawn a fresh subagent** via the Agent tool. No memory of the 2.2 implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): Changed Files above under `/Users/samestrin/Documents/GitHub/atcr/`
     - Checklist (pass verbatim):
       - HERMETICITY: Any real network call (not `httptest.NewServer`)? Any write outside `t.TempDir()`? Any shell string interpolation instead of `os/exec` arg slices?
       - CONTRACT COMPLETENESS: Are ALL documented output-tree files asserted, or is the test a subset that would pass while the contract regresses?
       - REGRESSION VALUE: Does the test actually re-run the real code path, or does it re-implement/stub engine behavior (a hollow lock)?
       - EDGE CASES: Bare id / explicit path / omitted-arg branches all covered and distinct?
       - ERROR HANDLING: Does a partial run / missing file fail loudly with a useful message?
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

### 2.3 [ ] **[Backend Contract Test - REFACTOR](plan/user-stories/02-backend-contract-backward-compatibility-test.md)**
   1. Fix CRITICAL/HIGH from 2.2.A (if any).
   2. Extract shared fixture helpers if `review_test.go`/`reconcile_test.go` patterns don't already cover them (T1); keep the test fast enough for the default `go test ./cmd/atcr/...` loop.
   3. Validate T3.
   4. COMMIT: `git add cmd/atcr/ && git commit -m "refactor(cmd): address review + share backend-contract fixtures"`
   **Duration:** ~0.25 day

### 2.4 [ ] **[Install Script - RED](plan/user-stories/03-install-script.md)**
   **Mode:** Moderate | **AC:** [03-01](plan/acceptance-criteria/03-01-install-script-core-install.md) · [03-02](plan/acceptance-criteria/03-02-install-script-prereq-and-path-checks.md)
   1. Create `cmd/atcr/install_script_test.go` (new, `//go:build integration`).
   2. Write failing `os/exec`-based assertions with a temp `GOPATH`/`PATH`: (a) core install path invokes `go install github.com/samestrin/atcr/cmd/atcr@latest` and succeeds when the Go toolchain is present (AC03-01); (b) non-zero exit + clear stderr when `go` is missing or install fails; non-fatal PATH warning (exit 0) when `$(go env GOPATH)/bin` is absent from `$PATH` (AC03-02).
   3. Verify assertions fail (no `install.sh` yet).
   **Files:** `cmd/atcr/install_script_test.go` | **Duration:** ~0.25 day

### 2.5 [ ] **[Install Script - GREEN](plan/user-stories/03-install-script.md)**
   **AC:** adds [03-03](plan/acceptance-criteria/03-03-readme-quickstart-documentation.md)
   1. Write `install.sh` (repo root, thin ~40-line wrapper): shebang + strict mode; check `go` present (fatal, non-zero + stderr if missing); run `go install github.com/samestrin/atcr/cmd/atcr@latest`; warn (non-fatal) if `$(go env GOPATH)/bin` not on `$PATH`; idempotent on re-run/upgrade. NO checksum/version-pin/OS-arch detection (Epic 21.0 owns that).
   2. Update `README.md` Quickstart to document `install.sh` as a companion to (not a replacement of) the existing manual `go install` instruction (AC03-03).
   3. Run T1/T2 (`go test -tags=integration ./cmd/atcr/...`), verify pass.
   4. COMMIT: `git add install.sh README.md cmd/atcr/install_script_test.go && git commit -m "feat(install): add install.sh go-install wrapper + README quickstart (green)"`
   **Files:** `install.sh`, `README.md` | **Duration:** ~0.5 day

### 2.5.A [ ] **[Install Script - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-install-script.md)**
   **Changed Files:** `install.sh`, `README.md`, `cmd/atcr/install_script_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of the 2.5 implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): Changed Files above under `/Users/samestrin/Documents/GitHub/atcr/`
     - Checklist (pass verbatim):
       - SUPPLY CHAIN: Does `install.sh` do ANYTHING beyond the documented `go install` step (remote fetch, eval, curl|bash of other content)? Is it minimal and auditable (~40 lines)?
       - SCOPE CREEP: Any checksum verification, version pinning, OS/arch detection, or release-automation logic that belongs to Epic 21.0?
       - ERROR HANDLING: Fatal (non-zero + stderr) for missing Go / failed install vs. non-fatal PATH warning (exit 0) — correctly distinguished? Idempotent on upgrade?
       - EDGE CASES: Go absent; proxy/network failure; GOPATH/bin already on PATH; already-installed binary.
       - DOC ACCURACY: Does README present `install.sh` as a companion, not a replacement, with no functional change to the existing Quickstart?
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

### 2.6 [ ] **[Install Script - REFACTOR](plan/user-stories/03-install-script.md)**
   1. Fix CRITICAL/HIGH from 2.5.A (if any).
   2. Tighten `install.sh` messaging/exit codes; align style with `examples/ci-gate.sh` conventions (T1).
   3. Validate T3 (unit + `-tags=integration`).
   4. COMMIT: `git add install.sh cmd/atcr/ README.md && git commit -m "refactor(install): address review + tighten script messaging"`
   **Duration:** ~0.25 day

### 2.7 [ ] **Phase 2 DoD — Stories 2 & 3**
   1. Run DoD checklist (Tests T3 incl. `-tags=integration`, Coverage ≥80% touched, Lint + vet, Build, Docs).
   2. Emit DoD Report (Story 2: 3 ACs 02-01…03; Story 3: 3 ACs 03-01…03).

### 2.8 [ ] **Phase 2 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2.

   **Spawn a fresh subagent** via the Agent tool (no memory of the phase). Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 2 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Does the backend-contract test assert the exact tree Story 4's doc pass will verify `docs/code-review-backend.md` against?
       - CONFIG SURFACE: `install.sh` documented, defaulted, back-compat with existing `go install` guidance?
       - INTEGRATION: Contract test hermetic under CI; integration-tagged install test not run in the default loop by accident?
       - PHASE-EXIT CONTRACT: Can Story 4 (docs) rely on both the test assertions and the README changes without rework?
       - REGRESSION: Full suite green; nothing outside `cmd/atcr/`, `install.sh`, `README.md` touched?
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

---

## Phase 3: Documentation & Migration Follow-Through

**Goal:** Diff-style documentation accuracy pass (Story 4, depends on Stories 1 & 3) and the external-migration descope note (Story 5, depends on Story 1). All ACs are Manual/verification — only lines demonstrably wrong get touched; no doc restructuring. Stories 4 & 5 (Effort: S each). ACs 04-01…03, 05-01…03.

### 3.1 [ ] **[Documentation Accuracy Pass - VERIFY & CORRECT](plan/user-stories/04-documentation-accuracy-pass.md)**
   **Mode:** Moderate (verification-driven; no automated RED — all ACs Manual) | **AC:** [04-01](plan/acceptance-criteria/04-01-skill-usage-doc-accuracy.md) · [04-02](plan/acceptance-criteria/04-02-code-review-backend-doc-accuracy.md) · [04-03](plan/acceptance-criteria/04-03-readme-command-accuracy-and-install-link.md)
   1. Verify `docs/skill-usage.md` command examples/paths against the post-rewrite `skill/SKILL.md`; correct only confirmed inaccuracies (AC04-01).
   2. Verify `docs/code-review-backend.md` "Output tree" / "Behavioral notes for callers" against current `--output-dir` behavior AND Story 2's test assertions; correct only confirmed drift (AC04-02).
   3. Verify `README.md` command table + Documentation section; cross-link to `install.sh`; no functional change to Quickstart (AC04-03).
   4. COMMIT: `git add docs/skill-usage.md docs/code-review-backend.md README.md && git commit -m "docs: accuracy pass for skill-usage, backend contract, README post-dispatcher"`
   **Files:** `docs/skill-usage.md`, `docs/code-review-backend.md`, `README.md` | **Duration:** ~0.5 day

### 3.2 [ ] **[External Migration Descope Note - AUTHOR](plan/user-stories/05-external-migration-descope-note.md)**
   **AC:** [05-01](plan/acceptance-criteria/05-01-external-migration-doc-existence-and-rationale.md) · [05-02](plan/acceptance-criteria/05-02-manual-migration-checklist-and-discoverability.md) · [05-03](plan/acceptance-criteria/05-03-scope-containment-no-external-repo-writes.md)
   1. Create `docs/external-migration.md`: state the workspace write-access boundary (agent operates only in the atcr repo), cite Epic 12.0 (AC05-01).
   2. Add a numbered checklist: replace fragmented private skills with the dispatcher, copy/adapt the `skill/SKILL.md` template, preserve `.planning/` hooks, validate against `docs/code-review-backend.md`; make it discoverable via a cross-link from README/docs (AC05-02).
   3. SCOPE GUARD: make NO writes or staged commits to `~/Documents/GitHub/claude-prompts/` — this note is the deliverable, the migration itself is a manual operator action (AC05-03).
   4. COMMIT: `git add docs/external-migration.md README.md && git commit -m "docs: add external-migration descope note + cross-link"`
   **Files:** `docs/external-migration.md`, `README.md` (cross-link only) | **Duration:** ~0.25 day

### 3.3 [ ] **[Documentation & Migration - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-documentation-accuracy-pass.md)**
   **Changed Files:** `docs/skill-usage.md`, `docs/code-review-backend.md`, `docs/external-migration.md`, `README.md`

   **Spawn a fresh subagent** via the Agent tool (no memory of the doc edits). Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.x docs`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): Changed Files above under `/Users/samestrin/Documents/GitHub/atcr/`, plus ground-truth `skill/SKILL.md`, `cmd/atcr/main.go`, `cmd/atcr/backend_contract_test.go`
     - Checklist (pass verbatim):
       - DOC↔CODE FIDELITY: Does every command/path/output-tree claim in the docs match the post-rewrite skill, `newRootCmd`, and the contract test? Any remaining stale reference?
       - OVER-EDIT: Any doc restructuring or new authoring beyond correcting confirmed inaccuracies (scope violation)?
       - QUICKSTART INTEGRITY: Is the README `go install` Quickstart functionally unchanged, with `install.sh` presented as a companion?
       - SCOPE CONTAINMENT: Any evidence of writes/staged changes to the external `claude-prompts` repo? (Must be none.)
       - DISCOVERABILITY: Is `docs/external-migration.md` reachable via cross-link, and does it correctly cite Epic 12.0 and the write-access boundary?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix in this phase before DoD, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.4 [ ] **Phase 3 DoD — Stories 4 & 5**
   1. Run DoD checklist (Docs updated; Tests T3 still green; Lint/Build unaffected).
   2. Confirm `git status` shows NO changes outside the atcr repo (AC05-03 scope guard).
   3. Emit DoD Report (Story 4: 3 ACs 04-01…03; Story 5: 3 ACs 05-01…03).

### 3.5 [ ] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3.

   **Spawn a fresh subagent** via the Agent tool (no memory of the phase). Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Docs now agree with code/tests on every command and output path?
       - CONFIG SURFACE: Cross-links resolve; no dangling references introduced?
       - INTEGRATION: `docs/code-review-backend.md` still matches what the Phase 2 contract test asserts?
       - PHASE-EXIT CONTRACT: Nothing left for Phase 4 integration to correct in docs?
       - REGRESSION: No code behavior changed by a doc-only phase; suite still green.
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

---

## Phase 4: Integration & Cross-Verification

**Goal:** End-to-end trace of the full review→reconcile→report flow through the new dispatcher; cross-check every routed command name against ground truth; run the full suite + linters.

### 4.1 [ ] **Integration & Cross-Verification**
   1. Trace the full review→reconcile→report flow as invoked through the new `skill/SKILL.md` dispatcher (each routed command resolves to a real `atcr` subcommand).
   2. Cross-check every routed command name in `skill/SKILL.md` against `cmd/atcr/main.go:185-208` (`newRootCmd`) — zero drift.
   3. Run the full suite `go test ./...` plus `go test -tags=integration ./...`, `golangci-lint run`, `go vet ./...`, `go build ./...`.
   4. Record results; fix any integration-level breakage found.
   **Files:** (verification; fixes only if needed) | **Duration:** ~0.75 day

### 4.2 [ ] **Phase 4 DoD**
   1. Full DoD checklist across the whole sprint's changes.
   2. Confirm all automated ACs (01-01…04, 02-01…03, 03-01…03) pass; Manual ACs noted for Final Phase closure.

### 4.3 [ ] **Phase 4 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed across the sprint (integration-level).

   **Spawn a fresh subagent** via the Agent tool (no memory). Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed across the sprint (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Dispatcher command surface == `newRootCmd`; backend contract test green and complete.
       - CONFIG SURFACE: `install.sh` + docs coherent as a shipped set.
       - INTEGRATION: End-to-end review→reconcile→report reachable via the dispatcher; no hidden coupling introduced.
       - PHASE-EXIT CONTRACT: Only Validation (DoD/drift) remains — nothing functional outstanding.
       - REGRESSION: `go test ./...` + integration tag + lint + vet all green.
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

---

## Final Phase: Validation

### Validation Checklist
- [ ] All tests passing (T3): `go test ./...` and `go test -tags=integration ./...`
- [ ] Coverage meets threshold (≥80% baseline): `go test -coverprofile=coverage.out ./...`
- [ ] Lint/format clean: `golangci-lint run`, `go vet ./...`, `go fmt ./...` (no diff)
- [ ] Build succeeds: `go build ./...`
- [ ] Frontmatter/line-budget constraints held (SKILL.md `name`≤64, `description`≤1024, body ≤~500 lines)
- [ ] No writes or staged commits to any external repository (`~/Documents/GitHub/claude-prompts/`)
- [ ] All 17 acceptance criteria closed (01-01…05, 02-01…03, 03-01…03, 04-01…03, 05-01…03)
- [ ] Manual ACs verified (01-05, 04-01…03, 05-01…03)

### Optional: Targeted Mutation Testing
MUTATION_TOOL = **UNAVAILABLE** (no `stryker-mutator`, `mutmut`, or `cargo-mutants` detected). Skip mutation testing this sprint.
**WARNING:** Do NOT run full-codebase mutation even if a tool is added later — it can take hours. Target specific changed files only.

### Drift Analysis
Compare the delivered work against [plan/original-requirements.md](plan/original-requirements.md):
- AC1 (install via `go install`) — satisfied by existing path + documented; `install.sh` is a companion (Story 3).
- AC2 (`.atcr/reviews/<id>/` output) — verified accurate post-rewrite (Story 1 / Story 4).
- AC3 (private-skill backward compatibility) — locked by repo-local contract test (Story 2), no external-repo changes.
- AC4 (`install.sh`) — delivered (Story 3); packaging/release automation remains descoped to Epic 21.0.
- Dispatcher override (single `/atcr <command>` UX) — delivered in `skill/SKILL.md` (Story 1); private `claude-prompts` migration recorded as a manual follow-up (Story 5).
- Confirm NO scope beyond the original request was added.

### Final Exit Gate (Gated Mode)
- [ ] Spawn a fresh subagent for a final hostile exit review over the full sprint diff (same brief shape as the phase gates). CRITICAL/HIGH → fix before completion; MEDIUM/LOW → `tech-debt-captured.md`.
