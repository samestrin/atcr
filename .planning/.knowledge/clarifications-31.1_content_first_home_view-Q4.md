---
id: mem-2026-07-19-dc73b9
question: "Is a test-only diff that adds t.Chdir(t.TempDir()) to isolate a test from real filesystem/CWD state (e.g. .atcr/latest) an acceptable fix under /resolve-td's deterministic test_only over-simplification gate, or does it need a different approach?"
created: 2026-07-19
last_retrieved: ""
sprints: []
files: [cmd/atcr/main_test.go, cmd/atcr/home_test.go]
tags: [clarifications, epic-31.1_content_first_home_view, testing, resolve-td]
retrievals: 0
status: active
type: clarifications
---

# Is a test-only diff that adds t.Chdir(t.TempDir()) to isolat

## Decision

Acceptable, when the diff is a pure isolation hardening: it must run before the code under test executes, must not weaken/skip/relax any existing assertion, must not touch production code, and ideally matches an existing t.Chdir(t.TempDir()) convention already used elsewhere in the same package/file (which confirms it isn't a novel workaround invented just to dodge the gate). Verify via git show --stat that only the test file changed and via diff review that assertions are byte-for-byte unchanged before flipping the TD row.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/main_test.go
- cmd/atcr/home_test.go
