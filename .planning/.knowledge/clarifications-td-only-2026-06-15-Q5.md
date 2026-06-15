---
id: mem-2026-06-15-57fbfe
question: "Should the severityRank map duplication across fanout/reconcile/verify/report be fixed inline or deferred?"
created: 2026-06-15
last_retrieved: ""
sprints: []
files: [internal/fanout/postprocess.go, .planning/epics/completed/2.2_code_review_fanout_hardening.md]
tags: [td-clarification, td-only, architecture, maintainability, severity-rank, fanout, reconcile]
retrievals: 0
status: active
type: clarifications td-only 2026-06-15
---

# Should the severityRank map duplication across fanout/reconc

## Decision

Defer to a dedicated Epic Plan (2.3 severity-package-dedup). The local copy in internal/fanout/postprocess.go is intentional per Epic 2.2's recorded design decisions: the map was kept local specifically to avoid a cross-package dependency on the reconciler, and reconcile-stage changes were placed out of scope. The 120-minute estimate and 5+ package footprint confirm this exceeds safe TD-inline scope. If eventually pursued inline, option (b) — exporting from internal/stream — is preferable to a new canonical package, since internal/stream is already a shared import of both internal/fanout and internal/reconcile.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/postprocess.go
- .planning/epics/completed/2.2_code_review_fanout_hardening.md
