---
id: mem-2026-06-19-b68e88
question: "Should guardForeignBackup be extended to cover tool-internal transient temp names (.bak.old, .bak.new) alongside the user-visible .bak name?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/fanout/reviewdir.go]
tags: [clarifications, epic-4.7.1_backup-swap-hardening, architecture, guardForeignBackup, backup, crash-safety]
retrievals: 0
status: active
type: epic clarifications 2026-06-19
---

# Should guardForeignBackup be extended to cover tool-internal

## Decision

Keep guardForeignBackup scoped to .bak only. The guard's heuristic checks for atcr review-tree markers (manifest.json + reviewSubdirs) to distinguish user-owned from atcr-owned directories. That test is meaningful for .bak (users could independently create directories with that suffix) but not for .bak.old or .bak.new — no user toolchain produces those names. Applying the guard to tool-internal temp names would be dead-weight complexity with zero safety benefit. The correct pattern is: unconditional RemoveAll of stale .bak.old/.bak.new stragglers at the start of the backup call, before the rename chain begins. This scoping principle generalizes: guardForeignBackup applies only to names a user might plausibly create independently; pure tool-internal temp names get unconditional cleanup instead.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/reviewdir.go
