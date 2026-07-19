---
id: mem-2026-07-19-d1bd85
question: "When a follow-up TD row proposes extracting a shared path-relativization helper (relHome/relDisplay/relativizePaths/expandHome) across cmd/atcr, internal/tools, and internal/log, is the cross-package extraction in-scope for a same-epic/same-TD-pass fix, or should it defer to a cross-reference TD note?"
created: 2026-07-19
last_retrieved: ""
sprints: []
files: [cmd/atcr/home.go, internal/tools/dispatch.go, internal/log/redact.go, cmd/atcr/quickstart.go, .planning/technical-debt/README.md]
tags: [clarifications, epic-31.1_content_first_home_view, scope, testing]
retrievals: 0
status: active
type: clarifications
---

# When a follow-up TD row proposes extracting a shared path-re

## Decision

Do not extract now when the originating epic/sprint declared a narrow single-component scope (here cmd/atcr only, COMPONENT_COUNT<=2 scope-guard). A shared internal/pathutil touching 3+ packages is new cross-package work outside that contract. Prefer updating the existing TD row's Fix column to cross-reference all sibling copies by name/location (noting real behavioral differences between them) and leave the row open, rather than doing the refactor immediately. The four candidates are NOT drop-in identical: relHome (cmd/atcr/home.go:44-58) is single-arg, uses mockable homeUserDir(), special-cases ~/~/rel, forward-slash-normalizes for an AXI/TOON contract; relDisplay (internal/tools/dispatch.go:370-377) is a generic two-arg base-relative helper with no ..-guard or ~ concept; relativizePaths (internal/log/redact.go:144-153) uses strings.ReplaceAll prefix-stripping over an arbitrary string, not filepath.Rel at all; expandHome (cmd/atcr/quickstart.go:279-292) is the inverse direction (~ -> full path) with a distinct (string) (string, error) signature. A future unification is a real design decision (reconciling 4 differing signatures/behaviors), not a mechanical copy-paste extraction.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/home.go
- internal/tools/dispatch.go
- internal/log/redact.go
- cmd/atcr/quickstart.go
- .planning/technical-debt/README.md
