---
id: mem-2026-07-05-935038
question: "Stderr warning convention for audit-write failures (cmd/atcr/review.go + resume.go)"
created: 2026-07-05
last_retrieved: ""
sprints: []
files: [cmd/atcr/review.go, cmd/atcr/resume.go, cmd/atcr/github.go, cmd/atcr/personas.go]
tags: [clarifications, epic-19.1_audit_trail, implementation, audit, cli-conventions]
retrievals: 0
status: active
type: clarifications
---

# Stderr warning convention for audit-write failures (cmd/atcr

## Decision

On an audit-write failure, print a `warning: failed to append audit record: &lt;err&gt;` line via `fmt.Fprintf(cmd.ErrOrStderr(), ...)` in addition to the existing `log.FromContext(ctx).Warn(...)` call, and apply the identical stderr warning to the mirrored hook in cmd/atcr/resume.go (`recordResumeAudit`) since it is an exact duplicate of the review.go audit-write-failure path.

Justification:
- cmd/atcr/review.go:394-399 currently only logs via log.FromContext(ctx).Warn("failed to append audit record", "error", aerr) — no stderr output; TD row's "clearly visible stderr warning" fix is additive, not a replacement (non-fatal design, no exit-code change).
- cmd/atcr/resume.go:227-234 (recordResumeAudit) is a byte-for-byte mirror of the review.go hook, created specifically to keep resumed runs recorded exactly once (resume.go:222-223) — the codebase convention is to keep audit/history hooks symmetric between review and resume.
- Established idiom in this codebase for non-fatal-but-visible warnings: fmt.Fprintf(cmd.ErrOrStderr(), "warning: ...: %v\n", err) — see cmd/atcr/github.go:182,185,214,231 and cmd/atcr/personas.go:185,200,204.
- Threading note: review.go's audit hook already has `cmd` in scope; resume.go's recordResumeAudit/recordResumeHistory are free functions taking ctx but not cmd, so implementing this requires threading cmd (or an io.Writer) into recordResumeAudit or moving the stderr print to the call sites in runResume (resume.go:151, 204-205).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/review.go
- cmd/atcr/resume.go
- cmd/atcr/github.go
- cmd/atcr/personas.go
