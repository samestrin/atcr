# Sprint 21.0: Release & Packaging Automation

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 21.0 step-by-step. Complete each step, check off work immediately. This sprint runs in **gated mode** — after each phase's boundary gate, stop and wait for confirmation before starting the next phase.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What This Sprint Builds

Stand up the `atcr` binary's first real release/packaging pipeline: a decided-and-documented `vX.Y.Z` tag convention, a `.goreleaser.yaml` that cross-compiles binaries and stamps both independent version variables from a single tag, a tag-triggered GitHub Actions workflow that publishes a GitHub Release, and end-to-end documentation of how a maintainer cuts a release. No `atcr` engine behavior changes — this is distribution infrastructure only.

### Why This Matters

Three prior epics (7.3, 16.0, 20.0) each independently hit the same missing release infrastructure and re-punted, because standing it up was out of scope for whatever feature needed it first. This sprint breaks that loop by giving release automation its own scoped plan, replacing the today-only `go install ...@latest` path and the `"0.0.0"` version placeholder with a real, versioned, tag-triggered release.

### Key Deliverables

- `docs/release-process.md` documenting the bare `vX.Y.Z` tag convention, disjoint from Epic 8.0's `reconcile/vX.Y.Z` namespace, formalizing `CHANGELOG.md`'s epic-number-as-semver history (AC1).
- `.goreleaser.yaml` with a `builds:` block stamping both `internal/version.Version` and `cmd/atcr`'s local `version` var via two `-X` ldflags entries from the same tag (AC2, AC3).
- `.github/workflows/release.yml`, `based_on: ci.yml`, triggered on `push: tags: ['v*']`, with `permissions: contents: write` and a `goreleaser-action` step (AC4).
- Complete release-process documentation (What Triggers / Who Cuts / Cutting a Release) plus correction of `.planning/specifications/git-strategy.md:36`'s stale merge-triggered-release claim (AC5).

### Success Criteria

- A versioning/tagging strategy is decided and documented as bare `vX.Y.Z` tags (AC1).
- `atcr version` / `atcr --version` reflects the real tagged version at build time (AC2).
- A goreleaser config exists and produces cross-platform binaries, verified via local `goreleaser release --snapshot --clean` (AC3).
- A tag-triggered GitHub Actions workflow builds and publishes a GitHub Release, verified via YAML lint + tag-filter disjointness (AC4).
- A documented process describing how to cut a release exists and matches the real `.goreleaser.yaml`/`release.yml` (AC5).
- No real `vX.Y.Z` tag is pushed as a side effect of this sprint — the first real cut is a deliberate, out-of-sprint maintainer action.

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Plan Type:** 🏗️ Infrastructure (TASK-BASED) — no `*_test.go` surface is created or modified by this sprint.

