---
id: mem-2026-06-12-e13ab0
question: "Should manifest.json include an ID field to make the generated review id recoverable when --output-dir is used?"
created: 2026-06-12
last_retrieved: ""
sprints: []
files: [internal/payload/manifest.go, cmd/atcr/review.go, cmd/atcr/reconcile.go, cmd/atcr/anchor.go, internal/fanout/review.go]
tags: [clarifications, epic-1.8_output-dir-support, architecture, data-model]
retrievals: 0
status: active
type: epic-1.8_output-dir-support clarifications 2026-06-12
---

# Should manifest.json include an ID field to make the generat

## Decision

No. The Manifest struct (internal/payload/manifest.go) intentionally has no id field; the generated id is carried only via the directory name in the default flow and via CLI stdout / fanout.ReviewResult.ID. Neither `atcr reconcile` nor `atcr report` reads an id from manifest.json — both resolve their working directory purely by path (reconcile.go:43, anchor.go). An orchestrator using --output-dir can capture the id from stdout. Adding an ID field to manifest.json would widen the data-model surface with no current consumer.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/payload/manifest.go
- cmd/atcr/review.go
- cmd/atcr/reconcile.go
- cmd/atcr/anchor.go
- internal/fanout/review.go
