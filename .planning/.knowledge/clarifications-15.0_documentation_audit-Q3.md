---
id: mem-2026-07-01-87885d
question: "TestReconcilerConfigSurfaceDocumented anchoring fix: verify: is only prose-documented in docs/registry.md — add a YAML block, or scope the test differently?"
created: 2026-07-01
last_retrieved: ""
sprints: []
files: [docs/registry.md, docs/verification.md, cmd/atcr/docs_audit_test.go]
tags: [clarifications, epic-15.0_documentation_audit, documentation, testing, resolve-td, diff_smell]
retrievals: 0
status: active
type: clarifications
---

# TestReconcilerConfigSurfaceDocumented anchoring fix: verify:

## Decision

Add a real `verify:` fenced-YAML example to docs/registry.md, mirroring the existing `debate:`/`executor:` blocks — don't special-case the test to accept `verify:`'s current prose-only mention. verification.md also documents `verify.min_severity`/`verify.votes` only as dotted-key prose, not a fenced block, so scoping the test to accept "documented elsewhere" would just relocate the same weak prose-only evidence rather than fix it. The epic's own Clarifications name `verify:` as equally real as `debate:`/`executor:`, and AC2 requires config examples to include "all necessary keys" — so the actual gap is a missing doc example, not test leniency.

General pattern: when a docs-drift regression test needs to move from a bare `strings.Contains` substring check to a real structural anchor (fenced-YAML key at column 0 / real heading), and one of the checked tokens only appears in prose, treat that as a genuine documentation gap to close (add the missing example) rather than loosening the test to special-case that one token — especially when a source-of-truth document (like an epic's recorded Clarifications) already asserts the token is an equally real, in-scope config surface as its siblings that DO have real examples.

Justification:
- docs/registry.md has exactly one occurrence of `verify:`, in prose at docs/registry.md:227, not inside any of the file's 9 fenced ```yaml blocks.
- `debate:` (docs/registry.md:284-292) and `executor:` (docs/registry.md:308-325) both have real fenced-YAML examples immediately followed by a field/default/notes table; `verify:` is the outlier.
- docs/verification.md:85-95 ("Cost Controls") documents `verify.min_severity`/`verify.votes` as dotted-key prose bullets, not a fenced YAML block.
- Epic 15.0's recorded Clarifications: "the real user-facing config surface is persona ... plus the debate:, verify:, and executor: blocks" — treats all four equally.
- cmd/atcr/docs_audit_test.go:389-401 (TestReconcilerConfigSurfaceDocumented) still does a flat strings.Contains for all four tokens — unfixed as of this clarification.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- docs/registry.md
- docs/verification.md
- cmd/atcr/docs_audit_test.go
