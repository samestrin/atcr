---
id: mem-2026-06-15-6c7d55
question: "What join key should scorecard verdictTallies use to match verification verdicts to raised findings?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/scorecard/scorecard.go, internal/verify/emit_findings.go, internal/verify/pipeline.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, correctness, architecture, finding-join-key, verification]
retrievals: 0
status: active
type: clarifications
---

# What join key should scorecard verdictTallies use to match v

## Decision

Keep the exact (file, line, problem) byte-match — it is the canonical finding-join key the rest of the pipeline already uses (verify's FindingKey{File, Line, Problem}, internal/verify/emit_findings.go:12-21). Do NOT switch to file+line only (deliberately reintroduces the collision the triple guards against) and do NOT normalize/hash the problem snippet (solves a non-existent problem). findings.json and verification.json are both emitted from the SAME reconciled finding objects (verify copies File/Line/Problem verbatim at internal/verify/pipeline.go:262,271,411 and reconcile dedupes before verification), so the problem text is byte-identical on both sides and cannot be reformatted between review and verify. The scorecard's findingKey(file,line,problem) at internal/scorecard/scorecard.go:311-313 correctly reuses this canonical scheme. The only worthwhile addition is a soft, non-fatal stderr warning when a verification finding matches no raised finding, mirroring verify's existing orphan_verdict diagnostic (internal/verify/pipeline.go:306) — degrade-gracefully, never a hard error. Convention: any new finding-matching code MUST reuse the canonical (file,line,problem) key, not invent a third scheme.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/scorecard.go
- internal/verify/emit_findings.go
- internal/verify/pipeline.go
