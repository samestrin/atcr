---
id: mem-2026-06-16-11015b
question: "What parameter mechanism should be used to thread an io.Writer through ReadRecords, FindByRunID, and ReadAll in the scorecard package?"
created: 2026-06-16
last_retrieved: ""
sprints: []
files: [/Users/samestrin/Documents/GitHub/atcr/internal/scorecard/store.go, /Users/samestrin/Documents/GitHub/atcr/internal/scorecard/scorecard.go, /Users/samestrin/Documents/GitHub/atcr/internal/scorecard/aggregate.go, /Users/samestrin/Documents/GitHub/atcr/.planning/epics/active/3.4_scorecard-diagnostics-writer.md]
tags: [clarifications, epic-3.3_per_run_scorecard, architecture, API design, scorecard, io.Writer, options struct, ReadOpts]
retrievals: 0
status: active
type: clarifications
---

# What parameter mechanism should be used to thread an io.Writ

## Decision

Use an explicit ReadOpts{Writer io.Writer} struct (option b). The 3.4 epic plan's Risks table mandates "Prefer an options struct carrying the writer over positional params." Every existing Opts type in the scorecard package (EmitOpts at internal/scorecard/scorecard.go:88, FilterOpts at internal/scorecard/aggregate.go:14) uses the struct pattern; there are zero ...io.Writer variadic usages anywhere in the codebase. Zero-value ReadOpts{} → nil Writer → fallback to os.Stderr satisfies AC5. Variadic ...io.Writer is idiomatic only when the element is truly optional config — a single behavioral override belongs in a named struct field.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- /Users/samestrin/Documents/GitHub/atcr/internal/scorecard/store.go
- /Users/samestrin/Documents/GitHub/atcr/internal/scorecard/scorecard.go
- /Users/samestrin/Documents/GitHub/atcr/internal/scorecard/aggregate.go
- /Users/samestrin/Documents/GitHub/atcr/.planning/epics/active/3.4_scorecard-diagnostics-writer.md
