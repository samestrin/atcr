---
id: mem-2026-07-13-f9f198
question: "Is a comment-only test-file fix sufficient when a TD row's PROBLEM is entirely comment wording accuracy? (withLstatStub doc/inline comment overstating lstatFn call sites)"
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

Yes. When a TD row's PROBLEM field is entirely about comment wording accuracy in a test file (not test assertions, coverage, or production behavior), a comment-only fix in that test file is the correct and complete resolution. The `diff_smell` over-simplification gate's `hard:test_only` flag is a false positive for such rows — there is no non-test-file fix possible for a comment-accuracy complaint, since the inaccurate text lives only in the test file. Confirmed for TD row `.planning/technical-debt/README.md:71` (commit 53d7ec4a): reworded `withLstatStub` doc comment + inline comment in `internal/fanout/reviewdir_test.go`, verified via `git show --stat` that only comment lines changed (no assertions/stub logic/production code), and verified via grep that `reviewdir.go:341` is indeed the sole `lstatFn` call site, matching the reworded comment's claim.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .planning/technical-debt/README.md
- internal/fanout/reviewdir_test.go
