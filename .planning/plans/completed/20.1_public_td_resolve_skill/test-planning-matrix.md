# Test Planning Matrix

**Generated:** 2026-07-11
**Plan:** 20.1_public_td_resolve_skill
**Total ACs:** 19

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01: Local TD Store Persistence | 4 | 4 | 0 | 0 | Medium |
| 02: Reconcile-Time Persistence Hook | 3 | 0 | 3 | 0 | Medium |
| 03: `/atcr debt resolve` Skill Route | 6 | 3 | 0 | 3 | High |
| 04: Shared Skill Conventions Extraction | 3 | 3 | 0 | 0 | Low |
| 05: Document Debt-Resolve in skill-usage.md | 3 | 3 | 0 | 0 | Low |

---

## Detailed AC List

### Story 01: Local TD Store Persistence

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | Package Structure and Store Operations | Unit | Medium | P1 |
| 01-02 | Record Identity via FindingID Reuse | Unit | Low | P1 |
| 01-03 | Tolerant Read Path (Malformed Lines and Schema Versioning) | Unit | Medium | P2 |
| 01-04 | Concurrency Guarantee and Package Documentation | Unit | Low | P2 |

### Story 02: Reconcile-Time Persistence Hook

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Persist Reconciled Findings Into the Local TD Store | Integration | Medium | P1 |
| 02-02 | `--no-local-debt` Opt-Out Flag | Integration | Low | P2 |
| 02-03 | Cross-Run Accumulation With Write-Time Dedup | Integration | High | P1 |

### Story 03: `/atcr debt resolve` Skill Route

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | SKILL.md Dispatcher Documentation for `/atcr debt resolve` | Unit | Low | P2 |
| 03-02 | `atcr debt resolve` CLI Subcommand | Unit | Medium | P1 |
| 03-03 | Local Store Item Selection and Justification/SourceReport Consumption | E2E | High | P1 |
| 03-04 | RED→GREEN→ADVERSARIAL→REFACTOR Resolution Cycle | E2E | High | P1 |
| 03-05 | Resolution Outcome Persistence and Branch Safety | E2E | High | P1 |
| 03-06 | Go Embed Wiring and Test Coverage for `skill/debt-resolve/SKILL.md` | Unit | Low | P2 |

### Story 04: Shared Skill Conventions Extraction

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | skill/CONVENTIONS.md Creation | Unit | Low | P2 |
| 04-02 | skill/SKILL.md Prerequisites Section Rewritten to Point to CONVENTIONS.md | Unit | Low | P2 |
| 04-03 | skill/CONVENTIONS.md Embedded in skill.go as ConventionsMD with Test Coverage | Unit | Low | P3 |

### Story 05: Document Debt-Resolve in skill-usage.md

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | `/atcr debt resolve` Route Documentation | Unit | Low | P2 |
| 05-02 | Local `.atcr/`-Scoped TD Store Documentation | Unit | Low | P2 |
| 05-03 | Public/Local vs. Private `.planning/`-Scoped Debt Disambiguation | Unit | Low | P3 |

---

## Test Coverage Notes

- **Unit Tests:** 13 ACs require unit tests (mostly `go test` string/embed assertions across `internal/localdebt`, `skill/skill_test.go`, and doc-presence checks mirroring `internal/scorecard/docs_test.go`)
- **Integration Tests:** 3 ACs require integration tests (all in Story 02, exercising the full `atcr reconcile` CLI against real temp `.atcr/debt/` files, following the `cmd/atcr/reconcile_test.go` style)
- **E2E Tests:** 3 ACs require E2E-style scenario walkthroughs (all in Story 03, since the RED→GREEN→ADVERSARIAL→REFACTOR resolution cycle and item-selection/justification-consumption behavior is Markdown-driven agent logic that `go test` cannot exercise directly — these require agent-driven fixture-repo walkthroughs)
- **High Complexity:** 4 ACs marked high complexity (02-03 cross-run dedup; 03-03, 03-04, 03-05 — the core autonomous resolution cycle, its item-selection logic, and its outcome-persistence/branch-safety behavior)
