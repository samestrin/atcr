# Test Planning Matrix

**Generated:** 2026-06-14
**Plan:** 3.0_adversarial_verification
**Total ACs:** 26

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01 - Skeptic Selection & Role Plumbing | 5 | 4 | 1 | 0 | Medium |
| 02 - Skeptic Invocation & Verdict Parsing | 6 | 4 | 2 | 0 | High |
| 03 - Confidence v2 & Re-emit | 5 | 3 | 2 | 0 | Medium |
| 04 - CLI Command & MCP Tool | 4 | 1 | 3 | 0 | Medium |
| 05 - Gate Semantics | 2 | 1 | 1 | 0 | Low |
| 06 - Report Updates & Documentation | 4 | 2 | 2 | 0 | Medium |
| **Total** | **26** | **15** | **11** | **0** | — |

---

## Detailed AC List

### Story 1: Skeptic Selection & Role Plumbing

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | AgentsByRole Filtering | Unit | Medium | P1 |
| 01-02 | Different Model Exclusion | Unit | High | P1 |
| 01-03 | Empty Selection → Unverifiable | Unit | Low | P1 |
| 01-04 | Empty Role Backward Compatibility | Unit | Medium | P2 |
| 01-05 | Test Coverage Requirements | Integration | Medium | P2 |

### Story 2: Skeptic Invocation & Verdict Parsing

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Skeptic Prompt Construction | Unit | Medium | P1 |
| 02-02 | Verdict Parsing | Unit | High | P1 |
| 02-03 | Skeptic Invocation | Integration | High | P1 |
| 02-04 | Failure Isolation | Unit | Medium | P1 |
| 02-05 | Budget Forwarding | Unit | Medium | P2 |
| 02-06 | Test Coverage | Integration | Medium | P2 |

### Story 3: Confidence v2 & Re-emit

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Confidence v2 Recomputation | Unit | Medium | P1 |
| 03-02 | Verification JSON Emission | Integration | Medium | P1 |
| 03-03 | Findings Re-emit | Integration | High | P1 |
| 03-04 | Manifest & Summary Updates | Unit | Low | P2 |
| 03-05 | Gate Excludes Refuted | Unit | Medium | P1 |

### Story 4: CLI Command & MCP Tool

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | Verify Subcommand | Integration | Medium | P1 |
| 04-02 | Review --verify Chaining | Integration | Medium | P1 |
| 04-03 | MCP Verify Tool | Integration | Medium | P1 |
| 04-04 | Artifact Consistency & Error Handling | Integration | High | P2 |

### Story 5: Gate Semantics

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | Gate Filtering & --require-verified | Unit | Medium | P1 |
| 05-02 | MCP Parity & Matrix Tests | Integration | High | P1 |

### Story 6: Report Updates & Documentation

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 06-01 | Report Rendering with Verification | Integration | High | P1 |
| 06-02 | Backward Compatibility (v1) | Unit | Low | P1 |
| 06-03 | Verification Documentation | Unit | Low | P2 |
| 06-04 | Verification Fixture Corpus | Integration | High | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 15 ACs require unit tests
- **Integration Tests:** 11 ACs require integration tests
- **E2E Tests:** 0 ACs require E2E tests
- **High Complexity:** 6 ACs marked high complexity (01-02, 02-02, 02-03, 03-03, 04-04, 06-01, 06-04)
