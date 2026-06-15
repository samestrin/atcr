---
id: mem-2026-06-15-38f906
question: "Should the loadPrior closure in runVerify be changed to a streaming JSON decoder to avoid potential memory issues with large verification.json files?"
created: 2026-06-15
last_retrieved: ""
sprints: []
files: [internal/verify/pipeline.go]
tags: [td-clarification, td-only, performance, verify-pipeline, streaming-decoder, memory]
retrievals: 0
status: active
type: td-clarification
---

# Should the loadPrior closure in runVerify be changed to a st

## Decision

No — deferred to future concern. The code comment on the loadPrior closure explicitly documents the decision: "A streaming decoder is not warranted at current scale: one record per finding, < 1 MB for any realistic review run (10–500 findings). The closure is lazy — only fires when at least one finding is skipped — so the memory cost is deferred and bounded." The lazy-load gate (priorLoaded flag) ensures the map is built at most once per runVerify call. Act only if the finding count grows beyond the 500-finding range or file size approaches multi-MB territory.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/pipeline.go
