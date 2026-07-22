---
id: mem-2026-07-21-c10fd1
question: "Docker tmpfs WorkSize sizing vs Memory cap — do tmpfs mounts count against --memory?"
created: 2026-07-21
last_retrieved: ""
sprints: [32.3_sandbox_ephemeral_copy_overlay]
files: [internal/sandbox/docker.go, internal/verify/autofix_exec.go]
tags: [clarifications, sprint-32.3_sandbox_ephemeral_copy_overlay, architecture, docker, tmpfs, sandbox]
retrievals: 0
status: active
type: clarifications
---

# Docker tmpfs WorkSize sizing vs Memory cap — do tmpfs moun

## Decision

A Docker `--tmpfs` mount is memory-backed and counts against the container's `--memory` cgroup cap. If a writable tmpfs overlay (e.g. `WorkSize`) defaults to the same value as `Memory`, seeding it (e.g. via `cp -a`) plus the workload's own working set can OOM-kill the run (exit 137) before useful work happens. Resolution is not to auto-shrink the overlay default or auto-raise Memory in code — document the `Memory >= WorkSize + build-working-set` relationship in the sizing field's doc comment, and make the overlay size operator-reachable via config (mirroring how other resource caps like Memory/CPUs are already plumbed from operator config into the runtime config struct) rather than hardcoding it. A visible OOM failure (surfaced as a wrapped backend-fault error, not a silent revert) is an acceptable interim posture; a silent/misclassified failure is not.
JUSTIFICATION:
- internal/sandbox/docker.go: DefaultDockerConfig() sets Memory and a tmpfs-size field (e.g. WorkSize) to the same default, and the mount branch unconditionally consumes the tmpfs-size field with no reconciliation logic against Memory.
- The failure surfaces via the Run() exit-code classification path (exit code >=125 maps to a wrapped backend-fault error), which is what keeps an OOM from looking like a silent revert.
- Operator-facing config resolvers (e.g. a ResolveXSandbox-style function) commonly map Memory/CPUs/PidsLimit/Timeout from operator config into the runtime DockerConfig but may omit newer sizing fields like a tmpfs WorkSize — leaving overlay sizing pinned to the hardcoded default regardless of operator memory budget.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/sandbox/docker.go
- internal/verify/autofix_exec.go
