---
id: mem-2026-07-01-e75dc2
question: "Should a new run-wide behavior toggle (e.g. bulk vs chunked review strategy) live on the global Settings/Registry struct, or be made selectable per-agent on AgentConfig?"
created: 2026-07-01
last_retrieved: ""
sprints: []
files: []
tags: []
retrievals: 0
status: active
type: project
---

# Should a new run-wide behavior toggle (e.g. bulk vs chunked 

## Decision

Default to a run-wide field on Settings/Registry, not a per-agent field on AgentConfig, unless an acceptance criterion specifically requires per-agent selectability.

Why: this codebase has an established idiom — Settings (internal/registry/precedence.go) is the home for run-wide toggles like PayloadMode, TimeoutSecs, PayloadByteBudget, MaxParallel, all resolved once per invocation via ResolveSettings through a CLI flag > project config > registry.yaml > embedded default precedence chain. AgentConfig only gets a per-agent override (via an Effective*(s Settings) resolver, e.g. EffectivePayloadMode) when there's a genuine need for a persona to diverge from the global default (e.g. max_context_lines needs to vary per-model because context windows differ). A pure on/off strategy switch has no such per-agent driver, so it belongs at the global tier — adding per-agent selectability for it would be unrequested scope increase.

How to apply: when introducing a new config flag, ask "does any AC require this to vary per agent?" If yes, put it on AgentConfig with an Effective* resolver overlaying a Settings default. If no, put it directly on Settings/Registry alongside PayloadMode/MaxParallel.</answer>
<tags>clarifications, epic-14.3_diff_chunking_context, architecture, registry, config</tags>
<files>internal/registry/precedence.go, internal/registry/config.go, internal/fanout/review.go</files>


## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A
