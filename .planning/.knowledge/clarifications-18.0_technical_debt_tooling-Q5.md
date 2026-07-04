---
id: mem-2026-07-03-9abfea
question: "How do I scrub secret-shaped tokens from arbitrary text output (e.g. a generated report/dashboard)?"
created: 2026-07-03
last_retrieved: ""
sprints: []
files: []
tags: [clarifications, epic-18.0_technical_debt_tooling, security, redaction, convention]
retrievals: 0
status: active
type: clarifications
---

# How do I scrub secret-shaped tokens from arbitrary text outp

## Decision

Reuse internal/log's Redactor as a general-purpose scrub primitive: NewRedactor("") returns a Redactor whose Redact(msg string) string masks bearer tokens and sk-/API-key shapes unconditionally (independent of any configured secrets), and is decoupled from logging. It has a containsFoldASCII prefilter that skips the regex engine on lines lacking a bearer/sk- marker, so running it over clean text is effectively free — meaning it's cheap enough to run ALWAYS (defense-in-depth) rather than gate behind a --public flag. Pass reviewRoot="" to avoid triggering absolute-path relativization. Do NOT treat repo-relative file paths (internal/foo.go:NN) as sensitive — in an OSS repo they're already public and are core navigational data.

Evidence:
- internal/log/redact.go:45 (NewRedactor), :86 (Redact(string) string)
- internal/log/redact.go:23-24 (bearerTokenPattern, skKeyPattern), :42-44 (always applies)
- internal/log/redact.go:100-105 (containsFoldASCII prefilter), :144-153 (relativizePaths gated on non-empty root)

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A
