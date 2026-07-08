# Test Planning Matrix

**Generated:** 2026-07-07
**Plan:** 19.6_community_registry_hub
**Total ACs:** 29

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01: Community-Canonical Fetch-and-Pin Distribution | 5 | 1 | 3 | 1 | High |
| 02: Structured Model Metadata Schema | 3 | 3 | 0 | 0 | Medium |
| 03: Model-Aware Search and Discovery | 4 | 1 | 3 | 0 | Medium |
| 04: Model-Indexed Persona Library Authoring | 7 | 5 | 2 | 0 | High |
| 05: Human-Names Migration for Built-in Stragglers | 4 | 3 | 1 | 0 | Medium |
| 06: Authoring Contract Enforcement | 3 | 2 | 1 | 0 | Low |
| 07: Onboarding-Hierarchy Documentation | 3 | 0 | 0 | 0 | Low |

---

## Detailed AC List

### Story 01: Community-Canonical Fetch-and-Pin Distribution

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | Registry Base URL Repoint | Unit | Low | P1 |
| 01-02 | Init/Quickstart Fetch-and-Pin | Integration | High | P1 |
| 01-03 | Offline Flag Fallback | Integration | Medium | P2 |
| 01-04 | Fetch Failure Error Handling | Integration | Medium | P1 |
| 01-05 | Preserve Existing Personas and Source Labeling | E2E | High | P1 |

### Story 02: Structured Model Metadata Schema

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Persona Index Entry Schema Extension | Unit | Low | P1 |
| 02-02 | Index.json Field Population Contract | Unit | Medium | P1 |
| 02-03 | Backward-Compatible Decode Test | Unit | Medium | P1 |

### Story 03: Model-Aware Search and Discovery

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Structured Model/Provider Filtering | Integration | Medium | P1 |
| 03-02 | Keyword Search Backward Compatibility | Unit | Low | P1 |
| 03-03 | Flag Registration and Arg Validation | Integration | Low | P2 |
| 03-04 | Search Table Provider/Model Columns | Integration | Low | P2 |

### Story 04: Model-Indexed Persona Library Authoring

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | Frontier Flagship+Fallback Persona Pairs | Unit | High | P1 |
| 04-02 | Flat-Rate Open Model Personas | Unit | High | P1 |
| 04-03 | Vendor-Grounded Prompt Structure Compliance | Unit | Medium | P1 |
| 04-04 | Fixture Authoring and Fixture Test Pass | Integration | Medium | P1 |
| 04-05 | Community Index Registration | Unit | Medium | P1 |
| 04-06 | Strict Schema and Naming Compliance | Unit | Medium | P1 |
| 04-07 | Model-Appropriate Task Scoping Differentiation | Integration | High | P2 |

### Story 05: Human-Names Migration for Built-in Stragglers

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | Atomic Rename: sentinel→sasha, tracer→penny | Unit | Medium | P1 |
| 05-02 | Ingrid Generalized Idiomatic Lens | Unit | Medium | P1 |
| 05-03 | Retired Slug Verification | Integration | Low | P1 |
| 05-04 | Documentation Updates | Unit | Low | P2 |

### Story 06: Authoring Contract Enforcement

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 06-01 | Model-in-Structured-Metadata Convention Documented | Unit | Low | P2 |
| 06-02 | All-Human-Names Convention Documented | Unit | Low | P2 |
| 06-03 | Fixture Test Asserts Bound Model Metadata | Integration | Medium | P1 |

### Story 07: Onboarding-Hierarchy Documentation

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 07-01 | README Quickstart Hierarchy Rewrite | Manual | Low | P1 |
| 07-02 | personas-install.md Tier Detail and Discover Flow | Manual | Low | P1 |
| 07-03 | personas-authoring.md Discover-by-Model Cross-Reference | Manual | Low | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 15 ACs require unit tests
- **Integration Tests:** 11 ACs require integration tests
- **E2E Tests:** 1 AC requires E2E tests (01-05, mock-registry end-to-end)
- **Manual/Documentation Review:** 3 ACs (Story 07, doc-content only, no code path)
- **High Complexity:** 5 ACs marked high complexity (01-02, 01-05, 04-01, 04-02, 04-07)
