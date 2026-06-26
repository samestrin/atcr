---
id: mem-2026-06-26-ba2499
question: "Should /scratch tmpfs use noexec or exec in the Docker sandbox, given Go run_tests writes and executes binaries from GOCACHE/GOTMPDIR there?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [internal/sandbox/docker.go]
tags: [clarifications, epic-11.0_executing_reviewers, security, docker, sandbox, tmpfs, go-test]
retrievals: 0
status: active
type: clarifications
---

# Should /scratch tmpfs use noexec or exec in the Docker sandb

## Decision

Already resolved: docker.go:114 mounts /scratch with rw,exec. The decision was that noexec provides no meaningful defense given run_script already pipes arbitrary sh into the container — the actual containment comes from --network none, --cap-drop ALL, --security-opt no-new-privileges, and a read-only rootfs. A separate exec-enabled cache mount would add unnecessary complexity without a security gain. Document the rationale in a comment at that line.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/sandbox/docker.go
