---
id: mem-2026-06-16-ab9350
question: "What io.Writer should be passed for scorecard diagnostic output in the MCP handleReconcile handler, which has e.log *slog.Logger but no cobra cmd?"
created: 2026-06-16
last_retrieved: ""
sprints: []
files: [/Users/samestrin/Documents/GitHub/atcr/internal/mcp/handlers.go, /Users/samestrin/Documents/GitHub/atcr/.planning/epics/active/3.4_scorecard-diagnostics-writer.md]
tags: [clarifications, epic-3.3_per_run_scorecard, architecture, MCP, os.Stderr, io.Writer, slog, EmitOpts, handleReconcile]
retrievals: 0
status: active
type: clarifications
---

# What io.Writer should be passed for scorecard diagnostic out

## Decision

Pass os.Stderr explicitly. The 3.4 epic plan's Key Design Gap section (3.4_scorecard-diagnostics-writer.md:55-59) identifies that the MCP entry point has no cobra cmd, so the CLI's cmd.ErrOrStderr() precedent does not apply. AC4 accepts a "defined writer or documented default" — os.Stderr qualifies and is the minimal change. Routing through e.log (*slog.Logger, used at internal/mcp/handlers.go:224-226) would change diagnostic format on the MCP path from plain text to structured log lines, which the epic explicitly places out of scope. The fix is: add Diag io.Writer field to EmitOpts, supply os.Stderr at the handleReconcile call site (internal/mcp/handlers.go:218), and fall back to os.Stderr when nil.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- /Users/samestrin/Documents/GitHub/atcr/internal/mcp/handlers.go
- /Users/samestrin/Documents/GitHub/atcr/.planning/epics/active/3.4_scorecard-diagnostics-writer.md
