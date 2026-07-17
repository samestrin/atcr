# Test Planning Matrix

**Generated:** 2026-07-16
**Plan:** 29.0_anti_slop_persona
**Total ACs:** 8

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01 — Author the `simon` Persona Unit | 3 | 2 | 1 | 0 | Medium |
| 02 — Fixture Authoring & Test-Gate Integration | 3 | 2 | 1 | 0 (1 manual smoke) | Medium |
| 03 — Verify and Refresh the Blog Post Outline | 2 | 0 | 0 | 0 (2 manual) | Low |

---

## Detailed AC List

### Story 01: Author the `simon` Persona Unit

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| [01-01](acceptance-criteria/01-01-simon-yaml-schema-binding.md) | `simon.yaml` Strict Schema and Concrete Provider/Model Binding | Unit | Medium | P1 |
| [01-02](acceptance-criteria/01-02-simon-md-template-structure-focus.md) | `simon.md` Canonical Section Order, Template-Token Contract, and Anti-Slop Focus | Unit | Medium | P1 |
| [01-03](acceptance-criteria/01-03-simon-authoring-contract-consistency.md) | `simon` Unit is Self-Consistent with the Authoring Contract and Auto-Discovered by the Registry Test Suite | Integration | Medium | P1 |

### Story 02: Fixture Authoring & Test-Gate Integration

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| [02-01](acceptance-criteria/02-01-fixture-patch-authoring.md) | Fixture Patch Authoring | Unit | Medium | P1 |
| [02-02](acceptance-criteria/02-02-community-roster-registration.md) | Community Roster Registration (`communityPersonas`) | Unit | Medium | P1 |
| [02-03](acceptance-criteria/02-03-index-registration-and-test-gate.md) | `index.json` Registration and Full Test-Gate Pass | Integration + Manual E2E smoke | High | P1 |

### Story 03: Verify and Refresh the Blog Post Outline

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| [03-01](acceptance-criteria/03-01-cta-command-fix.md) | CTA Command Fix | Manual + scripted grep | Low | P2 |
| [03-02](acceptance-criteria/03-02-category-word-framing-alignment.md) | Category Word & Framing Alignment | Manual + scripted grep/diff | Low | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 4 ACs require unit tests (`go test ./personas/...`, `internal/personas/...`)
- **Integration Tests:** 2 ACs require integration-level cross-file verification (registry/index/roster consistency, `internal/registry/...`)
- **E2E Tests:** 0 ACs require dedicated E2E automation; 1 AC (02-03) includes a manual E2E CLI smoke check (`atcr personas test simon`)
- **High Complexity:** 1 AC marked high complexity (02-03 — spans index.json, simon.yaml, and the Go roster simultaneously)
- **Manual-only:** 2 ACs (Story 03) are content-review/grep-verification only — no automated Go test coverage, consistent with the blog outline being non-code content.
