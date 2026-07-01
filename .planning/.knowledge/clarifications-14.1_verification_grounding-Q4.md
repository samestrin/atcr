---
id: mem-2026-06-30-8d679e
question: "Where in the atcr pipeline should a patch-grounding validator run — fan-out or reconcile?"
created: 2026-06-30
last_retrieved: ""
sprints: []
files: [internal/fanout/review.go, internal/fanout/artifacts.go, internal/fanout/postprocess.go, cmd/atcr/reconcile.go, internal/stream/validate.go]
tags: [clarifications, epic-14.1_verification_grounding, architecture]
retrievals: 0
status: active
type: clarifications
---

# Where in the atcr pipeline should a patch-grounding validato

## Decision

Run grounding validation in the fan-out phase, threading per-file changed-line-ranges from PrepareReview into WritePool/findingsFor — this is the only point in the pipeline with guaranteed live diff/base-head access. PrepareReview (internal/fanout/review.go) and buildPayloads construct per-file diff payloads but don't persist them past the merged payload text; WritePool (internal/fanout/artifacts.go) calls findingsFor per agent Result, which already applies enforceConstraints (internal/fanout/postprocess.go) — the existing precedent for an observable, stderr-logged drop at this exact stage. By contrast, `atcr reconcile <dir>` (cmd/atcr/reconcile.go) is a standalone CLI that discovers pre-existing findings files under sources/ with no live git diff or base/head refs available — a grep across internal/reconcile/*.go for Base/Head/Manifest returns no matches. The existing Epic 5.0 path-existence check at reconcile time (internal/stream/validate.go ValidatePath) only checks that a cited file exists on disk, not that a cited line is part of the diff, and keeps findings with a warning rather than dropping — a structurally different, weaker guarantee than line/snippet-in-diff grounding. Leaving standalone reconcile runs ungrounded is an accepted, documented gap, not an oversight.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go
- internal/fanout/artifacts.go
- internal/fanout/postprocess.go
- cmd/atcr/reconcile.go
- internal/stream/validate.go
