# Test Planning Matrix

**Generated:** 2026-07-11
**Plan:** 20.0_standalone_skill_release
**Total ACs:** 17

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Manual | Complexity |
|-------|-----|------|-------------|-----|--------|------------|
| 01 - Dispatcher Skill Rewrite | 5 | 4 | 0 | 0 | 1 | High |
| 02 - Backend Contract Backward-Compatibility Test | 3 | 0 | 3 | 0 | 0 | Medium |
| 03 - Install Script | 3 | 0 | 2 | 1 | 0 | Low |
| 04 - Documentation Accuracy Pass | 3 | 0 | 0 | 0 | 3 | Low |
| 05 - External Migration Descope Note | 3 | 0 | 0 | 0 | 3 | Low |
| **Total** | **17** | **4** | **5** | **1** | **7** | — |

---

## Detailed AC List

### Story 01: Dispatcher Skill Rewrite

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | Dispatcher Command Routing Table | Unit | High | P1 |
| 01-02 | Review Orchestration Flow Preserved Through the Dispatcher | Unit | High | P1 |
| 01-03 | Secondary Files Verbatim Content Split | Unit | High | P1 |
| 01-04 | Frontmatter Validity and SKILL.md Line-Budget Constraints | Unit | Medium | P1 |
| 01-05 | `docs/skill-usage.md` Consistency With the Dispatcher Rewrite | Manual | Low | P2 |

### Story 02: Backend Contract Backward-Compatibility Test

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Output Tree Contract Assertion | Integration | Medium | P1 |
| 02-02 | Id-or-Path Resolution Table-Driven Coverage | Unit/Integration | Medium | P1 |
| 02-03 | Hermetic Provider Mocking and Git Fixture Isolation | Integration | Medium | P1 |

### Story 03: Install Script

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Install Script Core Installation Flow | Integration | Low | P2 |
| 03-02 | Install Script Prerequisite and PATH Checks | Integration | Low | P2 |
| 03-03 | README Quickstart Documentation for install.sh | E2E | Low | P2 |

### Story 04: Documentation Accuracy Pass

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | `docs/skill-usage.md` Accuracy Against the Dispatcher Rewrite | Manual | Low | P2 |
| 04-02 | `docs/code-review-backend.md` `--output-dir` Contract Accuracy | Manual | Low | P2 |
| 04-03 | `README.md` Command Accuracy and `install.sh` Cross-Link | Manual | Low | P2 |

### Story 05: External Migration Descope Note

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | External Migration Doc Existence and Rationale | Manual | Low | P3 |
| 05-02 | Manual Migration Checklist and Discoverability | Manual | Low | P3 |
| 05-03 | Scope Containment — No External-Repo Writes | Manual | Low | P3 |

---

## Test Coverage Notes

- **Unit Tests:** 4 ACs require unit tests (all in Story 01, exercising Go `testing` assertions over the embedded `SkillMD` string and secondary-file constants).
- **Integration Tests:** 5 ACs require integration tests (Story 02's in-process CLI/reconcile contract coverage; Story 03's install-script installation and PATH-check flows).
- **E2E Tests:** 1 AC requires E2E coverage (Story 03's README Quickstart documentation walkthrough).
- **Manual Review:** 7 ACs are documentation/scope-verification items with no automated assertion (Story 01's doc-consistency check, all of Story 04's documentation accuracy pass, and all of Story 05's external-migration descope note).
- **High Complexity:** 4 ACs marked high complexity, all in Story 01 (Dispatcher Skill Rewrite) — the plan's highest-risk item since it changes `skill/SKILL.md`'s user-facing entry surface while an existing test suite (`skill/skill_test.go`) currently asserts content that must move to secondary files.
