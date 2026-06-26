---
id: mem-2026-06-26-819f46
question: "Where should log.Redactor be wired to scrub EvidenceExec.OutputExcerpt — inside repro.Reproduce or at the Stamp call site in pipeline.go?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [internal/repro/repro.go, internal/verify/pipeline.go, internal/mcp/handlers.go, internal/log/redact.go]
tags: [clarifications, epic-11.0_executing_reviewers, architecture, redaction, evidence_exec, pipeline, log-redactor]
retrievals: 0
status: active
type: clarifications
---

# Where should log.Redactor be wired to scrub EvidenceExec.Out

## Decision

Redact at the Stamp site in pipeline.go (lines 277-279), not inside Reproduce. Reproduce is a pure sandbox-execution function with no business knowing about secrets. Apply ev.OutputExcerpt = redactor.Redact(ev.OutputExcerpt) just before the repro.Stamp call. The Redactor (constructed at internal/mcp/handlers.go:104 from review-configured secrets) is threaded into the pipeline top-level function as a single parameter addition — zero signature changes to Reproduce, Stamp, or verifyFinding. Redactor.Redact(string) string is callable directly (internal/log/redact.go:86).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/repro/repro.go
- internal/verify/pipeline.go
- internal/mcp/handlers.go
- internal/log/redact.go
