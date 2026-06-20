---
id: mem-2026-06-19-a29f3d
question: "Where should --force and the updated collision error (naming both --resume and --force) apply in atcr review?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/fanout/reviewdir.go, internal/fanout/review_test.go, cmd/atcr/review.go]
tags: [clarifications, epic-4.7_idempotency, scope, architecture, collision-detection, force-flag, derived-id]
retrievals: 0
status: active
type: clarifications
---

# Where should --force and the updated collision error (naming

## Decision

Apply only to the explicit --id path (ScaffoldReviewDir at internal/fanout/reviewdir.go:203-220) and the --output-dir non-empty path (ScaffoldOutputDir at internal/fanout/reviewdir.go:233-275). Leave derived-id auto-suffix entirely unchanged. claimReviewDir at reviewdir.go:171-193 loops through collision candidates atomically (os.Mkdir as claim, ErrExist advances counter) and never surfaces an error to the user — there is no error message to rewrite and no --resume/--force branching to introduce. The explicit --id path is the only one that hard-errors today (ScaffoldReviewDir:208-210); AC1 requires that message be rewritten to name both --resume and --force. TestPrepareReview_RejectsExistingOverrideID at internal/fanout/review_test.go:419-429 confirms the current explicit-id collision behavior under test.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/reviewdir.go
- internal/fanout/review_test.go
- cmd/atcr/review.go
