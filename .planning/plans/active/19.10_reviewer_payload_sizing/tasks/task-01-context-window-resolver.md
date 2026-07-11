# Task 01: Per-Model Context-Window Resolver

**Source:** Plan 19.10 – Debt Item #1
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
The payload sizer applies a single global byte budget (`payload_byte_budget`, default 512 KiB) identically to every reviewer, regardless of that reviewer's actual model context window. This has no per-model token awareness at all: a 32k-window model (`dax`) and a 144k-window model (`otto`) receive the same byte cap. Confirmed failure mode from the 19.6 live run: `dax`'s effective input sizing produced exactly 24577 tokens = 32768 − 8192 + 1 — one token past its true available window once the 8192-token output cap is accounted for — causing an outright overflow instead of graceful degradation. Every downstream fix in this plan (effective per-agent budget, window-aware chunking, overflow policy dispatch) depends on first having a deterministic way to ask "how many tokens does this reviewer's model actually have to work with?" That building block does not exist yet.

## Solution Overview
Instead of a purely hard-coded map, implement a robust three-tier lookup hierarchy to determine a model's context window size. This prevents binary re-compilation limits and allows configuration updates separate from the binary:
1. **Agent-Level Override**: Add a `context_window` property (e.g., `ContextWindow *int `yaml:"context_window,omitempty"``) directly to `AgentConfig` in `internal/registry`. If set on the agent/persona, it wins immediately.
2. **Global Model-Context Table**: Add a `model_context_windows` mapping (e.g., `map[string]int`) to the global project config/registry settings, allowing users to define context windows for custom models globally.
3. **Static Go Map Fallback**: A hard-coded fallback map in Go (`internal/payload/contextwindow.go`) containing known defaults (e.g. `"dax": 32768`, `"otto": 144941`).
4. **Conservative Default**: An ultimate fallback constant (`32768`) for completely unknown models.

The resolver function `ContextWindowTokens` will accept the model ID and resolve using the active `ReviewConfig` / `Settings` where the configuration resides.

## Technical Implementation
### Steps
1. **Registry Extension**:
   - Add `ContextWindow *int `yaml:"context_window,omitempty"`` to `AgentConfig` in `internal/registry/config.go`.
   - Add `ModelContextWindows map[string]int `yaml:"model_context_windows,omitempty"`` to `ProjectConfig` / `Settings`.
2. **Implement Resolver**:
   - Create `internal/payload/contextwindow.go`. Implement the lookup hierarchy:
     - Check if the agent's config explicitly overrides it.
     - Check if the model is present in the global config table.
     - Fallback to the static map.
     - Fallback to the conservative default.
3. **Unit Tests**:
   - Write tests in `internal/payload/contextwindow_test.go` verifying each tier of the hierarchy: agent config override, global mapping override, static map fallback, and default fallback.

## Files to Create/Modify
- [MODIFY] [config.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/registry/config.go) – Add `ContextWindow` to `AgentConfig`
- [MODIFY] [project.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/registry/project.go) – Add `ModelContextWindows` to `ProjectConfig`
- [NEW] [contextwindow.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/payload/contextwindow.go) – Implement lookup hierarchy
- [NEW] [contextwindow_test.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/payload/contextwindow_test.go) – Verify configuration precedence


## Documentation Links
- [Context-Window Resolver](../documentation/context-window-resolver.md)

## Related Files (from codebase-discovery.json)
- `internal/payload/budget.go` – sibling file; doc-comment style and `FileEntry`/`Truncation` primitives this resolver sits alongside (untouched by this task)
- `internal/payload/budget_test.go` – test-pattern source (same-package tests, testify, `TestXxx_Scenario` naming)
- `internal/registry/config.go:298` – `AgentConfig.Model` field; the string this resolver is keyed on, shared with `diffCacheKey` and `AgentStatus.Model`

## Success Criteria
- [ ] `ContextWindowTokens(model string) int` is exported from package `payload` in `internal/payload/contextwindow.go`
- [ ] The function is a pure lookup against a static map — no network calls, no filesystem access, no external state
- [ ] `ContextWindowTokens("dax")` returns `32768`
- [ ] `ContextWindowTokens("otto")` returns `144941`
- [ ] Any model id not present in the map returns the conservative default constant, never zero and never an error
- [ ] Naming and doc comments make clear this is distinct from `MaxContextLines` (the unrelated per-chunk diff-line budget)

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `TestContextWindowTokens_KnownModel32k` — `dax` returns 32768
- `TestContextWindowTokens_KnownModel144k` — `otto` returns 144941
- `TestContextWindowTokens_UnknownModel_ReturnsDefault` — an arbitrary/made-up model id returns the conservative default constant
- `TestContextWindowTokens_EmptyModelString_ReturnsDefault` — empty string input still returns the default rather than panicking

**Integration Tests:**
- None — pure function, no integration surface. Integration with the effective-budget calculation and chunk planner is exercised by the later F2/F3 tasks that consume this function.

**Test Files:**
- `internal/payload/contextwindow_test.go`

## Risk Mitigation
- **Static table drift as the roster grows.** New personas/models added after this sprint silently fall back to the conservative default instead of failing loudly. Mitigated by keeping the default conservative (equal to the smallest currently-known window) rather than optimistic, so an unlisted model degrades safely (more aggressive shedding/chunking) rather than overflowing. A follow-on regression test (tracked separately, not blocking this task) can assert every model id currently referenced by `personas/`/registry config is present in the map or intentionally relies on the default.
- **Key mismatch with `AgentConfig.Model`.** Keying on any string other than exactly `AgentConfig.Model` would silently defeat the resolver for real reviewers. Mitigated by keying on the identical string already used by `diffCacheKey`/`AgentStatus.Model`, and calling this out explicitly in the doc comment so future callers cannot introduce a normalized/aliased key by mistake.

## Dependencies
- None — this is the foundational task other tasks (F2/F3) build on.

## Definition of Done
- [ ] `internal/payload/contextwindow.go` created with `ContextWindowTokens(model string) int` exported from package `payload`
- [ ] `internal/payload/contextwindow_test.go` created covering known-model and unknown-model-default cases
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes
- [ ] No changes made to `internal/payload/budget.go` or any other existing file — this task is purely additive
