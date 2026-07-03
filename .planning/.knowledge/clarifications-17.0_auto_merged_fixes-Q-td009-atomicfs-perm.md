---
id: mem-2026-07-03-5c447e
question: "Why does atomicfs backup/restore lose the original file mode (0755/0600 → 0600/0644), and where is the correct fix?"
created: 2026-07-03
last_retrieved: ""
sprints: [17.0_auto_merged_fixes]
files: [internal/atomicfs/atomic.go, internal/autofix/revert.go]
tags: []
retrievals: 0
status: active
type: project
---

# Why does atomicfs backup/restore lose the original file mode

## Decision

Root cause: atomicfs.copyFile (internal/atomicfs/atomic.go:235-252) opens dst with os.OpenFile(dst, O_WRONLY|O_CREATE|O_TRUNC, perm). The perm arg is only honored when OpenFile CREATES the file; on an already-existing dst it is ignored (O_TRUNC changes bytes, not mode). BackupToDotBak (atomic.go:77) pre-creates the staging temp with os.CreateTemp (mode 0600), closes it, then calls copyFile(src, tmp, origMode) — because the temp already exists, origMode is ignored and the .bak is stored at 0600, never carrying the source's 0755. So the original mode is destroyed at BACKUP time, before any revert runs; restoring from the .bak (or chmod-ing from the .bak's mode) yields 0600, not the original.

Correct fix (approach a): make copyFile honor its perm arg on existing files — add dstFile.Chmod(perm) after the O_TRUNC open. copyFile is unexported, so blast radius is contained to atomicfs; all three callers (CopyPath, BackupToDotBak, copyTree) pass the source's mode and want it preserved, so honoring perm makes every caller more correct. Fixing it once repairs both the backup-side mode capture and the CopyPath restore, so no BackupMap contract change and no separate revert-side chmod are needed. General lesson: os.OpenFile's perm applies only on creation — any copy-over-existing that must set mode has to Chmod explicitly. Add 0755/0600 round-trip mode-fidelity tests through BackupToDotBak→CopyPath.</answer>
<parameter name="tags">clarifications, sprint-17.0_auto_merged_fixes, atomicfs, file-permissions, correctness, gotcha

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/atomicfs/atomic.go
- internal/autofix/revert.go
