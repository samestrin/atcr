---
id: mem-2026-07-21-a032ee
question: "Where should tmpfs/mount size config fields live, and how should they be validated?"
created: 2026-07-21
last_retrieved: ""
sprints: [32.3_sandbox_ephemeral_copy_overlay]
files: [internal/sandbox/docker.go, internal/sandbox/sandbox.go]
tags: [clarifications, sprint-32.3_sandbox_ephemeral_copy_overlay, architecture, config-design, docker]
retrievals: 0
status: active
type: clarifications
---

# Where should tmpfs/mount size config fields live, and how sh

## Decision

Sizing fields for Docker mount options (e.g. a tmpfs `size=` value like `WorkSize`/`ScratchSize`) belong on the backend-wide config struct (e.g. `DockerConfig`), not on a per-call request struct (e.g. `RunSpec`) — mirror whatever field already carries an equivalent existing value (e.g. `ScratchSize`). A per-call struct's validate() method should not need to know about them. If a size-grammar validator already exists in the package for one size field (e.g. `Memory`, via something like `parseDockerMemory`), treat any other unvalidated size field (`WorkSize`, `ScratchSize`) as carrying the same trust level unless there's a specific reason to validate one and not the other — inconsistent validation across sibling config fields is itself a code-smell worth flagging as tech debt, but is often accepted deliberately (operator-owned config, not caller/request-controlled, so it's a low-severity gap). Don't reach for a "dynamic" runtime check (e.g. checking available disk space) for a memory-backed tmpfs — that's the wrong resource class; the mount is backed by RAM/swap, not disk.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/sandbox/docker.go
- internal/sandbox/sandbox.go
