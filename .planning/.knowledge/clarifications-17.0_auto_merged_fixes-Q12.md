---
id: mem-2026-07-03-2c35c9
question: "Should permission/scope validation (e.g. GitHub contents:write) be enforced with a runtime smoke-check in the --auto-fix gate, or documented as a precondition?"
created: 2026-07-03
last_retrieved: ""
sprints: [17.0_auto_merged_fixes]
files: [cmd/atcr/autofix.go, internal/ghaction/client.go]
tags: []
retrievals: 0
status: active
type: project
---

# Should permission/scope validation (e.g. GitHub contents:wri

## Decision

Document it as a precondition — do not add a runtime smoke-check. The --auto-fix backend gate (validateAutoFixBackend, cmd/atcr/autofix.go:86-96) is deliberately local-only: it performs only env/flag reads, config-shape parsing, and one os.Stat, and makes no network call. Adding a live GitHub permission probe would violate that no-network contract, add latency and a new failure surface, and is a separate deliberate feature decision, not a TD fix. Token scopes (contents:write + pull_requests:write) belong in the flag help (autofix.go:46) and the gate's doc contract as a stated precondition. General principle: keep configuration/permission gates local and fast; runtime permission smoke-checks (network calls) are a distinct feature, not part of a fail-closed local gate.</answer>
<parameter name="tags">clarifications, sprint-17.0_auto_merged_fixes, security, architecture, gate-design, github

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/autofix.go
- internal/ghaction/client.go
