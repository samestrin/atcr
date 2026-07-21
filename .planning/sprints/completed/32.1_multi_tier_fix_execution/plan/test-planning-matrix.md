# Test Planning Matrix

**Generated:** 2026-07-20
**Plan:** 32.1_multi_tier_fix_execution
**Total ACs:** 12

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01: Configure a Complexity Ceiling on the Executor | 2 | 2 | 0 | 0 | Low |
| 02: Skip Over-Ceiling Findings Safely | 3 | 2 | 1 | 0 | Medium |
| 03: Run a Second Tier Over Skipped Findings | 3 | 0 | 2 | 1 | High |
| 04: Validate Ceiling Configuration | 2 | 2 | 0 | 0 | Low |
| 05: Document the Multi-Tier Workflow | 2 | 0 | 1 | 0 | Low (1 Manual) |

---

## Detailed AC List

### Story 1: Configure a Complexity Ceiling on the Executor

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | ExecutorConfig Exposes Complexity Ceiling Fields | Unit | Low | P1 |
| 01-02 | Effective-Value Resolvers Return Correct Defaults | Unit | Low | P2 |

### Story 2: Skip Over-Ceiling Findings Safely

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Ceiling-Exceeding Findings Are Skipped Before Dispatch | Unit | Medium | P1 |
| 02-02 | Self-Gating Decline Never Presents a Partial Fix as Complete | Unit | Medium | P1 |
| 02-03 | Existing Skip Chain and Failure Branches Remain Unaffected | Integration | Medium | P2 |

### Story 3: Run a Second Tier Over Skipped Findings

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Two-Tier Run Partitions Every Finding Exactly Once | Integration | High | P1 |
| 03-02 | Fix Attribution Prevents Double-Processing Across Tiers | Integration | High | P1 |
| 03-03 | Two-Tier Workflow Is Test-Verified and Reproducible | E2E | High | P2 |

### Story 4: Validate Ceiling Configuration

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | Numeric and Severity Ceiling Values Are Range-Validated | Unit | Low | P2 |
| 04-02 | Floor-Ceiling Contradiction Is Rejected at Load Time | Unit | Low | P2 |

### Story 5: Document the Multi-Tier Workflow

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | Ceiling Fields Documented in Registry and Findings-Format Docs | Manual | Low | P3 |
| 05-02 | Worked Two-Tier Example Is Valid and Runnable | Integration | Low | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 6 ACs require unit tests (01-01, 01-02, 02-01, 02-02, 04-01, 04-02)
- **Integration Tests:** 4 ACs require integration tests (02-03, 03-01, 03-02, 05-02)
- **E2E Tests:** 1 AC requires an E2E test (03-03)
- **Manual Review:** 1 AC is documentation-only, verified by manual review (05-01)
- **High Complexity:** 3 ACs marked high complexity, all in Story 3 (the two-tier partition/attribution mechanics) — flagged during AC generation as carrying a real edge-case risk (fix-attribution is scoped to the executor's `Name` field; tiers with distinct names could break the "no double-processing" guarantee assumed by this story) that `/design-sprint` should address explicitly.
