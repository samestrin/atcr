---
id: mem-2026-06-25-53d396
question: "When building a PreparedReview from an ingested diff and a roster agent's effective payload mode is blocks or files (not diff), what should happen?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [internal/fanout/review.go, internal/registry/config.go, internal/registry/precedence_test.go]
tags: [clarifications, epic-10.1_diff_file_ingestion, architecture, fanout, mode-collapse, PreparedReview]
retrievals: 0
status: active
type: clarifications
---

# When building a PreparedReview from an ingested diff and a r

## Decision

Collapse all modes to the ingested diff (Option A). The new `PrepareReviewFromDiff` function should build a single `payloads` map keyed only to `"diff"` and resolve every agent's effective mode to `"diff"` regardless of configured `EffectivePayloadMode`. This is mechanically required: `buildAgent` does a strict map lookup on `payloads[mode]` at `internal/fanout/review.go:652-657` and fails loudly when the key is missing. Since the diff path never builds `blocks` or `files` entries, mode override is required. Rejecting mismatched modes (Option B) would break any roster where `"blocks"` is the inherited default, which is the common case.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go
- internal/registry/config.go
- internal/registry/precedence_test.go
