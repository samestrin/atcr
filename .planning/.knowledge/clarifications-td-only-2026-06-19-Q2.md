---
id: mem-2026-06-19-b130af
question: "When fanout needs a validator from internal/validation, import it or duplicate the check inline?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/boundaries_test.go, internal/validation/validation.go, internal/fanout/review.go]
tags: []
retrievals: 0
status: active
type: clarifications (resolve-td, 2026-06-19)
---

# When fanout needs a validator from internal/validation, impo

## Decision

Add the package to the importer's allowlist and reuse the canonical validator — do not duplicate inline. internal/validation is a stdlib-only leaf (allowlist entry "validation": {} in internal/boundaries_test.go:34, zero internal deps). Any engine-layer package (e.g. fanout) importing it is a downward engine→leaf dependency that satisfies the "lower layers never import higher ones" rule and keeps TestInternalPackages_AllowlistIsAcyclic green; the allowlist just needs the entry added. Example: to enforce validation.FilePath() (the /etc,/proc,/sys system-path reject at internal/validation/validation.go:57) inside the exported fanout.PrepareReview, add "validation" to fanout's allowlist and call validation.FilePath(abs) rather than forking the denylist inline (which would drift from the canonical copy).</answer>
<parameter name="tags">td-clarification, td-only, architecture, dependency-boundaries, internal/validation, internal/fanout

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/boundaries_test.go
- internal/validation/validation.go
- internal/fanout/review.go
