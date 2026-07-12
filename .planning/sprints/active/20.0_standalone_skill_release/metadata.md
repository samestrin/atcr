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

**Executed:** 2026-07-11
**Status:** Delivered with deferrals — Stories 1, 2, 4, 5 complete; Story 3 (+ AC04-03 install.sh link) deferred to Epic 33.2 (repo must go public first).

### Progress
- **Stories:** 4/5 delivered (1 dispatcher, 2 backend-contract test, 4 doc-accuracy, 5 migration note); Story 3 (install.sh) → 33.2.
- **Acceptance Criteria:** 14/17 closed (01-01…05, 02-01…03, 04-01/02, 05-01…03). Deferred to 33.2: 03-01…03 (install.sh) + 04-03 (README install.sh cross-link).

### Quality
- **Tests:** `go test ./...` green (40 packages, exit 0). Story 2 added `cmd/atcr/backend_contract_test.go` (locks the documented `--output-dir` + reconcile output tree).
- **Lint/Vet/Build:** `golangci-lint` (pre-push), `go vet ./...`, `go build ./...` all clean.
- **Command surface:** dispatcher routing table == `newRootCmd` (22 commands, zero drift).

### Adversarial Review
- Story 1: Phase 1 per-task + gate reviews (passed). Story 2: 2.2.A found 1 HIGH + 2 LOW, all fixed (full documented tree now asserted). Stories 4/5: 3.3 passed (NONE).

### Deferred (Epic 33.2 — Public Launch)
- Story 3 (install.sh + real `go install` integration test) and AC04-03's install.sh doc link — gated on the repo going public. Reconcile-publish + `go.mod` mechanics prototyped end-to-end (see TD-002). TD-003: README command-table completeness → Epic 33.0.

### Changes
- **Files Changed (this sprint's work):** `cmd/atcr/backend_contract_test.go` (new), `docs/external-migration.md` (new), `docs/README.md`, plus Phase 1's `skill/*` (Story 1) and planning artifacts.
- **Commits:** Story 2 (green + refactor), Stories 4/5 docs, planning (punt + 33.0/33.2 epics).
