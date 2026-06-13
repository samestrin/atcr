# Test Planning Matrix

**Generated:** 2026-06-13
**Plan:** 2.0_tool_using_reviewers
**Total ACs:** 22

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01: Agent Loop Execution | 6 | 1 | 5 | 0 | High |
| 02: Budget Enforcement | 4 | 0 | 4 | 0 | Medium |
| 03: Path Jail & Snapshot Sandbox | 4 | 2 | 2 | 0 | High |
| 04: Graceful Degradation | 4 | 2 | 2 | 0 | Medium |
| 05: Transcript & Accounting | 4 | 3 | 1 | 0 | Medium |

---

## Detailed AC List

### Story 1: Agent Loop Execution

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | ChatCompleter Interface & Wire Format | Integration | High | P1 |
| 01-02 | Multi-Turn Agent Loop | Integration | High | P1 |
| 01-03 | Per-Agent Budget Enforcement | Integration | High | P1 |
| 01-04 | Loop Hygiene | Integration | Medium | P1 |
| 01-05 | Degrade Path & Fallback Inheritance | Integration | Medium | P1 |
| 01-06 | Result Accounting & Backward Compat | Unit | Low | P2 |

### Story 2: Budget Enforcement

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Turn Budget Enforcement | Integration | Medium | P1 |
| 02-02 | Tool Byte Budget Enforcement | Integration | Medium | P1 |
| 02-03 | Timeout Enforcement | Integration | Medium | P1 |
| 02-04 | Budget Status Reporting & Partial Success | Integration | Medium | P2 |

### Story 3: Path Jail & Snapshot Sandbox

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Path Jail Escape Vector Rejection | Unit | High | P1 |
| 03-02 | Snapshot Manager Lifecycle | Integration | High | P1 |
| 03-03 | Worktree Cleanup & Manifest Recording | Integration | Medium | P1 |
| 03-04 | Read-Only Enforcement & Write Tool Guard | Unit | Medium | P1 |

### Story 4: Graceful Degradation

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | Single-Shot Degradation Path | Unit | Medium | P1 |
| 04-02 | Tool-Capable Agent Loop Path | Unit | Medium | P1 |
| 04-03 | Fallback Degradation Inheritance | Integration | Medium | P2 |
| 04-04 | Mixed Roster Reconciler Compatibility | Integration | High | P2 |

### Story 5: Transcript & Accounting

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | Transcript Event Emission | Unit | Medium | P1 |
| 05-02 | Transcript Durability & Replay | Integration | Medium | P1 |
| 05-03 | Live Status Counters | Unit | Low | P2 |
| 05-04 | Manifest Review Stage Entry | Unit | Low | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 8 ACs require unit tests
- **Integration Tests:** 14 ACs require integration tests
- **E2E Tests:** 0 ACs require E2E tests
- **High Complexity:** 6 ACs marked high complexity
