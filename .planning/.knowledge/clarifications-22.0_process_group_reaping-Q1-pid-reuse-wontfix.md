---
id: mem-2026-07-12-4001ab
question: "Should a documented \"accept the low probability\" PID-reuse decision in a test-only liveness helper be closed as won't-fix?"
created: 2026-07-12
last_retrieved: ""
sprints: []
files: [internal/verify/localvalidate_pgroup_unix_test.go, internal/verify/localvalidate_pgroup_unix.go]
tags: [clarifications, epic-22.0_process_group_reaping, testing, scope, wont-fix, process-groups]
retrievals: 0
status: active
type: clarifications
---

# Should a documented "accept the low probability" PID-reuse d

## Decision

Close as accepted/won't-fix — the documented decision stands. This is already resolved and implemented in the code (the NOTE comment plus the 2s-window narrowing in commit 802f7a5b), and none of the epic's Acceptance Criteria require PID-reuse-proof identity probing in the test helper.

- The NOTE at internal/verify/localvalidate_pgroup_unix_test.go:24-32 explicitly documents the tradeoff and already selects "accept the low probability," calling full identity probing "over-engineering for this regression test" — a recorded decision, not an open question.
- The mitigation (narrowing require.Eventually to a 2s window) is already implemented at internal/verify/localvalidate_pgroup_unix_test.go:55 and :78, from commit 802f7a5b.
- processAlive is test-only infrastructure, separate from configureProcessGroup (internal/verify/localvalidate_pgroup_unix.go:30-45), which is the actual epic deliverable — none of the Acceptance Criteria touch test-helper hardening.
- Portability reinforces the won't-fix conclusion: the test file carries //go:build unix (line 1), covering macOS/BSD which lack /proc, so a start-time/identity probe would need non-portable per-OS code — disproportionate for a helper that only verifies a regression test's own outcome.
- General pattern: a documented, implemented "accept low probability + narrow window" tradeoff in a test-only verification helper is a legitimate won't-fix closure when the epic's Acceptance Criteria don't touch that helper — don't re-open it just because a code-review gate flags a test-only diff.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/localvalidate_pgroup_unix_test.go
- internal/verify/localvalidate_pgroup_unix.go
