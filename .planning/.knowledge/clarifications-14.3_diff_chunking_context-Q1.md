---
id: mem-2026-07-01-fd0e64
question: "Chunk attribution architecture: how should a fan-out engine merge N chunk calls from one persona so the Reconciler still counts it as ONE reviewer/source?"
created: 2026-07-01
last_retrieved: ""
sprints: []
files: []
tags: []
retrievals: 0
status: active
type: project
---

# Chunk attribution architecture: how should a fan-out engine 

## Decision

Aggregate-at-source (not per-chunk subdirs + normalization): loop the persona's N chunk calls internally in the fan-out engine and merge them into a single raw/agent/<persona>/ directory with Reviewer=<persona> before the artifact-write step. This requires zero change to the reconciler.

Why: internal/fanout/artifacts.go's writePool hard-errors on duplicate agent directories, and agentDirName() only produces flat, single-segment names — it cannot express a chunk-N subdirectory convention. The Reviewer field is stamped once at write time in internal/fanout (findingsFor: findings[i].Reviewer = r.Agent), not re-derived later by the reconciler. The Two-Persona Consensus Rule in reconcile/consensus.go (panelReviewers) counts distinct Finding.Reviewer string values, not source directory names or counts — so as long as merged chunk results carry the plain persona name as Reviewer, the reconciler needs no chunk-awareness at all.

General pattern for this codebase: when a fan-out engine needs to make multiple LLM calls per logical source (chunking, retries, multi-pass), merge results back into one Result/directory at the dispatch layer rather than teaching the reconciler to normalize source names — the reconciler's counting/attribution logic is keyed on the stamped Reviewer/Source name, not on directory topology.</answer>
<tags>clarifications, epic-14.3_diff_chunking_context, architecture, fanout, reconcile</tags>
<files>internal/fanout/artifacts.go, internal/reconcile/discover.go, reconcile/consensus.go, reconcile/dedupe.go</files>


## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A
