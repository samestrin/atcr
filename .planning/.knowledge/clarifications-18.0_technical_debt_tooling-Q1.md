---
id: mem-2026-07-03-cf7944
question: "Should the shared README lock for TD writes be a new Go utility in internal/debt, or should writes funnel through debt.AppendItem coordinating with the skill-level .planning/.locks/td-readme.lock directory?"
created: 2026-07-03
last_retrieved: ""
sprints: []
files: [internal/debt/add.go, .planning/.locks/td-readme.lock, .planning/technical-debt/README.md, internal/atomicfs]
tags: []
retrievals: 0
status: active
type: project
---

# Should the shared README lock for TD writes be a new Go util

## Decision

Both — not competing options. Add a small reusable mkdir-based lock helper in internal/debt AND have the single exported writer debt.AppendItem acquire it internally around its full README read-modify-write + SyncShards + RefreshStats span (internal/debt/add.go:163-196). Load-bearing constraint: the Go lock MUST target the exact same .planning/.locks/td-readme.lock directory and protocol the skills use — atomic os.Mkdir (IsExist detection), owner.txt = session=<name>|epoch=<unix>, 60s wait / 300s stale-release (mirrors ~/.claude/skills/resolve-td/instructions.md:316-345). An in-process sync.Mutex or debt-only sentinel would serialize two atcr debt add processes but would NOT block a concurrent /resolve-td or group_td skill session, which is the documented corruption threat (.planning/technical-debt/README.md:77,91,93). No Go mkdir/flock lock helper exists yet; internal/atomicfs provides atomic-write primitives but no inter-process lock, so this is net-new and additive.</answer>
<parameter name="tags">clarifications, epic-18.0_technical_debt_tooling, architecture, concurrency, file-locking, technical-debt

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/debt/add.go
- .planning/.locks/td-readme.lock
- .planning/technical-debt/README.md
- internal/atomicfs
