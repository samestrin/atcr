---
id: TD-0021
order: 21
section: '[2026-06-20] From Sprint: 4.7.1_backup-swap-hardening'
date: "2026-06-20"
group: "3"
status: deferred
severity: LOW
file: internal/fanout/reviewdir_test.go:458
category: testing
est_minutes: "30"
source: code-review
reviewers: claude
confidence: MEDIUM
has_review_cols: true
---

## Problem

Test-coverage gaps in the failed-swap/cleanup paths: TestBackupExisting_FailedSwapPreservesPriorBak asserts no .bak.old straggler but not .bak.new; the non-ErrNotExist Lstat(backup) error branch (reviewdir.go:333-335) is untested; the entry-time RemoveAll straggler-cleanup failure legs (reviewdir.go:318-323) are untested. Each is a real error branch a regression could silently break.

## Fix

Add assert.NoDirExists for src+".bak.new" at reviewdir_test.go:458; add a perms-based test forcing Lstat(backup) to fail with a non-ErrNotExist error; add a test where .bak.old cannot be removed and assert the typed "clearing stale staging backup" error. Skip on root/CI where perms are not enforced. (Partial 2026-06-21: gaps 1 (.bak.new assertion) and 3 (RemoveAll(.bak.old) failure) covered; gap 2 (non-ErrNotExist Lstat(backup)) deferred — needs an lstatFn production seam since the staging siblings share a parent dir so perms cannot isolate it)
