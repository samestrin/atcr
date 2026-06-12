---
id: mem-2026-06-11-40b6d5
question: "What is gitrange.Resolve's contract on an empty range, and how should resolved refs be surfaced?"
created: 2026-06-11
last_retrieved: ""
sprints: [1.0_atcr_core]
files: [internal/gitrange/resolver.go, internal/mcp/handlers.go, internal/gitrange/resolver_test.go, cmd/atcr/range.go]
tags: []
retrievals: 0
status: active
type: project
---

# What is gitrange.Resolve's contract on an empty range, and h

## Decision

gitrange.Resolve intentionally returns (nil, ErrEmptyRange) on every failure — never a partial *Resolution. This honors the documented invariant "CommitCount is always >=1; a zero count is reported as ErrEmptyRange before a Resolution is ever constructed" (internal/gitrange/resolver.go:50-51, with sentinels built at resolver.go:128,135). Do NOT change the contract to return a partial Resolution or a ref-carrying typed error. The only consumer needing refs on an empty range, handleRange, already echoes the client-supplied in.Base/in.Head (internal/mcp/handlers.go:59-60), satisfying AC 04-04 Edge Case 5 ({base, head, commit_count:0, file_count:0}, no error). Other callers (handleReview handlers.go:96-98; cmd/atcr/range.go:32-34) depend on ErrEmptyRange being terminal. Tests lock the (nil, ErrEmptyRange) contract at resolver_test.go:129-142.</answer>
<parameter name="tags">clarifications, sprint-1.0_atcr_core, architecture, gitrange, contract, error-handling

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/gitrange/resolver.go
- internal/mcp/handlers.go
- internal/gitrange/resolver_test.go
- cmd/atcr/range.go
