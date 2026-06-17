---
id: mem-2026-06-17-c5d706
question: "Where should redaction live so AC3 (serve constructs no logger) and require.Same identity both hold?"
created: 2026-06-17
last_retrieved: ""
sprints: []
files: []
tags: []
retrievals: 0
status: active
type: project
---

# Where should redaction live so AC3 (serve constructs no logg

## Decision

Apply SECRET redaction once at root-logger construction in setupLogger (cmd/atcr/main.go:155) via WithRedactor(logger, NewRedactor("")). WithRedactor returns a NEW logger (log/handler.go:80), so wrapping at any per-subcommand site (serve.go:39) changes identity and breaks require.Same in TestServeCmd_UsesContextLogger (serve_test.go:60). Wrapping once at construction means the single redacted instance is stored in context and passed by reference — FromContext returns the same pointer, require.Same holds, and AC3 ("setupLogger is the single point where the root logger is constructed; no subcommand builds its own") is preserved. This realizes the epic's "single redaction point" so no new log site needs independent AC5 audit. KEY NUANCE: AC5 secret scrubbing is root-independent (NewRedactor("") still applies bearer/sk- scrubbing) so it belongs at construction; AC6 path relativization is inherently review-scoped and must STAY at review.go:169 (resolveRedactRoot) and handlers.go:80-87 (per-reviewID) because no review root exists at root-logger construction time — the per-review redactor layers on top and redactingHandler.WithAttrs/WithGroup preserve the wrapper so stacking composes. Reject wrapping in serve (relaxes a passing test+AC) and in mcp.Serve (duplicates the concern, misses CLI paths).</answer>
<tags>clarifications, sprint-4.0_structured_logging, architecture, redaction, slog, logging</tags>
<sprints>4.0_structured_logging</sprints>
<files>cmd/atcr/main.go, cmd/atcr/serve.go, internal/log/handler.go, internal/log/redact.go, internal/mcp/handlers.go</files>
<source>clarifications</source>


## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A
