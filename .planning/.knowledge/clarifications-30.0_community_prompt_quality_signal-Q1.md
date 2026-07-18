---
id: mem-2026-07-17-7c1b74
question: "TD scope-splitting: bundled fix exceeding SAFE_SCOPE (Compact temp-file sweep + defer hoist)"
created: 2026-07-17
last_retrieved: ""
sprints: [30.0_community_prompt_quality_signal]
files: [internal/localdebt/store.go, .planning/technical-debt/README.md, .planning/sprints/active/30.0_community_prompt_quality_signal/tech-debt-captured.md]
tags: [td-clarification, resolve-td, scope-splitting, safe-scope, process]
retrievals: 0
status: active
type: td-clarification
---

# TD scope-splitting: bundled fix exceeding SAFE_SCOPE (Compac

## Decision

When a TD row's Fix bundles multiple parts and the combined change exceeds resolve-td's SAFE_SCOPE (~5 lines / no cascading), split it: apply the part that actually resolves the reported Problem now, defer the part that is a pure structural refactor (no functional bug) to a post-sprint robustness pass.

Concrete case: internal/localdebt/store.go:324 (Compact) — Fix bundled (a) sweeping stale .*.jsonl.tmp-* files at Compact's start and (b) hoisting the temp lifecycle into a helper so the os.Remove defer isn't loop-scoped. Only (a) fixes the stated harm (temp files "accumulate across crashes, nothing reaps them"). (b) is cosmetic — the deferred closures are bounded by distinct month count and all still execute correctly at function return; not an actual leak. Decision: land (a) now via /resolve-td, defer (b).

Precedent that established this pattern: TD-003 (internal/localdebt/store.go:231, same file, same TD group 2) was deferred during /execute-sprint's adversarial review as "Post-sprint robustness pass," then resolved in a later /resolve-td session by picking the cheaper of its two offered Fix options (documenting the invariant) rather than the full logic change. See tech-debt-captured.md TD-003/005/006/008 for the sprint's broader convention of deferring non-blocking maintainability findings this way.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/localdebt/store.go
- .planning/technical-debt/README.md
- .planning/sprints/active/30.0_community_prompt_quality_signal/tech-debt-captured.md
