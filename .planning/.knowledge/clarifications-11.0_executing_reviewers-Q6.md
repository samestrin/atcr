---
id: mem-2026-06-26-960081
question: "Is feeding a script over stdin to /bin/sh -s a shell injection risk?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [internal/sandbox/docker.go]
tags: [clarifications, epic-11.0_executing_reviewers, security, shell-injection, false-positive, stdin, sandbox]
retrievals: 0
status: active
type: clarifications
---

# Is feeding a script over stdin to /bin/sh -s a shell injecti

## Decision

No — this is a false positive. Shell injection requires attacker-controlled content to be interpolated into a shell command string (e.g., sh -c "$script"). Feeding content over stdin to sh -s makes the script the program source, not an argv value — there is no injection vector. docker.go:127-129 shows the script never enters argv; docker.go:181-182 shows cmd.Stdin = strings.NewReader(spec.Script) is the sole delivery mechanism. The docker.go:97-99 comment confirms intent matches implementation. Document this in the AC-SECURITY checklist as mitigated.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/sandbox/docker.go
