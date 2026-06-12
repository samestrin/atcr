---
id: mem-2026-06-11-c77842
question: "What is the atcr CLI --base/--head flag pairing contract, and what does ci-gate.sh require?"
created: 2026-06-11
last_retrieved: ""
sprints: [1.0_atcr_core]
files: [cmd/atcr/flags.go, examples/ci-gate.sh]
tags: []
retrievals: 0
status: active
type: project
---

# What is the atcr CLI --base/--head flag pairing contract, an

## Decision

validateRangeFlags permits `--base` alone (head defaults to HEAD — explicitly documented as "the natural CI-gate invocation" at cmd/atcr/flags.go:9-13) and permits neither flag; the ONLY pairing rule is `if head && !base` -> usage error (flags.go:39-41), plus base/head cannot combine with --merge-commit (flags.go:42-44). examples/ci-gate.sh invokes with `--base` alone (ci-gate.sh:16) or neither flag (:19), both valid by design — so a claimed "--base/--head pairing mismatch" HIGH finding at flags.go:38 is a false positive, not a blocker. An observed exit 2 from ci-gate.sh is the documented missing-API-key path (AC 03-02:54-57,66-72), not flag validation. Running ci-gate.sh end-to-end needs provider keys and is a maintainer action under the no-keys boundary.</answer>
<parameter name="tags">clarifications, sprint-1.0_atcr_core, cli, flag-validation, ci-integration, exit-codes

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/flags.go
- examples/ci-gate.sh
