# Sprint 22.2

## Plan Metadata
**Created:** 2026-07-13
**Status:** Sprint Created - Ready for Execution
**Plan Number:** 22.2
**Plan Type:** tech-debt
**Feature Category:** Internal Tooling / Wasm Build Infrastructure
**Priority:** Medium
**Assigned Team:** Backend Team
**Epic/Initiative:** .planning/epics/active/22.2_astgroup_shared_guest_abi.md (source epic; superseded by this full-pipeline plan per /execute-epic scope-guard triage)
**Dependencies:** None
**Stakeholders:** Sam Estrin

## Complexity & Schedule
**Complexity:** 3/12 (SIMPLE)
**Timeline:** 1 day
**Phases:** 3
**Adversarial Review:** ENABLED 🎯
**Inline-Fix Severities:** CRITICAL/HIGH
**Execution Mode:** Continuous

## Estimated User Story Count
**Complexity Level:** Simple
**Estimated Stories:** 0 (tech-debt plan type — task-based, not story-based)

---

## Workflow Status

**Document Status**: [x] Plan Created [x] Sprint Created [ ] Sprint Executed [ ] Sprint Complete

## Sprint Tracking

**Sprint Reference:** [sprint-plan.md](sprint-plan.md)
**Sprint Number:** 22.2
**Sprint Created:** 2026-07-13
**Sprint Status:** Ready for Execution

---

**Single Source of Truth:** This metadata.md file is the active tracking document during sprint execution.

---

## Execution Metrics

**Status:** Ready for Review
**Executed:** 2026-07-13

### Progress
- **Phases:** 3/3
- **Tasks:** 3/3

### Quality
- **Tests:** `go test ./...` all passing (internal/astgroup suite incl. `TestEmbeddedParsersMatchManifest` green against regenerated `.wasm`)
- **Coverage:** 88.9% (≥80% baseline; wasip1-only parser modules excluded from root instrumentation)
- **Lint:** Clean (`golangci-lint run` → 0 issues; `go vet ./...` clean; `gofmt -l` clean)

### Changes
- **Files Changed:** 20 (2 guestabi + 4 goparser/pyparser + 2 braceparser + build.sh + 10 `.wasm` + SHA256SUMS)
- **Commits:** 3

### Deferred Tech Debt
6 non-blocking findings from adversarial review captured in `tech-debt-captured.md` (TD-001..006): all MEDIUM/LOW, inherited or by-design, below the CRITICAL/HIGH inline-fix bar. Pre-seeded for `/execute-code-review`.
