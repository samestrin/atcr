---
id: mem-2026-06-24-60cb70
question: "Should writeField/ReproHashManifest ignored write errors (sha256 hash) be refactored to propagate errors, or documented as safe?"
created: 2026-06-24
last_retrieved: ""
sprints: []
files: [internal/benchmark/benchmark.go]
tags: [clarifications, epic-10.0_model_eval_leaderboard, error-handling, convention, sha256, false-positive]
retrievals: 0
status: active
type: clarifications
---

# Should writeField/ReproHashManifest ignored write errors (sh

## Decision

Known false-positive — do NOT refactor the signature. Every writeField call passes h = sha256.New() (internal/benchmark/benchmark.go:173), a hash.Hash whose Write is contractually documented to never return a non-nil error, so the `_, _ =` discards are unreachable and idiomatic. Convention: follow precedent from commit 4a81406 ("document safe-to-ignore … errors") — add a one-line comment at writeField (benchmark.go:213) and the inline Fprintf (benchmark.go:201) explaining why the writes cannot fail, rather than cascading an error return across all callers.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/benchmark/benchmark.go
