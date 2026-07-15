---
id: mem-2026-07-14-8a73e4
question: "Ignore-file filtering scope: repo-root-only vs nested tree-wide .gitignore"
created: 2026-07-14
last_retrieved: ""
sprints: []
files: [internal/payload/diff.go, internal/payload/builder.go]
tags: [clarifications, epic-26.0_atcrignore_token_protection, scope, architecture]
retrievals: 0
status: active
type: clarifications
---

# Ignore-file filtering scope: repo-root-only vs nested tree-w

## Decision

ATCR's ignore-file filtering (both .gitignore and .atcrignore) is scoped repo-root-only, not true git semantics with nested per-directory .gitignore files. The file list the filter gates comes from a single repo-root-scoped git call — g.run("diff", "--name-status", "-M", base..head) at internal/payload/diff.go:123, run with -C g.dir (internal/payload/diff.go:96) — returning a flat list of repo-root-relative paths, consumed by changedFilesMemo (internal/payload/diff.go:159) and buildEntriesValidated (internal/payload/builder.go:117). There is no existing per-directory walk or nested-config discovery step; adding nested .gitignore precedence would require a second filesystem/git traversal purely to build the ignore-pattern set. This mirrors the .atcrignore scope decision (already root-only) and keeps the feature inside its stated "no complex globbing" bound.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/payload/diff.go
- internal/payload/builder.go
