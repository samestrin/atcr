# Test Planning Matrix

**Generated:** 2026-07-10
**Plan:** 19.9_community_prompt_submissions
**Total ACs:** 15

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Manual | Complexity |
|-------|-----|------|-------------|-----|--------|------------|
| 01 — Local Fixture-Gate Reuse and Submission Blocking | 3 | 3 | 0 | 0 | 0 | Medium |
| 02 — Fork + PR Automation via `gh` | 3 | 3 | 0 | 0 | 0 | High |
| 03 — `submitted` Status Distinct from `Source`/Provenance | 3 | 2 | 1 | 0 | 0 | Medium |
| 04 — Maintainer Graduation into the Vetted Library | 3 | 0 | 0 | 0 | 3 | Low |
| 05 — Documentation of the Submit Flow and Two-Tier Model | 3 | 0 | 0 | 0 | 3 | Low |

---

## Detailed AC List

### Story 01: Local Fixture-Gate Reuse and Submission Blocking

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | Invalid Persona Name Rejection | Unit | Low | P1 |
| 01-02 | Missing Fixture Blocks Submission | Unit | Low | P1 |
| 01-03 | Fixture Gate Pass/Fail Evaluation Gates Progression | Unit | Medium | P1 |

### Story 02: Fork + PR Automation via `gh`

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | `gh` Precondition Check (PATH + Auth) Before Any Fork/Branch Work | Unit | Medium | P1 |
| 02-02 | Fork, Branch/Push, and PR Create with PR URL Reported to the User | Unit (seam-stubbed) | High | P1 |
| 02-03 | Injectable `gh` Seam Matching `personasClient`/`personasFixtureRunner` Conventions | Unit | Medium | P2 |

### Story 03: `submitted` Status Distinct from `Source`/Provenance

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | `submitted` Status Is Not a Fourth `Source` Value | Unit | Medium | P1 |
| 03-02 | `submitted` Marker Carries Attribution and Persists Only via Atomic Write | Unit | Medium | P1 |
| 03-03 | Marker Lives Outside `personas/community/` and `List` Extension Point Leaves Existing Output Unchanged | Integration | Medium | P2 |

### Story 04: Maintainer Graduation into the Vetted Library

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | Documented Persona Placement and Index Entry Creation | Manual | Low | P2 |
| 04-02 | Submitted Marker Clearing Without Touching Source | Manual | Low | P2 |
| 04-03 | Manual PR-Native Process with Contribution Checklist Cross-Reference | Manual | Low | P3 |

### Story 05: Documentation of the Submit Flow and Two-Tier Model

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | `atcr personas submit <name>` Documented as the Seventh Subcommand | Manual | Low | P2 |
| 05-02 | Contribution Checklist Cross-References `atcr personas submit` | Manual | Low | P3 |
| 05-03 | New Section Explains the `submitted` → Graduated Two-Tier Model | Manual | Low | P3 |

---

## Test Coverage Notes

- **Unit Tests:** 8 ACs require unit tests (01-01, 01-02, 01-03, 02-01, 02-02, 02-03, 03-01, 03-02)
- **Integration Tests:** 1 AC requires an integration-flavored test (03-03, exercising the full `personas list` CLI output path)
- **E2E Tests:** 0 ACs require E2E tests — the `gh` fork/PR flow is validated via injectable-seam unit tests, not live GitHub calls (per 02-03)
- **Manual Review:** 6 ACs are documentation-only (04-01, 04-02, 04-03, 05-01, 05-02, 05-03), verified by doc review rather than automated tests
- **High Complexity:** 1 AC marked high complexity (02-02 — fork/branch/push/PR-create sequencing)
