---
id: mem-2026-06-13-ed59ae
question: "What is the manifest contract for SnapshotWorktreePath — should it be string or *string, and should omitempty be applied?"
created: 2026-06-13
last_retrieved: ""
sprints: []
files: [internal/payload/manifest.go, internal/fanout/manifest_review_test.go, internal/fanout/review.go]
tags: [clarifications, epic-2.1_manifest_review_stage_recording, architecture, manifest-schema, omitempty]
retrievals: 0
status: active
type: clarifications /2.1_manifest_review_stage_recording --from=resolve-td
---

# What is the manifest contract for SnapshotWorktreePath — s

## Decision

SnapshotWorktreePath must remain plain `string` with no `omitempty` tag. The field intentionally serializes as `""` in live mode so a reader can distinguish three states: (1) no `review` block at all = no snapshot attempted (parent field has omitempty); (2) review block present, `snapshot_worktree_path: ""` = live fast-path ran; (3) review block present, non-empty path = worktree slow-path ran. A `*string` change would collapse cases 1 and 2, breaking AC 03-03 Scenario 5. `SnapshotMode` and `HeadSHA` do carry omitempty (absent when no snapshot ran). The comment at manifest.go:76-79 makes this explicit. No callers need a type change.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/payload/manifest.go
- internal/fanout/manifest_review_test.go
- internal/fanout/review.go
