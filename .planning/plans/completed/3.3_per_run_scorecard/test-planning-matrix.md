# Test Planning Matrix

**Generated:** 2026-06-15
**Plan:** 3.3_per_run_scorecard
**Total ACs:** 20

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01: Auto-emit Scorecard | 5 | 3 | 2 | 0 | Medium |
| 02: View Single-Run Scorecard | 3 | 3 | 0 | 0 | Low |
| 03: View Aggregated Leaderboard | 5 | 5 | 0 | 0 | Medium |
| 04: Export Public Leaderboard Submission | 4 | 4 | 0 | 0 | Medium |
| 05: Suppress Emission | 3 | 2 | 2 | 0 | Low |

---

## Detailed AC List

### Story 1: Auto-emit Scorecard

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | JSONL File Creation and Update | Integration | Medium | P1 |
| 01-02 | Versioned Schema Record Shape | Unit | Low | P1 |
| 01-03 | Verification-Conditional Fields | Unit | Low | P1 |
| 01-04 | --no-scorecard Flag Suppression | Integration | Low | P2 |
| 01-05 | Aggregate Run Record | Unit | Medium | P1 |

### Story 2: View Single-Run Scorecard

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Scorecard Command Resolution and Lookup | Unit | Medium | P1 |
| 02-02 | Scorecard Table Rendering and Conditional Columns | Unit | Medium | P1 |
| 02-03 | Scorecard Error Handling and Edge Case Resilience | Unit | High | P2 |

### Story 3: View Aggregated Leaderboard

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Leaderboard Ranked Table Display | Unit | Medium | P1 |
| 03-02 | Time Range Filter (--since) | Unit | Low | P1 |
| 03-03 | Model and Persona Filters | Unit | Medium | P1 |
| 03-04 | Export Versioned JSON | Unit | Medium | P2 |
| 03-05 | Graceful Empty and Missing Data Handling | Unit | High | P2 |

### Story 4: Export Public Leaderboard Submission

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | Export Command & Public Submission Schema | Unit | Medium | P1 |
| 04-02 | Anonymization Pass — PII Stripping | Unit | High | P1 |
| 04-03 | Metric Preservation & Metadata Integrity | Unit | Medium | P1 |
| 04-04 | Determinism, Filtering & Error Handling | Unit | High | P2 |

### Story 5: Suppress Emission

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | CLI Flag Registration & Help Text | Unit | Low | P1 |
| 05-02 | Suppression Gate — Zero Records Written | Unit + Integration | Medium | P1 |
| 05-03 | Default Behavior Preserved & No Side Effects | Integration | Medium | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 17 ACs require unit tests (16 pure unit + 1 mixed unit+integration)
- **Integration Tests:** 4 ACs require integration tests (3 pure integration + 1 mixed unit+integration)
- **E2E Tests:** 0 ACs require E2E tests
- **High Complexity:** 4 ACs marked high complexity (02-03, 03-05, 04-02, 04-04)
