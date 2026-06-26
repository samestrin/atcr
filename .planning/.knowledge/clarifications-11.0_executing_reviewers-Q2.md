---
id: mem-2026-06-26-98aa2b
question: "What host-resource source should be used to validate Memory/CPUs/PidsLimit caps on macOS Docker (which runs in a VM)?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [internal/sandbox/docker.go, internal/sandbox/preflight.go]
tags: [clarifications, epic-11.0_executing_reviewers, implementation, docker, sandbox, platform, macos, preflight]
retrievals: 0
status: active
type: clarifications
---

# What host-resource source should be used to validate Memory/

## Decision

Use docker info (MemTotal and NCPU) as the sole resource-validation source. On macOS, Docker runs inside a Linux VM (Docker Desktop or Colima), so /proc/meminfo and cgroup limits reflect VM allocation — but that VM allocation is exactly the ceiling the daemon can enforce. docker info returns the same VM-scoped numbers the daemon uses when enforcing --memory and --cpus, making it the only authoritative, cross-platform, daemon-aware source. Piggyback on the already-planned docker info call in Preflight() — parse MemTotal and NCPU from docker info --format '{{json .}}' and validate cfg.Memory <= MemTotal and cfg.CPUs <= NCPU.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/sandbox/docker.go
- internal/sandbox/preflight.go
