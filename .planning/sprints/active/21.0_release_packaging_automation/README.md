# Sprint 21.0: Release & Packaging Automation

**Plan Type:** 🏗️ Infrastructure (TASK-BASED)
**Complexity:** 7/12 (COMPLEX)
**Timeline:** 8 days · 5 phases
**Branch:** `feature/21.0_release_packaging_automation`
**Execution Mode:** Gated 🚧 · Adversarial Review ENABLED 🎯 (inline-fix: CRITICAL/HIGH)

---

## Overview

Stand up the `atcr` binary's first real release/packaging pipeline. Three prior epics (7.3, 16.0, 20.0) each hit the same missing infrastructure and deferred it; this sprint gives release automation its own scoped plan. It replaces the today-only `go install ...@latest` distribution path and the `"0.0.0"` version placeholder with a decided `vX.Y.Z` tag convention, a goreleaser config that stamps both version variables from a single tag, a tag-triggered GitHub Actions release workflow, and end-to-end documentation. No `atcr` engine behavior changes — distribution infrastructure only.

## Timeline

| Phase | Focus | Task | Est. |
|-------|-------|------|------|
| 1 | Foundation — Versioning & Tagging Convention | Task 01 (AC1) | ~1.5 days |
| 2 | Core Implementation — GoReleaser Config + Dual Ldflags | Task 02 (AC2, AC3) | ~2.5 days |
| 3 | Integration — Tag-Triggered Release Workflow | Task 03 (AC4) | ~1.5 days |
| 4 | Documentation — Release-Process Docs + Spec Correction | Task 04 (AC5) | ~1.5 days |
| Final | Validation — cross-task DoD | — | ~1 day |

Each implementation phase runs: task → fresh-subagent adversarial review → address findings → DoD → phase-boundary gate (gated stop).

## Expected Outcomes

- **AC1** — bare `vX.Y.Z` tag convention decided and documented, disjoint from Epic 8.0's `reconcile/vX.Y.Z`, formalizing `CHANGELOG.md`'s epic-number-as-semver history.
- **AC2** — `atcr version` / `atcr --version` reflects the real tagged version at build time.
- **AC3** — `.goreleaser.yaml` produces cross-platform binaries, snapshot-verified.
- **AC4** — `.github/workflows/release.yml` builds and publishes a GitHub Release on `v*` tag push.
- **AC5** — documented process describing how to cut a release, matching the real config/workflow.

## Risk Summary (top 3)

1. **Irreversible first release (Risk/Unknowns 3/3).** The first real `vX.Y.Z` tag push publishes a public, hard-to-retract GitHub Release. Mitigation: this sprint pushes NO real tag; the first cut is a deliberate, out-of-sprint maintainer action gated on Final-Phase DoD and a mandatory `--snapshot --clean` dry run.
2. **Version-prefix / dual-stamp divergence.** `{{.Version}}` (v-stripped) vs `{{.Tag}}` (v-prefixed) must be resolved deliberately so the two independent version vars agree on the numeric `X.Y.Z`; otherwise the CLI and the leaderboard submission envelope silently disagree. Mitigation: Task 02 Step 4 snapshot dry run confirms both targets before Phase 3.
3. **Release-workflow write permissions / supply chain.** `release.yml` carries `permissions: contents: write` — a higher-value target than the read-only base workflows. Mitigation: trigger scoped to tag push only, trusted `[self-hosted, gauntlet]` runner, default scoped `GITHUB_TOKEN` (no new secrets), all actions pinned (`@v6`/`@v4`/`@v5`).

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Executable phase/task plan (gated + adversarial) |
| [metadata.md](metadata.md) | Plan + sprint tracking |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge manifest (0 referenced) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, risk analysis |
| [plan/original-requirements.md](plan/original-requirements.md) | Source of truth |
| [plan/tasks/](plan/tasks/) | The 4 task specifications |
| [plan/documentation/](plan/documentation/) | goreleaser / version-tagging / CI-reuse references |

---

🎯 **Next:** `/refine-sprint @.planning/sprints/active/21.0_release_packaging_automation/`
