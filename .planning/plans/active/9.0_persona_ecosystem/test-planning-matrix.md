# Test Planning Matrix

**Generated:** 2026-06-24
**Plan:** 9.0_persona_ecosystem
**Total ACs:** 26

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Manual | Complexity |
|-------|-----|------|-------------|-----|--------|------------|
| 01: Bonus Built-In Domain Personas | 3 | 3 | 1 | 0 | 0 | Medium |
| 02: Personas CLI Discovery and Lifecycle | 6 | 3 | 5 | 0 | 0 | High |
| 03: Language-Aware Skeptic Routing | 5 | 4 | 2 | 0 | 0 | High |
| 04: Domain Bundles | 5 | 3 | 2 | 0 | 0 | Medium |
| 05: Corroboration Feedback | 4 | 4 | 1 | 0 | 0 | Medium |
| 06: In-Repo Documentation | 3 | 1 | 0 | 0 | 2 | Low |
| **Total** | **26** | **18** | **11** | **0** | **2** | — |

---

## Detailed AC List

### Story 01: Bonus Built-In Domain Personas

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | Names Registry Returns Nine | Unit | Medium | P1 |
| 01-02 | Bonus Persona Prompt Content | Unit | Medium | P1 |
| 01-03 | Fixture CI Tests (No Network) | Unit + Integration | Medium | P1 |

### Story 02: Personas CLI: Discovery and Lifecycle

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Install Persona from Community Repo | Integration | High | P1 |
| 02-02 | List Installed Personas | Unit + Integration | Medium | P1 |
| 02-03 | Search Community Repo Index | Unit + Integration | Medium | P2 |
| 02-04 | Remove Installed Persona | Unit | Medium | P1 |
| 02-05 | Test Persona Fixture | Unit + Integration | High | P2 |
| 02-06 | Upgrade Installed Personas | Integration | High | P2 |

### Story 03: Language-Aware Skeptic Routing

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | AgentConfig Language Field | Unit | Medium | P1 |
| 03-02 | SelectEligibleSkeptics Language Routing | Unit | High | P1 |
| 03-03 | Pipeline Caller Signature Update | Integration | Medium | P1 |
| 03-04 | Silent Fallback When No Language Match | Unit | Medium | P1 |
| 03-05 | Registry YAML Backward Compatibility | Unit + Integration | High | P1 |

### Story 04: Domain Bundles

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | Clean Bundle Installation | Integration | Medium | P1 |
| 04-02 | Partial Bundle Install Skip Behavior | Integration | Medium | P2 |
| 04-03 | Unknown Bundle Error Handling | Unit | Low | P1 |
| 04-04 | Bundle Manifest Parse Validation | Unit | Medium | P1 |
| 04-05 | Bundle Test Coverage | Unit | Medium | P2 |

### Story 05: Corroboration Feedback via Persona Scores

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | Baseline List No Regression | Unit | Low | P1 |
| 05-02 | Scores Column Display | Unit + Integration | Medium | P1 |
| 05-03 | Sort Ordering | Unit | Medium | P2 |
| 05-04 | Help Documentation | Unit | Low | P3 |

### Story 06: In-Repo Documentation

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 06-01 | Personas Installation Guide | Manual | Low | P2 |
| 06-02 | Personas Authoring Guide | Manual | Low | P2 |
| 06-03 | Registry and Example File Updates | Unit | Low | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 18 ACs require unit tests (go test / testify)
- **Integration Tests:** 11 ACs require integration tests (httptest.NewServer, filesystem, scorecard JSONL)
- **E2E Tests:** 0 ACs require E2E tests
- **Manual Review:** 2 ACs require manual review (documentation completeness)
- **High Complexity:** 5 ACs marked high complexity (02-01, 02-05, 02-06, 03-02, 03-05)
- **P1 Priority:** 15 ACs are P1 (must pass before sprint closes)
