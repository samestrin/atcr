---
id: mem-2026-06-26-1e2d3f
question: "On timeout, exec.CommandContext SIGKILLs the docker CLI not the container — what is the correct approach to reclaim container resources?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [internal/sandbox/docker.go]
tags: [clarifications, epic-11.0_executing_reviewers, implementation, docker, sandbox, timeout, container-cleanup]
retrievals: 0
status: active
type: clarifications
---

# On timeout, exec.CommandContext SIGKILLs the docker CLI not 

## Decision

Use --name with a pre-generated UUID (not --cidfile which has a write-race). Generate containerName := "atcr-sbx-" + shortUUID() before building args; inject "--name", containerName into dockerRunArgs output. After detecting DeadlineExceeded at docker.go:205, spawn exec.CommandContext(5s-background-ctx, dockerPath, "kill", containerName).Run() before returning. Since --rm is already in args (docker.go:105), the container auto-removes after kill. The fix is localized to Run() with no new test infrastructure beyond the existing mock-CLI pattern.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/sandbox/docker.go
