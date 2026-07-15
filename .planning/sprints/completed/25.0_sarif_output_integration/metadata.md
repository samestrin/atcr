# Sprint 25.0: SARIF Output Integration

## Plan Metadata
**Created:** 2026-07-14
**Status:** Sprint Created
**Plan Number:** 25.0
**Plan Type:** feature
**Feature Category:** CI/Security Integration
**Priority:** Medium
**Assigned Team:** Core CLI
**Epic/Initiative:** Epic 25.0: SARIF Output Integration
**Dependencies:** None
**Stakeholders:** ATCR maintainers, enterprise CI/security integrators

## Estimated User Story Count
**Complexity Level:** Semi-Complex
**Estimated Stories:** 4

---

## Complexity & Schedule

**Complexity:** 6/12 (MODERATE)
**Timeline:** 5 days
**Phases:** 4
**Adversarial Review:** ENABLED 🎯
**Inline-Fix Severities:** CRITICAL/HIGH
**Execution Mode:** Continuous

---

## Workflow Status

**Document Status**: [x] Plan Created [x] User Stories Added [x] Acceptance Criteria Added [x] Ready for Sprint

## Sprint Tracking

**Sprint Reference:** [sprint-plan.md](sprint-plan.md)
**Sprint Number:** 25.0
**Sprint Created:** July 14, 2026 05:02:57PM
**Sprint Status:** PASS
**Sprint Completed:** 2026-07-14
**Review Summary:** Code review Pass (19/19 tasks, 4/4 stories); TD reconciliation all 14 issues resolved (2 deferred); alignment EXCELLENT (4/4 requirements delivered); TDD Excellent; audit PASS.

---

**Single Source of Truth:** This metadata.md file is the active tracking document during sprint execution.

---

## Execution Metrics

**Status:** Ready for Review
**Executed:** 2026-07-14

### Progress
- **Phases:** 4/4 (Foundation, Severity & Anchoring, Integration, Validation) — including 4.3 live GitHub Code Scanning smoke test (PASSED on samestrin/scratch)
- **Work Items:** 4/4 user stories; 19/19 sprint tasks

### Quality
- **Tests:** full suite PASS (`go test ./...`); `internal/report` includes SARIF unit + golden + SARIF 2.1.0 schema-conformance tests
- **Coverage:** `internal/report` 97.6% (≥80% threshold); all sarif.go helpers 100%, renderSarif 88.9% (only the defensively-unreachable marshal-error branch uncovered)
- **Lint:** Clean (`golangci-lint run` → 0 issues; `go vet ./...` clean)

### Changes
- **Files Changed:** 10 code/doc files (sarif.go new, render.go, report.go, ci-integration.md, + tests/fixtures)
- **Commits:** 8 on `feature/25.0_sarif_output_integration`

### Tech Debt Captured
- TD-001 (MEDIUM): empty `File` → empty `artifactLocation.uri` may break GitHub ingestion (AC-mandated pass-through)
- TD-002 (LOW): empty `Category` → blank `ruleId`/rule id (AC-mandated pass-through)
- TD-003 (MEDIUM): MCP report tool transport enum excludes `sarif` — **RESOLVED 2026-07-14** (enum + doc strings + tests updated; over-the-wire parity now tested)
