# Sprint 20.0: Standalone Skill Release

**Type:** ✨ Feature · **Complexity:** 8/12 (COMPLEX) · **Timeline:** 8 days · **Phases:** 5
**TDD Mode:** Moderate 🔄 · **Adversarial:** ENABLED 🎯 (inline CRITICAL/HIGH) · **Execution:** Gated 🚧
**Branch:** `feature/20.0_standalone_skill_release`

---

## Overview

Release atcr as a public engine alongside a single dispatcher skill (`/atcr <command>`), so anyone can run multi-agent code reviews on any repository without the private `.planning/` workflow — while keeping the internal private skills fully backward-compatible. `skill/SKILL.md` becomes a lightweight router over the `atcr` CLI with on-demand secondary instruction files, backed by a repo-local test that locks the documented backend contract.

## Timeline

| Phase | Focus | Est. |
|-------|-------|------|
| 1. Foundation | Dispatcher rewrite of `skill/SKILL.md` + secondary files (Story 1) | ~3 days |
| 2. Independent Verification | Backend-contract regression test (Story 2) + `install.sh` (Story 3) | ~2 days |
| 3. Documentation & Migration | Doc accuracy pass (Story 4) + external-migration note (Story 5) | ~1 day |
| 4. Integration | End-to-end dispatcher trace + command cross-check + full suite | ~1 day |
| 5. Validation | DoD, drift analysis, 17-AC closure | ~1 day |

## Expected Outcomes

- `skill/SKILL.md` dispatcher with routing table == `newRootCmd` (`cmd/atcr/main.go:185-208`), plus `host-review.md`, `ambiguity-adjudication.md`, `findings-format.md`.
- Hermetic `cmd/atcr/backend_contract_test.go` asserting the full documented `--output-dir` + reconcile output tree.
- `install.sh` thin `go install` wrapper + README Quickstart companion; `cmd/atcr/install_script_test.go` (`//go:build integration`).
- Accurate `docs/skill-usage.md`, `docs/code-review-backend.md`, `README.md`; new `docs/external-migration.md`.

## Risk Summary (top 3)

1. **Dispatcher rewrite regresses existing review→reconcile→report orchestration** → preserve all orchestration + Host Review/Ambiguity instructions verbatim in secondary files; entry surface only changes.
2. **Routing table drifts from the live Cobra tree** → treat `cmd/atcr/main.go:185-208` as sole source of truth; verified by AC01-01, not the stale `cobra.md` snapshot.
3. **`install.sh` scope creep into release automation** → constraint carried from Epic 21.0; adversarial review rejects any addition beyond the thin `go install` wrapper.

## Sprint Assets

- [sprint-plan.md](sprint-plan.md) — executable TDD sprint plan (this sprint's driver)
- [metadata.md](metadata.md) — plan + sprint tracking (single source of truth)
- [sprint-knowledge.yaml](sprint-knowledge.yaml) — knowledge manifest (created/referenced)
- [plan/](plan/) — archived plan snapshot (sprint-design, user-stories, acceptance-criteria, documentation, original-requirements)

---

**Next:** `/refine-sprint @.planning/sprints/active/20.0_standalone_skill_release/`
