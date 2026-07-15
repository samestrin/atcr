---
id: mem-2026-07-14-589344
question: "Gitignore matching: vetted library vs hand-rolled matcher"
created: 2026-07-14
last_retrieved: ""
sprints: []
files: [go.mod, internal/verify/select.go, cmd/atcr/init.go]
tags: [clarifications, epic-26.0_atcrignore_token_protection, implementation]
retrievals: 0
status: active
type: clarifications
---

# Gitignore matching: vetted library vs hand-rolled matcher

## Decision

Use github.com/sabhiram/go-gitignore rather than hand-rolling .gitignore semantics. No gitignore-matching capability exists anywhere in the codebase (grep across internal/, cmd/ only turns up unrelated hits: internal/verify/select.go:126-127 dotfile-extension guard, cmd/atcr/init.go:58-74 the .atcr/.gitignore scaffold). go.mod (Go 1.25.0) has a modest, curated dependency set with no existing gitignore library. A hand-rolled matcher risks silently mishandling negation, **, anchoring, or directory-only patterns — either leaking ignored files into the LLM payload (the exact token-waste problem the feature exists to prevent) or over-excluding real diffs from review. Applies to any future ATCR feature that needs .gitignore-syntax pattern matching.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- go.mod
- internal/verify/select.go
- cmd/atcr/init.go
