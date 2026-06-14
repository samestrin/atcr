---
id: mem-2026-06-13-78c573
question: "Should tests for the manifest snapshot fields cover the current string contract or a proposed *string contract, and do the two TD items need reconciliation before writing tests?"
created: 2026-06-13
last_retrieved: ""
sprints: []
files: [internal/payload/manifest.go, internal/fanout/manifest_review_test.go, internal/tools/snapshot.go]
tags: [clarifications, epic-2.1_manifest_review_stage_recording, testing, architecture, manifest-schema]
retrievals: 0
status: active
type: clarifications /2.1_manifest_review_stage_recording --from=resolve-td
---

# Should tests for the manifest snapshot fields cover the curr

## Decision

Write tests against the current `string` contract as-shipped — no reconciliation sprint is needed. The code at manifest.go:76-79 carries an explicit comment confirming string/no-omitempty is intentional. Tests should assert: (a) `snapshot_worktree_path` is present as `""` in live mode; (b) `snapshot_worktree_path` is the non-empty worktree leaf path in worktree mode; (c) `snapshot_mode` and `head_sha` are absent when no snapshot ran (they carry omitempty). The *string proposal was an earlier draft that was not merged.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/payload/manifest.go
- internal/fanout/manifest_review_test.go
- internal/tools/snapshot.go
