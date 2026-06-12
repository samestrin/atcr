# Test Planning Matrix

**Generated:** 2026-06-10
**Plan:** 1.0_atcr_core
**Total ACs:** 24

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01 - CLI Review Workflow | 6 | 5 | 1 | 0 | High |
| 02 - Agent Configuration | 4 | 3 | 1 | 0 | Medium |
| 03 - CI Integration | 2 | 1 | 1 | 0 | Medium |
| 04 - MCP Integration | 4 | 1 | 3 | 0 | Medium |
| 05 - Host Review via Skill | 4 | 1 | 3 | 0 | High |
| 06 - Payload Mode Selection | 4 | 3 | 1 | 0 | Medium |
| **Total** | **24** | **14** | **10** | **0** | |

---

## Detailed AC List

### Story 01: CLI Review Workflow

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | End-to-End Review | Integration | High | P1 |
| 01-02 | Git Range Resolution | Unit | High | P1 |
| 01-03 | Review Directory Structure | Unit | Medium | P1 |
| 01-04 | Fan-out Agent Execution | Unit | High | P1 |
| 01-05 | Reconciliation Pipeline | Unit | High | P1 |
| 01-06 | Report Rendering | Unit | Medium | P2 |

### Story 02: Agent Configuration

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Init Command | Integration | Medium | P1 |
| 02-02 | Provider and Agent Registry | Unit | Medium | P1 |
| 02-03 | Precedence and Validation | Unit | High | P1 |
| 02-04 | Persona Resolution and Override | Unit | Medium | P2 |

### Story 03: CI Integration

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Fail-on Severity Threshold | Unit | Medium | P1 |
| 03-02 | CI One-Shot and Example | Integration | Medium | P2 |

### Story 04: MCP Integration

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | MCP Stdio Server | Integration | Medium | P1 |
| 04-02 | Tool Registration and Schemas | Unit | Medium | P1 |
| 04-03 | Review and Reconcile Handlers | Integration | High | P1 |
| 04-04 | Report, Range, and Status Handlers | Integration | Medium | P2 |

### Story 05: Host Review via Skill

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | Skill Structure and Installation | Integration | Medium | P1 |
| 05-02 | Host Review Findings Generation | Unit | High | P1 |
| 05-03 | Orchestration Loop | Integration | High | P1 |
| 05-04 | Adversarial Review and Adjudication | Integration | High | P2 |

### Story 06: Payload Mode Selection

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 06-01 | Payload Builders | Unit | High | P1 |
| 06-02 | Payload Mode Configuration | Unit | Medium | P1 |
| 06-03 | Byte Budget and Truncation | Unit | High | P1 |
| 06-04 | Payload Templates and Documentation | Integration | Medium | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 14 ACs require unit tests
- **Integration Tests:** 10 ACs require integration tests
- **E2E Tests:** 0 ACs marked E2E (orchestration covered by integration tests)
- **High Complexity:** 8 ACs marked high complexity (01-01, 01-02, 01-04, 01-05, 02-03, 04-03, 05-02, 05-03, 05-04, 06-01, 06-03)