- **Verification model:** This plan produces declarative config (`.goreleaser.yaml`), a GitHub Actions workflow (`release.yml`), and documentation — not Go code. `PRIMARY_TEST_LOCATION` is **N/A**; the existing `go test ./...` suite and 80% coverage baseline are unaffected because no `.go` files change.
- **Substitute verification** (per the design's TDD-Specific risk mitigation):
  - Task 02 / Final Phase: local `goreleaser release --snapshot --clean` dry run (non-publishing) exercising the full cross-platform build + dual-ldflags stamping path.
  - Task 03: YAML parse/lint (`yamllint` or `python3 -c "import yaml; yaml.safe_load(open(...))"`) + manual tag-filter disjointness check against `reconcile-module.yml`.
  - Tasks 01 & 04: manual doc review against each task's Success Criteria.
- **Adversarial review:** ENABLED 🎯 — a fresh subagent reviews each task's artifacts (inline-fix bar: **CRITICAL/HIGH**; defer **MEDIUM/LOW** to tech debt).
- **Gated execution:** ENABLED 🚧 — a phase-boundary gate runs after each phase's DoD; `/execute-sprint` stops at each boundary.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, risk analysis |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [tasks/](plan/tasks/) | The 4 infrastructure tasks this sprint executes |
| [documentation/](plan/documentation/) | Grounded package + convention references (goreleaser, version-tagging, CI-reuse) |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `yaml.safe_load(<file>)` / section re-read |
| T2: Module | After completing a task | `goreleaser release --snapshot --clean` (Task 02) / `yamllint` (Task 03) |
| T3: Full | DoD validation, pre-commit | `go test ./...` (regression — unaffected) + full artifact cross-check |

### DoD Verification Checklist

1. Task Success Criteria: all checked
2. Artifact validity: YAML parses / docs read coherently
3. Grounding: citations to live code (`internal/version/version.go`, `cmd/atcr/version.go`, `reconcile-module.yml`, `CHANGELOG.md`) accurate
4. No scope creep: only the task's declared files touched
5. No real `vX.Y.Z` tag pushed

### DoD Report Template

```
Phase-{N} DoD Complete
Auto: {X}/5 | Task-Specific: {Y}/{Z}
Manual Review: [ ] Artifact reviewed against original-requirements.md
```

### Commit Process

Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

---

## Development Standards

**Coding Standards** (from `.planning/specifications/coding-standards.md`) — no Go code is authored here, but any incidental change follows: Conventional Commits (`type(scope): description`), `go fmt`/`goimports`, `golangci-lint run`, `go vet ./...`.

**Git Strategy** (from `.planning/specifications/git-strategy.md`) — GitHub Flow: short-lived `feature/` branch, atomic Conventional Commits, PR against `main`, squash-and-merge. NOTE: `git-strategy.md:36` currently carries the stale "Merging to `main` builds production release binaries" claim that **Task 04 corrects** — do not treat that line as authoritative during this sprint.

**Implementation Standards** (from `.planning/specifications/implementation-standards.md`) — replaceability: the release mechanism (goreleaser config + workflow) must stay fully swappable without touching `cmd/atcr/version.go` or `internal/version/version.go` — only their build-time `-ldflags` values change.

---

## External Resources

From [plan/documentation/README.md](plan/documentation/README.md):

- [version-tagging-strategy.md](plan/documentation/version-tagging-strategy.md) `[CRITICAL]` — the two independent version variables and the bare `vX.Y.Z` convention.
- [goreleaser-configuration.md](plan/documentation/goreleaser-configuration.md) `[CRITICAL]` — dual `-X` ldflags stamping and the `GOOS`/`GOARCH` matrix.
- [ci-workflow-reuse.md](plan/documentation/ci-workflow-reuse.md) `[IMPORTANT]` — the `based_on:` reuse convention and the full workflow-inventory tag-collision check.
- Grounded source: `.planning/specifications/packages/goreleaser.md`.

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation — Versioning & Tagging Convention (~1.5 days)

### 1.1 [ ] **🏗️ Document the Versioning & Tagging Convention**
   **Task:** [task-01-versioning-tagging-strategy.md](plan/tasks/task-01-versioning-tagging-strategy.md) (AC1)
   **Priority:** P1 | **Effort:** S | **Type:** Add
   1. Create `docs/release-process.md` with a `# Release Process` heading and a `## Versioning & Tagging Convention` section (author only this section; leave the file otherwise minimal so Task 04 can append cleanly).
   2. State the convention: bare `vX.Y.Z` git tags matching the corresponding `CHANGELOG.md` entry (e.g. `## [20.1.0]` → tag `v20.1.0`); cite the unbroken `[1.0.0]`→`[20.1.0]` epic-number-as-semver history and note zero tags exist against it (forward-only, no retroactive backfill).
   3. Document disjointness from Epic 8.0's `reconcile/vX.Y.Z` namespace, quoting/paraphrasing `.github/workflows/reconcile-module.yml:14-16` as the precedent that bare `vX.Y.Z` is free.
   4. Note (do not implement) the dual `-ldflags` stamping contract Task 02 fulfills: the tag value stamps both `internal/version.Version` (internal/version/version.go:16) and `cmd/atcr`'s local `version` (cmd/atcr/version.go:14).
   5. Do NOT touch `git-strategy.md:36` — that correction is Task 04's.
   **Success Criteria:** section exists; bare `vX.Y.Z` tied to a `CHANGELOG.md` heading example; disjointness cited; forward-only noted; dual-ldflags contract referenced (not implemented); no code/workflow/config changed.
   **Files:** `docs/release-process.md` (create) | **Duration:** ~0.75 day
   6. COMMIT: `git add docs/release-process.md && git commit -m "docs(release): document vX.Y.Z tagging convention (task 01)"`

### 1.1.A [ ] **Task 01 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `docs/release-process.md`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.1 — intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.1`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `/Users/samestrin/Documents/GitHub/atcr/docs/release-process.md`
     - Ground-truth to verify against: `internal/version/version.go`, `cmd/atcr/version.go`, `.github/workflows/reconcile-module.yml`, `CHANGELOG.md`
     - Checklist (release-doc adapted — pass verbatim):
       - ACCURACY: Do all cited file paths, line numbers, and `CHANGELOG.md` version examples match the live repo?
       - NAMESPACE SAFETY: Is the `vX.Y.Z` vs `reconcile/vX.Y.Z` disjointness claim correct and unambiguous (no tag can match both)?
       - CONTRACT CLARITY: Is the dual-ldflags contract described precisely enough for Task 02 to implement without guessing (which var, which package path)?
       - SCOPE: Did this task stay documentation-only (no code/workflow/config, no `git-strategy.md` edit)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 1.1.R, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 1.1.R [ ] **Task 01 — REFACTOR / Address Findings**
   1. Fix CRITICAL/HIGH issues from 1.1.A (if any)
   2. Improve doc clarity / fix any inaccurate citation (T1: re-read section)
   3. COMMIT (only if changes made): `git add docs/release-process.md && git commit -m "docs(release): address review findings (task 01)"`
   **Duration:** ~15-30 min

### 1.2 [ ] **Phase 1 — DoD Verification**
   - [ ] Task 01 Success Criteria all checked
   - [ ] `docs/release-process.md` reads coherently; all citations accurate (T3 cross-check)
   - [ ] Only `docs/release-process.md` changed this phase
   - [ ] No real `vX.Y.Z` tag pushed
   - [ ] Adversarial review passed / findings resolved
   **DoD Report:** emit Phase-1 DoD Report per template.

### 1.3 [ ] **Phase 1 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 1 (`docs/release-process.md`)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. No memory of the phase's implementation — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 1 (absolute paths): `/Users/samestrin/Documents/GitHub/atcr/docs/release-process.md`
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Is the `vX.Y.Z` ↔ `X.Y.Z`/`vX.Y.Z` stamping contract stated unambiguously so Phase 2 can implement it without reinterpreting?
       - CONFIG SURFACE: Is the tag convention documented such that a maintainer could apply it with no other context?
       - INTEGRATION: Does the disjointness claim vs `reconcile-module.yml` hold against the actual workflow file?
       - PHASE-EXIT CONTRACT: Can Phase 4 append its three sections without restructuring what Phase 1 wrote?
       - REGRESSION: N/A (no prior phase).
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

   🚧 **GATED STOP:** Phase 1 complete. Stop and await confirmation before starting Phase 2.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 2: Core Implementation — GoReleaser Config + Dual Ldflags (~2.5 days)

### 2.1 [ ] **🏗️ GoReleaser Configuration with Dual Version Stamping**
   **Task:** [task-02-goreleaser-configuration.md](plan/tasks/task-02-goreleaser-configuration.md) (AC2, AC3)
   **Priority:** P1 | **Effort:** S | **Type:** Add
   1. Create `.goreleaser.yaml` at repo root with a `builds:` block: `main: ./cmd/atcr`, `binary: atcr`, `env: [CGO_ENABLED=0]`, and an `ldflags:` list carrying **both** `-X github.com/samestrin/atcr/internal/version.Version=...` and `-X main.version=...`.
   2. **DELIBERATE DECISION (Risk/Unknowns driver):** resolve `{{.Version}}` (v-stripped) vs `{{.Tag}}` (v-prefixed) for each `-X` target so the CLI output matches Task 01's documented convention and the `go install` path; the two targets must agree on the numeric `X.Y.Z` portion (v-prefix is the only allowed difference). Record the choice in a config comment. If you diverge from documentation/goreleaser-configuration.md's Quick Reference (which shows `{{.Version}}` for both), note the follow-up doc update.
   3. Consider `{{.CommitDate}}` (or omitting the date stamp) instead of default `{{.Date}}` for reproducible builds — document the choice inline either way.
   4. Add `archives:` and `checksum:` blocks using goreleaser defaults. Omit `goos`/`goarch` to accept the default matrix unless a build failure forces a documented exclusion.
   5. Ensure `dist/` is git-ignored (add to `.gitignore` if missing); do NOT commit `dist/`.
   **Success Criteria:** valid goreleaser YAML; both `-X` targets present; `goreleaser release --snapshot --clean` completes; a built binary's `atcr version`/`--version` matches the snapshot version; `internal/version.Version` confirmed stamped to the same numeric value (binary inspection or throwaway build); full matrix builds.
   **Files:** `.goreleaser.yaml` (create), `.gitignore` (modify if needed) | **Duration:** ~1.5 day
   6. **T2 VERIFY:** run `goreleaser release --snapshot --clean` from repo root (install via `go install github.com/goreleaser/goreleaser/v2@latest` or `brew install goreleaser` if absent). Inspect `dist/`, confirm both ldflags targets agree on the numeric version.
   7. COMMIT: `git add .goreleaser.yaml .gitignore && git commit -m "build(release): add goreleaser config with dual ldflags stamping (task 02)"`

### 2.1.A [ ] **Task 02 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `.goreleaser.yaml`, `.gitignore`

   **Spawn a fresh subagent** via the Agent tool. No memory of 2.1 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.1`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `/Users/samestrin/Documents/GitHub/atcr/.goreleaser.yaml`, `/Users/samestrin/Documents/GitHub/atcr/.gitignore`
     - Ground-truth: `internal/version/version.go` (package path + var), `cmd/atcr/version.go` (`main.version`), `go.mod` (module path)
     - Checklist (goreleaser-config adapted — pass verbatim):
       - LDFLAGS CORRECTNESS: Are BOTH `-X` targets present with the exact package paths (`github.com/samestrin/atcr/internal/version.Version` and `main.version`)? Do they agree on the numeric `X.Y.Z` (only v-prefix may differ)?
       - VERSION-PREFIX CONSISTENCY: Does the `{{.Version}}` vs `{{.Tag}}` choice actually match Task 01's documented convention and `atcrVersion()`'s `debug.ReadBuildInfo()` fallback behavior?
       - REPRODUCIBILITY: Is the date-stamp decision (`{{.Date}}` vs `{{.CommitDate}}`/omit) made and documented, not left to a silent default?
       - SUPPLY CHAIN / LEAKAGE: Is `dist/` git-ignored so build artifacts and any embedded paths are not committed? Any secrets or absolute-path leakage in the config?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 2.1.R, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 2.1.R [ ] **Task 02 — REFACTOR / Address Findings**
   1. Fix CRITICAL/HIGH issues from 2.1.A (if any)
   2. Re-run `goreleaser release --snapshot --clean` to confirm the config still builds and both targets still stamp correctly (T2)
   3. COMMIT (only if changes made): `git add .goreleaser.yaml .gitignore && git commit -m "build(release): address review findings (task 02)"`
   **Duration:** ~30 min

### 2.2 [ ] **Phase 2 — DoD Verification**
   - [ ] Task 02 Success Criteria all checked
   - [ ] `goreleaser release --snapshot --clean` passes; both version vars stamped to the same numeric value
   - [ ] `{{.Version}}`/`{{.Tag}}` and date-stamp decisions documented in-config
   - [ ] `dist/` git-ignored and not committed
   - [ ] Only `.goreleaser.yaml` / `.gitignore` changed; no `.go` files modified
   - [ ] No real `vX.Y.Z` tag pushed
   - [ ] Adversarial review passed / findings resolved
   **DoD Report:** emit Phase-2 DoD Report per template.

### 2.3 [ ] **Phase 2 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2 (`.goreleaser.yaml`, `.gitignore`)

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 2 (absolute paths): `/Users/samestrin/Documents/GitHub/atcr/.goreleaser.yaml`, `/Users/samestrin/Documents/GitHub/atcr/.gitignore`
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Does `.goreleaser.yaml` honor Task 01's documented tag→version contract exactly?
       - CONFIG SURFACE: Are all deliberate decisions (ldflags prefix, date stamp, matrix exclusions) documented inline and defaulted safely?
       - INTEGRATION: Will Task 03's `goreleaser-action release --clean` consume this config without extra flags/config it doesn't provide?
       - PHASE-EXIT CONTRACT: Can Task 03 wire a workflow around this config as-is, and Task 04 document its commands accurately?
       - REGRESSION: Does the existing `go build`/`go install` path still work (no `.go` change, no `go.mod` dependency added)?
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

   🚧 **GATED STOP:** Phase 2 complete. Stop and await confirmation before starting Phase 3.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 3: Integration — Tag-Triggered Release Workflow (~1.5 days)

### 3.1 [ ] **🏗️ Tag-Triggered Release Workflow**
   **Task:** [task-03-tag-triggered-release-workflow.md](plan/tasks/task-03-tag-triggered-release-workflow.md) (AC4)
   **Priority:** P1 | **Effort:** S | **Type:** Add
   1. Create `.github/workflows/release.yml` with a header comment (mirroring `reconcile-module.yml:1-9`) and a `based_on: .github/workflows/ci.yml` line naming exactly what is reused verbatim (`[self-hosted, gauntlet]` runner, Go 1.25 setup).
   2. Trigger: `on: push: tags: ['v*']`, with an inline comment (mirroring `reconcile-module.yml:11-16`) explaining the bare `v*` filter is deliberately disjoint from `reconcile/v*`.
   3. Set `permissions: contents: write` (the single deliberate divergence from the base workflows' `contents: read` — goreleaser publishes a Release).
   4. Add `concurrency: release-${{ github.ref }}` with `cancel-in-progress: true` (matching `ci.yml:12-14` / `reconcile-module.yml:21-23`).
   5. Single `release` job on `runs-on: [self-hosted, gauntlet]`: `actions/checkout@v4` with `fetch-depth: 0` (goreleaser needs full tag history), `actions/setup-go@v5` (`go-version: '1.25'`, `cache: false`, carry the rationale comment), then `goreleaser/goreleaser-action@v6` with `args: release --clean` using the default `GITHUB_TOKEN` (no new secret).
   6. Add a comment near the goreleaser step referencing the dry-run precedent: the first real tag push publishes a public, hard-to-retract Release, so run `goreleaser release --snapshot --clean` locally and confirm with the maintainer before the first `git push --tags`.
   **Success Criteria:** exists; triggers only on `push: tags: 'v*'`; `based_on` header present; `permissions: contents: write`; runner/checkout/setup-go reused verbatim; `goreleaser-action` runs `release --clean`; disjointness from `reconcile-module.yml` documented inline.
   **Files:** `.github/workflows/release.yml` (create) | **Duration:** ~0.75 day
   7. **T2 VERIFY:** validate YAML — `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/release.yml'))"` (or `yamllint`). Confirm `v1.2.3` matches `v*` but not `reconcile/v*`, and `reconcile/v1.2.3` matches only the latter. Do NOT push a real tag.
   8. COMMIT: `git add .github/workflows/release.yml && git commit -m "ci(release): add tag-triggered goreleaser release workflow (task 03)"`

### 3.1.A [ ] **Task 03 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `.github/workflows/release.yml`

   **Spawn a fresh subagent** via the Agent tool. No memory of 3.1 — intentional. Do NOT review inline. This workflow holds `contents: write` — review it as a security-sensitive artifact (per the design's Security-Sensitive Areas table).

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.1`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `/Users/samestrin/Documents/GitHub/atcr/.github/workflows/release.yml`
     - Ground-truth: `.github/workflows/ci.yml`, `.github/workflows/reconcile-module.yml` (runner, setup-go, concurrency, tag-filter precedent)
     - Checklist (release-workflow security-adapted — pass verbatim):
       - TRIGGER SCOPE: Is the workflow scoped to `push: tags: 'v*'` only (never `pull_request`)? Can any untrusted event invoke it with write permissions?
       - PERMISSIONS: Is `contents: write` the minimum needed, declared at the right scope, and not broadened beyond what goreleaser's Release publish requires?
       - SUPPLY CHAIN: Are all third-party actions pinned to a fixed version (`goreleaser-action@v6`, `checkout@v4`, `setup-go@v5`)? Any unpinned/floating ref that could pull compromised release-time code with write access?
       - NAMESPACE DISJOINTNESS: Does `v*` provably NOT overlap `reconcile/v*` (no tag fires both)? Is the disjointness documented inline?
       - TOOLCHAIN DRIFT: Do `go-version`, `cache: false`, and runner label match `ci.yml` verbatim so a tag that passed CI can't fail the release build on drift?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 3.1.R, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 3.1.R [ ] **Task 03 — REFACTOR / Address Findings**
   1. Fix CRITICAL/HIGH issues from 3.1.A (if any) — especially any permissions/supply-chain finding
   2. Re-validate YAML parses and tag-filter disjointness still holds (T2)
   3. COMMIT (only if changes made): `git add .github/workflows/release.yml && git commit -m "ci(release): address review findings (task 03)"`
   **Duration:** ~30 min

### 3.2 [ ] **Phase 3 — DoD Verification**
   - [ ] Task 03 Success Criteria all checked
   - [ ] `release.yml` valid YAML; triggers only on `v*`; disjoint from `reconcile/v*`
   - [ ] `permissions: contents: write`; runner/checkout/setup-go reused verbatim; actions pinned
   - [ ] Only `.github/workflows/release.yml` changed this phase
   - [ ] No real `vX.Y.Z` tag pushed
   - [ ] Adversarial review passed / findings resolved
   **DoD Report:** emit Phase-3 DoD Report per template.

### 3.3 [ ] **Phase 3 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3 (`.github/workflows/release.yml`)

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): `/Users/samestrin/Documents/GitHub/atcr/.github/workflows/release.yml`
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Does `release.yml` invoke `.goreleaser.yaml` (Phase 2) exactly as that config expects (`release --clean`, no missing flags)?
       - CONFIG SURFACE: Is `permissions: contents: write` the only divergence from `based_on: ci.yml`, and is it documented as deliberate?
       - INTEGRATION: Full workflow-inventory collision check — does the `v*` trigger conflict with ci.yml / reconcile-module.yml / hermes-auto-merge.yml / refresh-synthetic-manifest.yml?
       - PHASE-EXIT CONTRACT: Can Task 04 accurately document this workflow's filename, trigger, and behavior without it changing?
       - REGRESSION: Do the existing workflows still behave unchanged (no shared file edited)?
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

   🚧 **GATED STOP:** Phase 3 complete. Stop and await confirmation before starting Phase 4.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 4: Documentation — Release-Process Docs + Spec Correction (~1.5 days)

### 4.1 [ ] **🏗️ Complete Release-Process Documentation**
   **Task:** [task-04-release-process-documentation.md](plan/tasks/task-04-release-process-documentation.md) (AC5)
   **Priority:** P2 | **Effort:** S | **Type:** Add
   1. Read the existing `docs/release-process.md` (Phase 1) and APPEND after its `## Versioning & Tagging Convention` section — do not restructure it.
   2. `## What Triggers a Release`: a `vX.Y.Z` tag push (NOT a merge to `main`) fires `release.yml` → goreleaser → GitHub Release. State explicitly that a PR merge to `main` does not produce a release.
   3. `## Who Cuts a Release`: single maintainer (Sam Estrin); solo decision, not a formal rotation. One or two sentences.
   4. `## Cutting a Release`: exact procedure — confirm the `CHANGELOG.md` entry exists (tag matches heading, e.g. `## [21.0.0]` → `v21.0.0`); recommended local `goreleaser release --snapshot --clean` dry run first; the real cut `git tag vX.Y.Z && git push origin vX.Y.Z`; what happens next (workflow auto-publishes); explicit note that the first real tag is public/hard-to-retract so the dry run is non-optional.
   5. Correct `.planning/specifications/git-strategy.md:36`: replace the stale "Merging to `main` builds production release binaries..." line with an accurate tag-triggered description linking to `docs/release-process.md` (fix the relative path for git-strategy.md's location). Change ONLY that line.
   6. Re-read both edited files in full: appended sections read coherently after Phase 1's content; git-strategy.md change is a single self-contained line.
   **Success Criteria:** all four sections present; trigger/who/cutting accurate to the real `.goreleaser.yaml`/`release.yml`; `git-strategy.md:36` no longer claims merge-triggered releases and links the doc.
   **Files:** `docs/release-process.md` (append), `.planning/specifications/git-strategy.md` (1-line correction) | **Duration:** ~0.75 day
   7. COMMIT: `git add docs/release-process.md .planning/specifications/git-strategy.md && git commit -m "docs(release): complete release-process doc + correct git-strategy (task 04)"`

### 4.1.A [ ] **Task 04 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `docs/release-process.md`, `.planning/specifications/git-strategy.md`

   **Spawn a fresh subagent** via the Agent tool. No memory of 4.1 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.1`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `/Users/samestrin/Documents/GitHub/atcr/docs/release-process.md`, `/Users/samestrin/Documents/GitHub/atcr/.planning/specifications/git-strategy.md`
     - Ground-truth: the ACTUAL `.goreleaser.yaml` (Phase 2) and `.github/workflows/release.yml` (Phase 3) — commands and behavior in the doc must match these, not the plan's design intent
     - Checklist (release-doc adapted — pass verbatim):
       - COMMAND ACCURACY: Do the documented `git tag`/`git push`/`goreleaser` commands match exactly what `.goreleaser.yaml` and `release.yml` actually do (tag format, workflow filename, goreleaser invocation)?
       - COMPLETENESS: Are all four sections present and does the "Cutting a Release" procedure make the snapshot dry run a non-optional first step?
       - SPEC CORRECTION: Does `git-strategy.md:36` now accurately describe the tag-triggered mechanism, with a working relative link, and is it a single self-contained line change (no surrounding text disturbed)?
       - IRREVERSIBILITY WARNING: Is the public/hard-to-retract nature of the first real tag push called out clearly?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 4.1.R, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.1.R [ ] **Task 04 — REFACTOR / Address Findings**
   1. Fix CRITICAL/HIGH issues from 4.1.A (if any) — especially any doc-vs-implementation command mismatch
   2. Re-read both files to confirm coherence (T1)
   3. COMMIT (only if changes made): `git add docs/release-process.md .planning/specifications/git-strategy.md && git commit -m "docs(release): address review findings (task 04)"`
   **Duration:** ~15-30 min

### 4.2 [ ] **Phase 4 — DoD Verification**
   - [ ] Task 04 Success Criteria all checked
   - [ ] All four `docs/release-process.md` sections read coherently as one document
   - [ ] Documented commands verified against the real `.goreleaser.yaml` / `release.yml`
   - [ ] `git-strategy.md:36` corrected; no other line disturbed
   - [ ] No real `vX.Y.Z` tag pushed
   - [ ] Adversarial review passed / findings resolved
   **DoD Report:** emit Phase-4 DoD Report per template.

### 4.3 [ ] **Phase 4 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4 (`docs/release-process.md`, `.planning/specifications/git-strategy.md`)

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths): `/Users/samestrin/Documents/GitHub/atcr/docs/release-process.md`, `/Users/samestrin/Documents/GitHub/atcr/.planning/specifications/git-strategy.md`
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Does the full document accurately describe the mechanism Phases 1-3 built (no aspirational/incorrect commands)?
       - CONFIG SURFACE: Is the "Cutting a Release" procedure runnable verbatim by a maintainer with no other context?
       - INTEGRATION: Is the `git-strategy.md` correction consistent with the surrounding CI/CD subsection (no lingering "merge = deploy" implication)?
       - PHASE-EXIT CONTRACT: Is AC5 fully satisfied and does the doc close the loop on AC1-AC4?
       - REGRESSION: Does Phase 1's section remain intact and unrestructured after the append?
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

   🚧 **GATED STOP:** Phase 4 complete. Stop and await confirmation before starting the Final Phase.

---

## Final Phase: Validation (~1 day)

Cross-task Definition of Done verification — confirm the four artifacts cohere end-to-end. No new implementation.

### Validation Checklist
- [ ] Re-run `goreleaser release --snapshot --clean`; snapshot build succeeds and both `-X` ldflags targets (`internal/version.Version`, `main.version`) stamp to the same numeric value.
- [ ] `docs/release-process.md` reads coherently end-to-end across all four sections (Versioning & Tagging Convention, What Triggers, Who Cuts, Cutting a Release).
- [ ] Documented commands match the actual `.goreleaser.yaml` and `.github/workflows/release.yml` produced (tag format, workflow filename, goreleaser invocation).
- [ ] `.planning/specifications/git-strategy.md:36` correction is a single, self-contained line change and reads consistently in its CI/CD subsection.
- [ ] `.github/workflows/release.yml` still parses as valid YAML and its `v*` trigger remains disjoint from `reconcile/v*`.
- [ ] All 5 acceptance criteria (AC1–AC5) satisfied and traceable to the delivered artifacts.
- [ ] **No real `vX.Y.Z` tag was pushed** as a side effect of implementation — verify `git tag` still returns zero `v*` app tags. The first real cut is a deliberate, out-of-sprint maintainer action gated on this DoD.
- [ ] Regression: `go test ./...` still passes and coverage baseline is unchanged (no `.go` files modified).

### Optional: Targeted Mutation Testing
MUTATION_TOOL = **UNAVAILABLE** (no stryker/mutmut/cargo-mutants detected) — and not applicable regardless: this sprint modifies no Go code, so there is no mutable code surface. Skip.
**WARNING:** Do NOT run full codebase mutation — it can take hours. N/A for this sprint.

### Drift Analysis
Compare the four delivered artifacts against [plan/original-requirements.md](plan/original-requirements.md):
- AC1 → Task 01: `vX.Y.Z` convention documented, disjoint from `reconcile/v*`, formalizes `CHANGELOG.md` history.
- AC2 → Task 02: `atcr version`/`--version` reflects the tagged version via ldflags stamping.
- AC3 → Task 02: goreleaser config produces cross-platform binaries (snapshot-verified).
- AC4 → Task 03: tag-triggered workflow builds and publishes a GitHub Release.
- AC5 → Task 04: documented cut-a-release process exists and matches the real config/workflow.
- Confirm no scope crept beyond the original request (no Homebrew/npm/Docker distribution, no engine behavior change, no re-opening Epic 20.0).
