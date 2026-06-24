---
id: mem-2026-06-23-211017
question: "diff_smell test_only gate fires on a comment-only production change — should the item be re-attempted or treated as resolved?"
created: 2026-06-23
last_retrieved: ""
sprints: [8.0_reconciler_library]
files: [reconcile/merge.go, reconcile/merge_test.go, reconcile/finding.go, reconcile/reconcile_test.go]
tags: [clarifications, sprint-8.0_reconciler_library, testing, process, diff_smell, TD-005, resolve-td]
retrievals: 0
status: active
type: clarifications
---

# diff_smell test_only gate fires on a comment-only production

## Decision

The diff_smell `test_only` verdict can be a false positive when the only production-code artifact is a comment/godoc change. In the TD-005 case (reconcile/merge.go:48-49), the `Merge` godoc was updated with "Input Verification blocks are intentionally NOT propagated: Verification is stamped post-reconcile by the caller after the verify stage resolves verdicts." — satisfying the TD-005 "document the intent" resolution path. The gate fires because it classifies comment-only edits as non-functional. Resolution: ensure the production-doc change is committed in the same diff window as any accompanying contract-guard test; the gate should then see both a production-source change and a test change and classify the fix correctly. The behavior itself (Verification not propagated through Merge) is intentional at the ATCR boundary — verify stage stamps verdicts post-reconcile onto JSONFinding, where *Verification pointer identity is preserved by the adapter.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/merge.go
- reconcile/merge_test.go
- reconcile/finding.go
- reconcile/reconcile_test.go
