---
id: mem-2026-07-05-9194bb
question: "Duplicate parse of sources/pool/findings.txt by history and audit capture hooks — won't-fix rationale"
created: 2026-07-05
last_retrieved: ""
sprints: []
files: [internal/history/capture.go, internal/audit/capture.go, cmd/atcr/review.go, cmd/atcr/resume.go]
tags: [clarifications, epic-19.1_audit_trail, architecture, scope, wont-fix]
retrievals: 0
status: active
type: clarifications
---

# Duplicate parse of sources/pool/findings.txt by history and 

## Decision

Won't-fix. The audit hook and history hook each independently parsing sources/pool/findings.txt in the same review run is a deliberate consequence of an explicit design decision to mirror internal/history/'s independent package layout rather than share state with it. A cross-package signature refactor (changing history.RecordReview and audit.RecordReview plus both call sites and tests) to eliminate one extra parse of a small per-run file is disproportionate to the gain.

Justification:
- Both hooks independently call os.ReadFile(filepath.Join(reviewDir, poolFindingsRel)) followed by stream.ParseSource(data): internal/history/capture.go:30-38 and internal/audit/capture.go:63-71.
- The epic 19.1 Clarifications' Technical Approach explicitly directs: "Mirror internal/history/ layout: record.go (types), writer.go (Append via O_APPEND), reader.go (Load), capture.go (hook), render.go (markdown)" — a decision to duplicate the pattern, not factor out a shared reader.
- Each ledger is designed as an independently-failing, non-fatal "repo-level accumulator" (cmd/atcr/review.go:394-401 history hook, cmd/atcr/review.go:409-416 audit hook) — decoupled parallel writers by design, not a shared pipeline.
- sources/pool/findings.txt is normally small (one row per finding across a handful of reviewer agents), soft-bounded per agent by maxAgentFileBytes = 32 << 20 (internal/fanout/resume.go:466, a corruption guard not a normal-case size) — re-parsing twice per run is a bounded, once-per-run cost.
- General precedent: when a proposed refactor touches multiple internal packages' public APIs plus call sites plus tests for a sub-millisecond, once-per-run perf gain, and the duplication stems from an explicit "mirror independent package layout" design decision rather than an oversight, prefer won't-fix over the refactor.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/history/capture.go
- internal/audit/capture.go
- cmd/atcr/review.go
- cmd/atcr/resume.go
