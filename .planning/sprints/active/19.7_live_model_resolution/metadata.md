# Sprint 19.7: Live Model Resolution, Lockfile & Drift Detection

## Plan Metadata
**Created:** 2026-07-08
**Status:** Sprint Ready
**Plan Number:** 19.7
**Plan Type:** feature
**Feature Category:** Persona Ecosystem / Model Resolution
**Priority:** High
**Assigned Team:** Backend / CLI
**Epic/Initiative:** Epic 19.6 (Community Registry Hub); feeds Epic 19.8 (Hermes Maintenance Agents)
**Dependencies:** Epic 19.6 (Community Registry Hub)
**Stakeholders:** Sam Estrin (maintainer)

## Estimated User Story Count
**Complexity Level:** Very-Complex
**Estimated Stories:** 8

---

## Complexity & Schedule

**Complexity:** 10/12 (VERY COMPLEX)
**Timeline:** 15 days
**Phases:** 8
**Pattern:** Research & Spike → Foundation → Core Resolution → Upgrade Integration → Discovery Command → Validation Gate → Roster Reconciliation → Integration & Docs
**TDD Mode:** Strict 🔒 (all stories)
**Adversarial Review:** ENABLED 🎯
**Inline-Fix Severities:** CRITICAL/HIGH
**Execution Mode:** Gated 🚧
**Branch:** `feature/19.7_live_model_resolution`

---

## Workflow Status

**Document Status**: [x] Plan Created [x] User Stories Added [x] Acceptance Criteria Added [x] Ready for Sprint

## Sprint Tracking

**Sprint Reference:** `.planning/sprints/active/19.7_live_model_resolution/`
**Sprint Number:** 19.7
**Sprint Created:** 2026-07-08
**Sprint Status:** Created — awaiting `/refine-sprint` then `/execute-sprint`

---

## Sprint Execution Tracking

_Populated by `/execute-sprint` as phases complete._

**Execution Mode:** Gated — `/execute-sprint` stops at each phase boundary for review.

| Phase | Story | Status |
|-------|-------|--------|
| 1 | 01: Catalog Routability Spike & Stable-Channel Heuristic | ✅ Complete |
| 2 | 02: Family/Channel Binding & Resolved Lock | ✅ Complete |
| 3 | 03: Hybrid Resolver (Alias / Created-Timestamp / Explicit-Pin) | ✅ Complete |
| 4 | 04: Reproducible Upgrade with Before→After Lock Reporting | ✅ Complete |
| 5 | 05: `atcr models check` Drift Report | ✅ Complete (gated stop) |
| 6 | 06: Major-Bump Re-Validation Gate | ⬜ Not started |
| 7 | 07: init/quickstart Roster Reconciliation | ⬜ Not started |
| 8 | 08: Catalog Snapshot Fixture, Refresh Command & Documentation | ⬜ Not started |

---

**Single Source of Truth:** This metadata.md tracks Sprint 19.7 execution. The `plan/` subfolder preserves the archived planning artifacts.

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
