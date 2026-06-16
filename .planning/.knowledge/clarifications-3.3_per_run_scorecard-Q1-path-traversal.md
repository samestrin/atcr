---
id: mem-2026-06-15-5bc458
question: "Should scorecard runIDFromReviewDir (cmd/atcr/scorecard.go:104) enforce a path-traversal bound on the user-supplied review-directory argument?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [cmd/atcr/scorecard.go, cmd/atcr/anchor.go, cmd/atcr/leaderboard.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, security, scorecard, path-traversal, cli-convention, accept-and-document]
retrievals: 0
status: active
type: clarifications skill, sprint 3.3_per_run_scorecard, 2026-06-15
---

# Should scorecard runIDFromReviewDir (cmd/atcr/scorecard.go:1

## Decision

No bound — close as accept-and-document, the same posture used across the atcr CLI for user-supplied filesystem paths. atcr is a local, read-only CLI that runs with the invoking user's own permissions, so reaching any file the user can already read grants zero privilege escalation; runIDFromReviewDir only reads `<arg>/reconciled/summary.json` under the supplied dir. This is the established convention documented in anchorDir (cmd/atcr/anchor.go:12-17,32-33): path-shaped args (absolute, contain a separator, or ".") are "used verbatim — this is intentional: the user may point at a review directory anywhere on their own machine," while only bare ids are validated against ".." via fanout.ValidateReviewID. resolveScorecardRunID (scorecard.go:78) explicitly mirrors that contract. The identical risk class recurs at leaderboard.go:147 (--output symlink/TOCTOU), also dispositioned accept-and-document. Reusable rule: traversal/symlink findings on user-supplied paths in this local read-only CLI are accept-and-document, not bounded.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/scorecard.go
- cmd/atcr/anchor.go
- cmd/atcr/leaderboard.go
