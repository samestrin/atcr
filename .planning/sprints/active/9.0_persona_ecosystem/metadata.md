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

**Current Phase:** Phase 4 complete — gated stop before Phase 5 (Sprint B, Story 02 done)
**Phases Complete:** 4 / 6
**Last Checkpoint:** 2026-06-24 — Phase 4 (T2 `atcr personas` CLI) green; gate passed (PASS on all 5 checklist items, no CRITICAL/HIGH/MEDIUM). New `internal/personas` package (install/list/search/remove/test/upgrade) + `cmd/atcr/personas.go` 6 subcommands; root now 15 subcommands; `registry.ValidateAgentYAML` validates fetched YAML before write; path-traversal guarded; zero live network (httptest). Coverage: internal/personas 84.4%, cmd/atcr 84.1%. Adversarial 4.2.A/4.4.A: no CRITICAL/HIGH; TD-007…TD-012 captured (2 MEDIUM, 4 LOW). Commits: 82e808c GREEN(core), d5d31c9 GREEN(cmd, atomic 14→15), e623c39 refactor(test gaps). golang.org/x/mod added for semver.

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
