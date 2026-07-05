---
id: mem-2026-07-04-3901cd
question: "Where should atcr's cross-run findings-history ledger live — .atcr/ (persistent workspace) or under --output-dir (per-run tree)?"
created: 2026-07-04
last_retrieved: ""
sprints: []
files: [cmd/atcr/review.go, internal/fanout/review.go, cmd/atcr/init.go, cmd/atcr/trust.go]
tags: [clarifications, epic-19.0_finding_history, architecture]
retrievals: 0
status: active
type: clarifications
---

# Where should atcr's cross-run findings-history ledger live �

## Decision

Put the ledger at .atcr/findings-history.jsonl, always — independent of --output-dir. The project already treats .atcr/ as the persistent, repo-level workspace root (config, cache, personas, .atcr/latest pointer), while --output-dir is explicitly documented as a one-off redirect for a single run's tree that leaves .atcr/latest untouched; an accumulating cross-run ledger belongs with the other persistent workspace state.

Justification:
- cmd/atcr/review.go:37 documents --output-dir as writing the review tree to an alternate path instead of .atcr/reviews/<id>/, and explicitly does not update .atcr/latest.
- internal/fanout/review.go:373-377: .atcr/latest is only repointed if req.OutputDir == "" — reinforcing --output-dir runs are out-of-band relative to tracked workspace state.
- .atcr/ already hosts other persistent cross-run state: diff cache (internal/fanout/resume.go:333), .atcr/latest, .atcr/config.yaml, .atcr/registry.yaml (cmd/atcr/trust.go:25).
- .gitignore ignores .atcr/ wholesale as local-only "diff cache + reviewer outputs, never committed" (cmd/atcr/init.go:36-41) — consistent with per-machine accumulating history.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/review.go
- internal/fanout/review.go
- cmd/atcr/init.go
- cmd/atcr/trust.go
