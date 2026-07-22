---
id: mem-2026-07-22-887f5a
question: "When a signature change to fix a bug (e.g. threading a `root` param into a path-validation function) is described as \"rippling to every caller,\" how do you decide whether to change the signature outright vs. add a parallel function?"
created: 2026-07-22
last_retrieved: ""
sprints: [32.4_workspace_integrity_sanitization]
files: [internal/security/pathguard.go, internal/autofix/apply.go, internal/security/pathguard_test.go]
tags: [clarifications, sprint-32.4_workspace_integrity_sanitization, architecture, api-design, go]
retrievals: 0
status: active
type: clarifications
---

# When a signature change to fix a bug (e.g. threading a `root

## Decision

Check the actual blast radius before assuming a breaking signature change is expensive: grep every caller first. If the function/package was introduced in the same sprint with a single production caller (no external module consumers), change the signature outright rather than adding a parallel function (e.g. `IsProtectedPathAt`) — a second near-duplicate entry point leaves a trap where old callers/tests can still invoke the unfixed path and silently reintroduce the bug. Reserve additive parallel functions for genuinely mature, externally-depended-on APIs where a breaking change has real cross-team cost. Example: `internal/security.IsProtectedPath`'s layer-2 symlink check anchored to process CWD instead of the working-tree root (TD-003) — grep showed exactly one production caller (`internal/autofix/apply.go`'s `applyOne`, which already had `root` in scope), so the fix was `IsProtectedPath(path, root string) bool` with a one-line call-site update, not a parallel `IsProtectedPathAt`.
File_path: internal/security/pathguard.go:127, internal/autofix/apply.go:125-137

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/security/pathguard.go
- internal/autofix/apply.go
- internal/security/pathguard_test.go
