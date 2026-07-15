# Sprint 25.0: SARIF Output Integration

**Metadata:** See [metadata.md](metadata.md)
**Sprint Plan:** [sprint-plan.md](sprint-plan.md)
**Plan Type:** ✨ Feature

---

## Overview

ATCR gains a fourth render target — SARIF 2.1.0 JSON — via `atcr report --format=sarif`, so review findings can feed GitHub Advanced Security's Code Scanning "Security" tab and GitLab CI's native SAST report widget. These are the two centralized, cross-repo security surfaces ATCR's existing `atcr github` PR-check/inline-comment integration does not reach.

## Timeline

**Complexity:** 6/12 (MODERATE)
**Estimated Duration:** 5 days
**Phases:** 4

| Phase | Focus | Duration |
|-------|-------|----------|
| 1 | Foundation — SARIF Document Structure | 1.5 days |
| 2 | Severity & Anchoring | 1.5 days |
| 3 | Integration — CLI/MCP Parity & CI Documentation | 1 day |
| 4 | Validation | 1 day |

## Expected Outcomes

- `atcr report --format=sarif` produces schema-valid SARIF 2.1.0 JSON, validated against a local schema fixture via `google/jsonschema-go`.
- ATCR severities (CRITICAL, HIGH, MEDIUM, LOW) map deterministically to SARIF levels (`error`/`warning`/`note`) through a single rubric call site (`sarifLevel`), reusing `reconcile.NormalizeSeverity`/`SeverityRank` exclusively.
- Every finding — line-level or file-level — anchors and renders in GitHub's Security tab, using a synthesized `1,1,1,1` fallback region for `Line<=0` findings.
- `docs/ci-integration.md` documents both the GitHub Code Scanning upload path and the GitLab CI SAST-widget equivalent, explicitly distinguished from the already-shipped `atcr github` flow.

## Risk Summary (Top 3)

1. **Hand-rolled SARIF struct tree drifts from the SARIF 2.1.0 spec.** Mitigated by schema-validating `renderSarif` output in tests against a local schema fixture (`google/jsonschema-go`), plus a manual upload smoke-test to a scratch repo's Code Scanning tab before marking the plan's AC1 done.
2. **A future edit reimplements severity comparison locally, silently creating a second rubric copy (the TD-0052 failure mode).** Mitigated by keeping `sarifLevel` the sole severity-comparison site in `sarif.go`, verified in code review and by a test asserting `renderSarif`'s per-result `level` matches `sarifLevel`'s own output.
3. **`Line<=0` boundary mis-implemented (e.g. only checking `Line==0`, missing negative values).** Mitigated by an explicit, separate table-driven test case for `Line<0` (not collapsed with `Line==0`) — the single highest-value regression test in this sprint.

## Sprint Assets

- [sprint-plan.md](sprint-plan.md) — Phase-by-phase TDD task breakdown
- [metadata.md](metadata.md) — Tracking document
- [sprint-knowledge.yaml](sprint-knowledge.yaml) — Knowledge manifest
- [plan/](plan/) — Original plan, user stories, acceptance criteria, sprint design, documentation
