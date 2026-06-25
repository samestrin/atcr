---
id: mem-2026-06-24-e9ed53
question: "How should the cross-package invariant that validateAgent rejects empty Language tokens be enforced in testing?"
created: 2026-06-24
last_retrieved: ""
sprints: [9.0_persona_ecosystem]
files: [internal/verify/select.go, internal/registry/config.go, internal/registry/config_test.go]
tags: [clarifications, sprint-9.0_persona_ecosystem, testing, defense-in-depth, language-invariant, integration-test]
retrievals: 0
status: active
type: clarifications skill — resolve-td batch 2026-06-24
---

# How should the cross-package invariant that validateAgent re

## Decision

Add a cross-package integration test rather than a second comment. The invariant comment already exists at select.go:122-124. validateAgent's empty-entry guard is at config.go:704 and is unit-tested at config_test.go:664-695 (TestAgentConfig_LanguageField_Validation) for empty string, whitespace-only, single dot, and dot-with-spaces. What is absent: a test that loads a registry via LoadRegistry, calls AgentsByRole(RoleSkeptic), and asserts no resulting Language slice contains an empty string. This cross-package integration test catches a validateAgent regression at the loaded-registry level — a regression that the isolated unit test alone cannot detect.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/select.go
- internal/registry/config.go
- internal/registry/config_test.go
