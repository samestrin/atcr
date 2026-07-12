# Sprint 20.0: Standalone ATCR Skill Distribution

## Plan Metadata
**Created:** 2026-07-11
**Status:** Sprint Created - Ready for Refinement
**Plan Number:** 20.0
**Plan Type:** feature
**Feature Category:** Distribution/Packaging
**Priority:** High
**Assigned Team:** Backend Team
**Epic/Initiative:** None
**Dependencies:** Epic 12.0 (Skill Integration, complete), Epic 21.0 (Release & Packaging Automation, parallel/out-of-scope for binary packaging)
**Stakeholders:** Sam

## Estimated User Story Count
**Complexity Level:** Complex
**Estimated Stories:** 5

---

## Complexity & Schedule

**Complexity:** 8/12 (COMPLEX)
**Timeline:** 8 days
**Phases:** 5
**Pattern:** Foundation → Independent Verification → Documentation & Migration → Integration → Validation
**TDD Mode:** Moderate 🔄 (auto, complexity 8)
**Adversarial Review:** ENABLED 🎯
**Inline-Fix Severities:** CRITICAL/HIGH
**Execution Mode:** Gated 🚧

---

## Workflow Status

**Document Status**: [x] Plan Created [x] User Stories Added [x] Acceptance Criteria Added [x] Sprint Created

## Sprint Tracking

**Sprint Reference:** .planning/sprints/active/20.0_standalone_skill_release/
**Sprint Number:** 20.0
**Sprint Created:** 2026-07-11
**Sprint Status:** Delivered-with-deferrals — Phase 1 (Story 1 dispatcher) complete; Phase 2 Story 2 (backend-contract test) complete & committed. **Stories 3/4/5 PUNTED to Epic 33.2 (Public Launch).** Root cause (TD-002): external `go install …/atcr@latest` is gated on the repo going PUBLIC (currently private → public proxy 404s the module; the `go.mod:41` replace directive is a second, non-binding cause). This can only land at launch. See `.planning/epics/active/33.2_public_launch.md`. Note: Epic 21.0 shares the same public-repo prerequisite.
**Branch:** feature/20.0_standalone_skill_release

_Note: This section is updated as the sprint progresses through refine → execute → review → complete → finalize._

---

**Single Source of Truth:** This metadata.md file is the active tracking document during sprint execution.

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
