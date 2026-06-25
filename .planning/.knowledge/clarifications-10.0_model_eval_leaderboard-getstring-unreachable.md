---
id: mem-2026-06-24-21aba9
question: "Should ignored errors from cobra Flags().GetString() calls be documented as unreachable or explicitly returned? (cmd/atcr/benchmark.go GetString TD items)"
created: 2026-06-24
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark.go, cmd/atcr/leaderboard.go, cmd/atcr/review.go, cmd/atcr/github.go, cmd/atcr/main.go]
tags: [clarifications, epic-10.0_model_eval_leaderboard, implementation, convention, cobra, error-handling, go]
retrievals: 0
status: active
type: clarifications skill (resolve-td mode), 2026-06-24
---

# Should ignored errors from cobra Flags().GetString() calls b

## Decision

Documenting them as unreachable is the correct, intended fix — not a reward-hack. cobra's Flags().GetString(name) returns a non-nil error ONLY when `name` is an undefined flag. When the flag is registered on the command before use (e.g. cmd.Flags().String("in", ...) / String("output", ...) in newBenchmarkExportCmd at cmd/atcr/benchmark.go:86-87), the error path is unreachable, so `, _ :=` plus a documenting comment is correct. This is a project-wide convention: a grep found 31-32 GetString call sites across cmd/atcr/ and internal/ that all ignore the returned error, and ZERO that handle it. Returning the error would add a dead, untestable branch and make the new code an inconsistent outlier. Required-vs-optional flag semantics are enforced separately downstream (os.ReadFile for a required path, `if output == ""` for an optional one), not via the GetString error — so a single documenting comment legitimately covers multiple GetString calls. Minor: the in-code comment's hardcoded "27 sites" count is now stale (actual ~31-32); prefer "project-wide convention" without a number.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark.go
- cmd/atcr/leaderboard.go
- cmd/atcr/review.go
- cmd/atcr/github.go
- cmd/atcr/main.go
