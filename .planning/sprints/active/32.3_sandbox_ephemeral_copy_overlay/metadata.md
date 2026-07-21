# Sprint 32.3

## Plan Metadata
**Created:** 2026-07-20
**Status:** Draft - Awaiting User Stories
**Plan Number:** 32.3
**Plan Type:** feature
**Feature Category:** Sandbox / Auto-Fix Execution
**Priority:** Medium
**Assigned Team:** Backend Team
**Epic/Initiative:** Originated as Epic Plan 32.3 (routed here after exceeding /execute-epic's <=2-component scope guard; see plan/original-requirements.md)
**Dependencies:** Builds on Epic 32.0 (Sandboxed Auto-Fix Validation) and Epic 11.0 (sandbox.Backend / --exec); shares internal/sandbox/docker.go with --exec, which must remain behaviorally unchanged
**Stakeholders:** Sam Estrin

## Estimated User Story Count
**Complexity Level:** Semi-Complex
**Estimated Stories:** 5

---

## Complexity & Schedule

**Complexity:** 9/12 (COMPLEX)
**Timeline:** 8 days
**Phases:** 4
**Adversarial Review:** ENABLED 🎯
**Inline-Fix Severities:** CRITICAL/HIGH
**Execution Mode:** Gated 🚧

---

## Workflow Status

**Document Status**: [x] Plan Created [x] User Stories Added [x] Acceptance Criteria Added [x] Ready for Sprint

## Sprint Tracking

**Sprint Reference:** [sprint-plan.md](sprint-plan.md)
**Sprint Number:** 32.3
**Sprint Created:** 2026-07-21
**Sprint Status:** Ready for Execution

_Note: This section will be updated when sprint is created with `/create-sprint`_

---

**Single Source of Truth:** This metadata.md file was copied to the sprint folder and is the active tracking document during sprint execution.

---

## Execution Metrics

**Executed:** 2026-07-21
**Status:** Ready for Review

### Progress
- **Phases:** 4/4
- **User Stories:** 5/5

### Quality
- **Tests:** touched-package suites all passing (360 sub-tests, 0 failures); full `go test ./...` green
- **Coverage:** sandbox 87.2%, verify 95.3% (both ≥80%)
- **Lint:** Clean (`golangci-lint run` 0 issues, `gofmt -l` clean, `go vet` clean)

### Changes
- **Files Changed:** 8 source/docs (`internal/sandbox/{docker,sandbox,docker_test,sandbox_test}.go`, `internal/verify/{sandboxvalidate,sandboxvalidate_test,autofix_exec}.go`, `docs/auto-fix.md`)
- **Commits:** 13 on `feature/32.3_sandbox_ephemeral_copy_overlay`

### Tech Debt
- 7 open items deferred (TD-001, TD-003..TD-008) in `tech-debt-captured.md`; TD-002 resolved mid-sprint. All below the CRITICAL/HIGH inline-fix bar; pre-seeded for `/execute-code-review`.
