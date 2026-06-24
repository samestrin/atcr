# Sprint 9.0: Persona Ecosystem

## Plan Metadata
**Created:** 2026-06-24
**Status:** Sprint Created — Ready for Execution
**Plan Number:** 9.0
**Plan Type:** feature
**Feature Category:** Extensibility / Domain Personas
**Priority:** High
**Assigned Team:** Full Stack
**Epic/Initiative:** Epic 9.0 — Persona Ecosystem
**Dependencies:** Epic 1.1 (registry schema — complete), Epic 3.0 (SelectEligibleSkeptics — complete)
**Stakeholders:** Sam Estrin

## Estimated User Story Count
**Complexity Level:** Very-Complex
**Estimated Stories:** 6

---

## Complexity & Schedule

**Complexity:** 10/12 (VERY COMPLEX)
**Timeline:** 17 days across two sprints (A: Phases 1-3, B: Phases 4-6)
**Phases:** 6
**Pattern:** Foundation → Core Routing → Built-in Personas → CLI Surface → Bundles+Scores → Docs+Validation
**Adversarial Review:** ENABLED 🎯
**Inline-Fix Severities:** CRITICAL/HIGH
**Execution Mode:** Gated 🚧

---

## Workflow Status

**Document Status**: [x] Plan Created [x] User Stories Added [x] Acceptance Criteria Added [x] Ready for Sprint

## Sprint Tracking

**Sprint Reference:** .planning/sprints/active/9.0_persona_ecosystem/
**Sprint Number:** 9.0
**Sprint Created:** 2026-06-24
**Sprint Status:** Active — Not Yet Executed
**Branch:** feature/9.0_persona_ecosystem

---

**Single Source of Truth:** This metadata.md file is the active tracking document during sprint execution.

---

## Sprint Execution Tracking

_Updated by `/execute-sprint` during execution._

**Current Phase:** Phase 5 complete — gated stop before Phase 6 (Sprint B, Stories 04 + 05 done)
**Phases Complete:** 5 / 6
**Last Checkpoint:** 2026-06-24 — Phase 5 (T5 domain bundles + T6 corroboration scores) green; gate passed (no CRITICAL/HIGH; regression suite clean). T5: `internal/personas/bundles.go` resolver over `go:embed bundles/*.yaml` (django + go-production manifests), typed `ErrUnknownBundle`, `InstallBundle` partial-skip/idempotent loop, `personas install bundle/<name>` CLI delegation. T6: `personas list --scores` joins per-reviewer corroboration rates (lowercase key) to a CORROBORATION column, sort numeric-desc then n/a-alphabetical, "no data" footer; per-reviewer aggregation collapses (reviewer,model) rows. Adversarial 5.2.A: no CRITICAL/HIGH (dedup fixed inline per AC 04-04 EC3). Adversarial 5.4.A: 1 HIGH (multi-model score last-wins) fixed inline with regression test + 2 MEDIUM/1 LOW fixed inline. Gate 5.LAST: TD-013 (MEDIUM key-convention divergence), TD-014/TD-015 (LOW) captured. Coverage: internal/personas 86.9%, cmd/atcr 84.0% (both ≥80%). Commits: (T5 green) domain bundles, (T6 green) list --scores, (refactor) per-reviewer aggregation + dedup.

---

## Execution Metrics

_Populated by `/execute-sprint` upon completion_

**Executed:** _Not yet executed_
**Runtime:** _TBD_
**Status:** _TBD_

### Progress
- **Phases:** _TBD_
- **Work Items:** _TBD_

### Quality
- **Tests:** _TBD_
- **Coverage:** _TBD_
- **Lint:** _TBD_

### Changes
- **Files Changed:** _TBD_
- **Commits:** _TBD_
