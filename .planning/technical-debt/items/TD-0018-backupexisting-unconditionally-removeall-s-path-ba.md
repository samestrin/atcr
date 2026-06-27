---
id: TD-0018
order: 18
section: '[2026-06-20] From Sprint: 4.7.1_backup-swap-hardening'
date: "2026-06-20"
group: "3"
status: deferred
severity: MEDIUM
file: internal/fanout/reviewdir.go:318
category: security
est_minutes: "60"
source: code-review
reviewers: claude
confidence: MEDIUM
has_review_cols: true
---

## Problem

backupExisting() unconditionally RemoveAll's <path>.bak.old and <path>.bak.new at entry, but forceBackupOutputDir() only calls guardForeignBackup(dir+".bak") — the two staging siblings are not guarded. On an arbitrary --output-dir a user who owns <output-dir>.bak.old or <output-dir>.bak.new has it silently deleted by --force, re-opening the Epic 4.7 "never silently delete user data" contract. NOTE: epic clarification Q4 deliberately scoped the guard to .bak only (atcr-internal suffixes); this finding re-raises that decision as worth reconsidering, not a defect against the recorded scope. (intent_note: deferred per epic §Clarifications Q4 — extending guardForeignBackup to .bak.old/.bak.new is out of scope) (Won't-fix 2026-06-20: binding Epic 4.7.1 Q4 — guard stays scoped to .bak; .bak.old/.bak.new are atcr-owned staging names cleared by entry-time RemoveAll; confirmed via /sprint-clarification 95%)

## Fix

In forceBackupOutputDir() (reviewdir.go:440) extend the guard to guardForeignBackup(dir+".bak.old") and guardForeignBackup(dir+".bak.new") before backupExisting, or refuse if either exists non-empty. Add a test pre-creating a foreign <output-dir>.bak.old with user content and assert --force errors instead of deleting it.
