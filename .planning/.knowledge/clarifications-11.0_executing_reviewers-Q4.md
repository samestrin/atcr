---
id: mem-2026-06-26-bc8f54
question: "Should renderCommand embed the raw script body in evidence_exec.command, or redact/hash/truncate it?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [internal/sandbox/docker.go]
tags: [clarifications, epic-11.0_executing_reviewers, design-intent, evidence_exec, command, script, truncation]
retrievals: 0
status: active
type: clarifications
---

# Should renderCommand embed the raw script body in evidence_e

## Decision

Leave as-is. The script is operator-authored — no confidentiality concern. T5 explicitly states the purpose is to show the operator the command, exit code, and output excerpt. Truncation applies only to OutputExcerpt per T2 (not to the Command field). docker.go:138-143 implementation is correct. The Command field is always short enough that a separate budget is unnecessary.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/sandbox/docker.go
