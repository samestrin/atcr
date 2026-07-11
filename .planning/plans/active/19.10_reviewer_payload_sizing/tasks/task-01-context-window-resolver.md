# Task 01: Per-Model Context-Window Resolver

**Source:** Plan 19.10 – Debt Item #1
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
The payload sizer applies a single global byte budget (`payload_byte_budget`, default 512 KiB) identically to every reviewer, regardless of that reviewer's actual model context window. This has no per-model token awareness at all: a 32k-window model (`dax`) and a 144k-window model (`otto`) receive the same byte cap. Confirmed failure mode from the 19.6 live run: `dax`'s effective input sizing produced exactly 24577 tokens = 32768 − 8192 + 1 — one token past its true available window once the 8192-token output cap is accounted for — causing an outright overflow instead of graceful degradation. Every downstream fix in this plan (effective per-agent budget, window-aware chunking, overflow policy dispatch) depends on first having a deterministic way to ask "how many tokens does this reviewer's model actually have to work with?" That building block does not exist yet.

## Solution Overview
Instead of a hard-coded map in the binary which will inevitably drift and become stale, implement a clean two-tier configuration lookup hierarchy to resolve a model's context window size:
1. **Agent-Level Override**: Add a `context_window` property (e.g., `ContextWindow *int `yaml:"context_window,omitempty"``) directly to `AgentConfig` in `internal/registry`. If set on the agent/persona, it wins immediately.
2. **Global Model-Context Table**: Add a `model_context_windows` mapping (e.g., `map[string]int`) to the global project config/registry settings, allowing users to define context windows for custom models globally.
3. **Conservative Default**: An ultimate fallback constant (`32768`) for completely unknown models where no override is provided in config.

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
     - Fallback to the conservative default.
3. **Unit Tests**:
   - Write tests in `internal/payload/contextwindow_test.go` verifying each tier of the hierarchy: agent config override, global mapping override, and default fallback.


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
- [ ] `ContextWindowTokens` resolves context windows using the config settings.
- [ ] Setting `context_window: 65536` on an agent config correctly returns `65536`.
- [ ] Setting a model in `model_context_windows` globally correctly overrides the value for that model.
- [ ] An unconfigured model fallback returns the conservative default constant (`32768`), never zero and never an error.

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `TestContextWindow_AgentOverride` — verifies explicit `context_window` on `AgentConfig` wins.
- `TestContextWindow_GlobalConfigOverride` — verifies model mapping in `model_context_windows` globally works.
- `TestContextWindow_DefaultFallback` — verifies unmapped models return the default constant (`32768`).

**Integration Tests:**
- None — pure function, no integration surface.

**Test Files:**
- `internal/payload/contextwindow_test.go`

## Risk Mitigation
- **Config file drift.** Users can update context lengths in `config.yaml` as providers release model updates, meaning zero code changes are required for new models.
- **Key mismatch with `AgentConfig.Model`.** Mitigated by using the identical model string resolved by the registry.

## Dependencies
- None — this is the foundational task other tasks (F2/F3) build on.

## Definition of Done
- [ ] `internal/payload/contextwindow.go` created with `ContextWindowTokens(model string) int` exported from package `payload`
- [ ] `internal/payload/contextwindow_test.go` created covering known-model and unknown-model-default cases
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes
- [ ] No changes made to `internal/payload/budget.go` or any other existing file — this task is purely additive
