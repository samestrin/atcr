---
id: mem-2026-07-16-0e4411
question: "Atomic stale-lock reclamation: use os.Rename CAS, not RemoveAll after re-read"
created: 2026-07-16
last_retrieved: ""
sprints: []
files: [internal/registry/telemetry_setting.go, internal/registry/trust.go]
tags: [clarifications, epic-28.0_telemetry_expansion_cloud_sync, implementation]
retrievals: 0
status: active
type: clarifications
---

# Atomic stale-lock reclamation: use os.Rename CAS, not Remove

## Decision

For the telemetry-setting config lock's stale-owner reclamation, use rename-based CAS: os.Rename(lockDir, uniqueTempName) and proceed with reclamation only if the rename succeeds. A re-read/re-confirm-then-RemoveAll approach is NOT atomic (still two separate syscalls with a gap between them) and only shrinks the race window rather than closing it.

Justification:
- Current code performs an unconditional os.RemoveAll(lockDir) after reading ownerFile once, with no re-check before removal (internal/registry/telemetry_setting.go:154-160) — a classic TOCTOU.
- os.Rename on a specific source path is atomic at the filesystem/inode level: only one caller can successfully rename a given existing path; every other concurrent caller gets ENOENT and must retry rather than blindly reclaiming.
- The codebase already establishes os.Rename as its atomic primitive of choice for this class of hazard — the config-write path in the same file uses temp-write + os.Rename (internal/registry/telemetry_setting.go:87-119), and internal/registry/trust.go:156 follows the identical pattern.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/telemetry_setting.go
- internal/registry/trust.go
