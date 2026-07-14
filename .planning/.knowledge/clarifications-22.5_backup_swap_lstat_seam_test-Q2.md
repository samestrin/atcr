---
id: mem-2026-07-13-93f6e6
question: "Is a comment-only test-file fix sufficient when a TD row's PROBLEM is a doc comment hardcoding brittle source line numbers? (TestBackupExisting_LstatProbeFailureSurfaces doc comment citing reviewdir.go line numbers)"
created: 2026-07-13
last_retrieved: ""
sprints: []
files: [.planning/technical-debt/README.md, internal/fanout/reviewdir_test.go]
tags: [clarifications, epic-22.5_backup_swap_lstat_seam_test, testing, resolve-td, diff_smell]
retrievals: 0
status: active
type: clarifications:22.5_backup_swap_lstat_seam_test
---

# Is a comment-only test-file fix sufficient when a TD row's P

## Decision

Yes. When a TD row's PROBLEM is that a test doc comment hardcodes production source line numbers that will rot as the file changes, and the FIX is to reference the branch by symbol/behavior instead, a comment-only fix in the test file is the correct and complete resolution — this class of complaint has no possible production-code counterpart. The `diff_smell` gate's `hard:test_only` flag is a false positive here too. Confirmed for TD row `.planning/technical-debt/README.md:72` (commit c3458cdd): replaced hardcoded `reviewdir.go:341`/`346-347` citations in the `TestBackupExisting_LstatProbeFailureSurfaces` doc comment with a symbol/behavior reference ("the probe's non-ErrNotExist arm, wrapping the underlying failure"); `git show --stat` confirms only comment lines changed in `internal/fanout/reviewdir_test.go`, no production code touched. General pattern: when a resolve-td ADVERSARIAL gate flags `hard:test_only` on a TD row whose PROBLEM/FIX pair is itself entirely about test-file comment text, the flag is a structural false positive, not a signal of a weakened or reward-hacked fix.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .planning/technical-debt/README.md
- internal/fanout/reviewdir_test.go
