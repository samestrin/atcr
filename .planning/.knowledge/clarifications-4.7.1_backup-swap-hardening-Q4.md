---
id: mem-2026-06-20-fbeb0e
question: "Is backupCrossDevice's inner os.Rename(backupNew, backup) safe / same-filesystem, and should it be guarded?"
created: 2026-06-20
last_retrieved: ""
sprints: []
files: [internal/fanout/reviewdir.go]
tags: [clarifications, epic-4.7.1_backup-swap-hardening, architecture, backup, atomic-swap, exdev, wont-fix]
retrievals: 0
status: active
type: clarifications
---

# Is backupCrossDevice's inner os.Rename(backupNew, backup) sa

## Decision

Same-fs by construction — no guard needed (won't-fix). In internal/fanout/reviewdir.go, backup := path + ".bak" and backupNew := path + ".bak.new" are same-directory siblings of path, so backupCrossDevice's inner os.Rename(backupNew, backup) is same-filesystem and atomic in every present-day call path. There is no construction site where the two staging paths could diverge across mounts; the EXDEV boundary that triggers this fallback is between path and its .bak sibling (path being a mountpoint), not between the two .bak* staging siblings. A runtime guard asserting same-fs would be unreachable dead code today (only a hypothetical future refactor relocating staging could break the invariant), so per the project's minimum-code/nothing-speculative rule it is closed won't-fix. The invariant is already documented at the function level (reviewdir.go:415-419: "same-fs staging sibling … next to backup on the parent filesystem", "atomic same-fs swap").

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/reviewdir.go
