---
id: mem-2026-06-24-597541
question: "Does hash.Hash.Write return errors that should be propagated in Go?"
created: 2026-06-24
last_retrieved: ""
sprints: []
files: [internal/benchmark/benchmark.go:202]
tags: [clarifications, epic-10.0_model_eval_leaderboard, implementation, go-stdlib, hash, error-handling]
retrievals: 0
status: active
type: clarifications/10.0_model_eval_leaderboard
---

# Does hash.Hash.Write return errors that should be propagated

## Decision

No. The Go stdlib hash.Hash interface guarantees that Write always returns len(p), nil — it never returns a non-nil error. Silencing the return values with _, _ when writing to a hash.Hash (e.g., sha256) is correct and conventional, not a bug. Adding error handling or changing a helper's signature to return error solely because it writes to a hash.Hash is unnecessary. This applies to any function whose only io.Writer is a hash implementation (sha256, md5, sha512, etc.).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/benchmark/benchmark.go:202
