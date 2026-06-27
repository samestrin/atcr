---
id: TD-0020
order: 20
section: '[2026-06-20] From Sprint: 4.7.1_backup-swap-hardening'
date: "2026-06-20"
group: "3"
status: deferred
severity: LOW
file: internal/fanout/reviewdir.go:388
category: testing
est_minutes: "120"
source: code-review
reviewers: claude
confidence: MEDIUM
has_review_cols: true
---

## Problem

backupCrossDevice's inner os.Rename(backupNew,backup) relies on backupNew and backup sharing a filesystem, an invariant that holds only by naming coincidence (both are siblings of path). If anyone later relocates backupNew under path, the inner rename silently becomes cross-device and returns a raw EXDEV to the user. (Won't-fix 2026-06-20: same-fs invariant holds by construction — backup and backupNew are both siblings of path; a runtime guard would be unreachable dead code today and is already documented at reviewdir.go:415-419; confirmed via /sprint-clarification 90%)

## Fix

Add a test that forces renameFn to return syscall.EXDEV and makes the copy fail (a copy seam, or unreadable src), staging a prior .bak first; assert the prior .bak content is restored intact, the live tree survives, and .bak.new is cleaned up. Cover the copy-failure leg at minimum.
