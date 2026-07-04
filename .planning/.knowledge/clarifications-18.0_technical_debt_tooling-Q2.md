---
id: mem-2026-07-03-8f4c7d
question: "Where should new technical-debt items be persisted — the README table or the YAML shards under items/?"
created: 2026-07-03
last_retrieved: ""
sprints: []
files: []
tags: [clarifications, epic-18.0_technical_debt_tooling, architecture, technical-debt, tdmigrate]
retrievals: 0
status: active
type: clarifications
---

# Where should new technical-debt items be persisted — the R

## Decision

The `.planning/technical-debt/README.md` Markdown table is the write-master; the `items/*.yaml` shards are a generated, one-way projection. `td-migrate migrate` does ReadFile(README) -> ParseREADME -> WriteShards, and WriteShards prunes and rewrites every shard, so any shard-only write is destroyed on the next migrate. Therefore new-item writers (e.g. `atcr debt add`) must append to the README table (mirroring the existing group_td append format: rows under `### [date] From ...` headers), then optionally regenerate shards via migrate in the same step. Never write only a shard.

Evidence:
- internal/tdmigrate/run.go:73-109 (migrate = README -> ParseREADME -> WriteShards, one-way)
- internal/tdmigrate/shard.go:63-126 (WriteShards prunes prior shards; re-migrate overwrites hand-edits)
- internal/tdmigrate/parse.go:22-56 (README row/column contract to mirror)
- .planning/technical-debt/README.md:44-50 (README authoritative; shards not yet canonical)

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A
