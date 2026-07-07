# Test Planning Matrix

**Generated:** 2026-07-06
**Plan:** 19.6_community_registry_hub
**Total ACs:** 9

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Manual | Complexity |
|-------|-----|------|-------------|-----|--------|------------|
| 01: Author Model-Tuned Persona Content | 5 | 0 | 0 | 0 | 5 | Medium |
| 02: Publish Personas to Community Index | 2 | 0 | 0 | 0 | 2 | Low |
| 03: Recommend Default Persona Pack in Documentation | 2 | 0 | 0 | 0 | 2 | Low |

**Note:** All 9 ACs are MANUAL because this plan's actual code/content changes (Stories 1-2) land in the external `atcr/personas` repo, outside this codebase's CI/test surface, and Story 3 is a pure documentation edit. No AC requires a new Go unit/integration/E2E test in this repo — verification is external observation (`atcr personas search`/`install`/`test` against the published repo) or manual doc review.

---

## Detailed AC List

### Story 1: Author Model-Tuned Persona Content

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | Anthropic Claude Persona Content | Manual | Medium | P1 |
| 01-02 | OpenAI GPT Persona Content | Manual | Medium | P1 |
| 01-03 | Google Gemini Persona Content | Manual | Medium | P1 |
| 01-04 | Passing Fixtures for the 3 New Personas | Manual | Medium | P1 |
| 01-05 | Schema & Template-Structure Validation Across All 3 Personas | Manual | Low | P2 |

### Story 2: Publish Model-Tuned Personas to the Community Registry Index

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | index.json Entries Added for the 3 New Personas | Manual | Low | P1 |
| 02-02 | End-to-End Search and Install Discoverability | Manual | Low | P1 |

### Story 3: Recommend Default Persona Pack in Documentation

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Quick Walkthrough Recommends Default Pack (`docs/personas-install.md`) | Manual | Low | P2 |
| 03-02 | README Quickstart Recommends Default Pack | Manual | Low | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 0 ACs require a new unit test in this repo.
- **Integration Tests:** 0 ACs require a new integration test in this repo.
- **E2E Tests:** 0 ACs require a new E2E test in this repo.
- **Manual/External Verification:** 9 of 9 ACs — Stories 1-2 verify via live `atcr personas search`/`install`/`test` against the published `atcr/personas` repo once it lands (out of this codebase's CI reach); Story 3 verifies via manual doc review / `git diff`.
- **High Complexity:** 0 ACs marked high complexity.
