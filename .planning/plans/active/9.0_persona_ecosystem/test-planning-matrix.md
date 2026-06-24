# Test Planning Matrix

**Generated:** 2026-06-24
**Plan:** 9.0_persona_ecosystem
**Total ACs:** 19

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01: Bonus Built-In Domain Personas | 4 | 3 | 1 | 0 | Medium |
| 02: Personas CLI Discovery and Lifecycle | 4 | 2 | 2 | 0 | High |
| 03: Language-Aware Skeptic Routing | 4 | 3 | 1 | 0 | High |
| 04: Domain Bundle Installation | 5 | 3 | 2 | 0 | Medium |
| 05: Corroboration Feedback | — | — | — | — | — |
| 06: In-Repo Documentation | — | — | — | — | — |

> Note: Stories 05 and 06 AC files not yet generated as of this matrix snapshot.

---

## Detailed AC List

### Story 04: Domain Bundle Installation

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | Clean Bundle Installation | Integration | Medium | P1 |
| 04-02 | Partial Bundle Install Skip Behavior | Integration | Medium | P1 |
| 04-03 | Unknown Bundle Error Handling | Unit | Low | P1 |
| 04-04 | Bundle Manifest Parse Validation | Unit | Low | P2 |
| 04-05 | Bundle Test Coverage in bundles_test.go | Unit | Medium | P1 |

---

## Test Coverage Notes

- **Unit Tests:** Story 04 has 3 ACs requiring unit tests (04-03, 04-04, 04-05)
- **Integration Tests:** Story 04 has 2 ACs requiring integration tests (04-01, 04-02)
- **E2E Tests:** 0 ACs in Story 04 require E2E tests
- **High Complexity:** 0 ACs in Story 04 marked high complexity
- **Key Test Infrastructure:** `httptest.NewServer` for community-repo fetch simulation; `t.TempDir()` for isolated config dir per test; `go test -race` required for all bundle tests
