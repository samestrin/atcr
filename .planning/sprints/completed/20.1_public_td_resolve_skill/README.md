# Sprint 20.1: Public TD Resolve Skill

**Type:** ✨ Feature Development
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 10 days
**Phases:** 5
**Execution Mode:** Gated 🚧 | **Adversarial Review:** ENABLED 🎯 (inline-fix CRITICAL/HIGH)
**Branch:** `feature/20.1_public_td_resolve_skill`

---

## Overview

Give standalone/public `atcr` users the review-and-fix loop the private `.planning/technical-debt/` + `/resolve-td` pipeline already has. This sprint adds a local, `.atcr/`-scoped append-only technical-debt store, a reconcile-time persistence hook that accumulates findings across runs, and a new autonomous `/atcr debt resolve` dispatcher route that resolves them via a `.planning/`-free RED→GREEN→ADVERSARIAL→REFACTOR cycle. It is the first public-side consumer of Epic 18.3's `justification`/back-reference finding enrichment.

## Timeline

| Phase | Focus | Est. |
|-------|-------|------|
| 1. Foundation | `internal/localdebt` append-only store (Story 1) | 1.5d |
| 2. Core Items | Reconcile persistence hook (Story 2) + shared `CONVENTIONS.md` (Story 4), parallel | 2d |
| 3. Advanced | `atcr debt resolve` CLI + `skill/debt-resolve/SKILL.md` (Story 3) | 3.5d |
| 4. Integration & Docs | Document in `docs/skill-usage.md` + dispatcher consistency (Story 5) | 1.5d |
| 5. Validation | Cumulative adversarial, E2E walkthrough, coverage/lint gates, drift | 1.5d |

## Expected Outcomes

- **AC1:** Local TD store format defined, documented, and unit-tested (`.atcr/debt/YYYY-MM.jsonl`).
- **AC2:** Reconciled findings persist across multiple review runs with write-time dedup by `FindingID`.
- **AC3:** `skill/SKILL.md` extended; `atcr debt resolve` autonomously resolves store items, consuming `justification`/`SourceReport`.
- **AC4:** Capability documented in `docs/skill-usage.md`.
- **AC5 (refinement):** Shared skill boilerplate extracted to `skill/CONVENTIONS.md`; both SKILL.md files point at it.

## Risk Summary (Top 3)

1. **E2E ACs are agent-driven Markdown behavior `go test` cannot exercise (Story 3: 03-04, 03-05).** → `skill/skill_test.go` covers structural/stage-name assertions only; the actual cycle is validated via a Phase 5 fixture-repo scenario walkthrough — budgeted as a distinct activity, not skipped because "tests pass."
2. **Sequencing: Story 4 (`CONVENTIONS.md`) must land before Story 3 references it.** → Phase 2 bundles Story 4 (parallel with Story 2) so Phase 3 starts against a finished file, not a placeholder.
3. **New `.atcr/`-scoped store resembles the `.atcr/findings-history.jsonl` design Epic 19.4 moved away from.** → `internal/localdebt` package doc comments state the differing audience explicitly; `internal/history` imported only for `FindingID`, never its storage logic. Concurrent-write tear accepted per TD-004 (documented, one `os.Write` per record).

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Executable phase/task plan (TDD + adversarial + gated) |
| [metadata.md](metadata.md) | Plan + sprint tracking, execution metrics |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge manifest (created/referenced) |
| [plan/](plan/) | Archived source plan (requirements, stories, ACs, design, docs) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, risk analysis |
| [plan/original-requirements.md](plan/original-requirements.md) | Source of truth |

---

**Next:** `/refine-sprint @.planning/sprints/active/20.1_public_td_resolve_skill/`
