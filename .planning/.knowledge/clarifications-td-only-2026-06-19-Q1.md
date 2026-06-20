---
id: mem-2026-06-19-708a5b
question: "When a package sits just under the 80% per-package coverage guideline because only defensive stdlib error-return branches are uncovered, accept the gap or add a fault-injection seam to production code?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/atomicfs/atomic.go]
tags: [td-clarification, td-only, testing, coverage, atomicfs, go]
retrievals: 0
status: active
type: clarifications td-only 2026-06-19 Q1 (epic-4.7)
---

# When a package sits just under the 80% per-package coverage 

## Decision

Accept the defensive branches as untested. The 80% per-package figure is a guideline, not a CI gate; module-wide coverage (89.6%) is what matters. For internal/atomicfs/atomic.go the uncovered clusters are WriteFileAtomic mid-stream Write/Chmod/Close failures (atomic.go:32-42) and BackupToDotBak staging RemoveAll/Rename error paths (atomic.go:101-127) — pure error-propagation (return err) on stdlib file ops with no logic to verify. Adding an injectable file-op seam to reach >=80% is production indirection purely for test reach, which contradicts the minimum-code/nothing-speculative principle and risks tripping the over-complexity SAFE_SCOPE gate. Resolution: close the coverage TD with a one-line coverage-exception note; do not add a seam. The TD row's own Fix text sanctioned this option.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/atomicfs/atomic.go
