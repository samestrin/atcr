---
id: mem-2026-06-19-a5ee98
question: "Should validation.Severity and validation.Enum be deleted, wired into ParseSeverity/ValidFormat, or kept as public API for AC5/AC7?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/validation/validation.go, .planning/epics/completed/4.3_input_validation.md]
tags: [td-clarification, td-only, over_engineering, architecture, validation, Severity, Enum]
retrievals: 0
status: active
type: clarifications skill — td-only 2026-06-19
---

# Should validation.Severity and validation.Enum be deleted, w

## Decision

Keep as public API. The exported Severity and Enum validators in internal/validation are deliberately NOT wired to ParseSeverity/ValidFormat. They exist to satisfy AC5 (validation.Severity must return an error for invalid input) and AC7 (100% test coverage). Epic 4.3 Clarifications (recorded 2026-06-18) explicitly state "validation.Severity/Enum ship in the package for AC5/AC7 + future use but do NOT replace those paths." Deleting them breaks AC5/AC7; wiring them in was explicitly out of scope.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/validation/validation.go
- .planning/epics/completed/4.3_input_validation.md
