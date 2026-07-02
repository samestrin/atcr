---
id: mem-2026-07-02-aa433a
question: "scrubField merges distinct reviewer identities by design (privacy scrubbing)"
created: 2026-07-02
last_retrieved: ""
sprints: []
files: [internal/scorecard/export.go, internal/scorecard/export_test.go]
tags: [clarifications, epic-15.1_leaderboard-cost-na-rendering, implementation, scorecard, privacy-scrubbing, export]
retrievals: 0
status: active
type: clarifications
---

# scrubField merges distinct reviewer identities by design (pr

## Decision

By-design behavior, not a bug: internal/scorecard/export.go groups reviewer records by scrubbed identity (`scrubField(r.Persona)` / `scrubField(r.Model)`, export.go:229-231), scrubbing BEFORE keying so PII/paths/secrets never reach the public export (scrubField doc, export.go:305-317). TestExport_DistinctIdentitiesMergeWhenTheyScrubEqual (export_test.go:296-311) explicitly locks this in as an anti-regression invariant, with an inline comment stating the test exists to catch a future refactor that reorders scrub-then-key into key-then-scrub and "silently un-merges groups and changes public output." Two records differing only in embedded secret paths (e.g. /tmp/secretA vs /tmp/secretB) are expected to merge into one aggregated row. "Fixing" this to keep pre-scrub identities distinct would reintroduce the PII-leak risk scrubField exists to prevent, so any future TD/bug report claiming scrubField "incorrectly" merges identities should be closed as won't-fix unless it's proposing a real design change to the anonymization contract.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/export.go
- internal/scorecard/export_test.go
