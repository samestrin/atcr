# Task 01: Per-Model Context-Window Resolver

**Source:** Plan 19.10 – Debt Item #1
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement

The payload sizer applies a single global byte budget (`payload_byte_budget`, default 512 KiB) identically to every reviewer, regardless of that reviewer's actual model context window. This has no per-model token awareness at all: a 32k-window model (`dax`) and a 144k-window model (`otto`) receive the same byte cap. Confirmed failure mode from the 19.6 live run: `dax`'s effective input sizing produced exactly 24577 tokens = 32768 − 8192 + 1 — one token past its true available window once the 8192-token output cap is accounted for — causing an outright overflow instead of graceful degradation. Every downstream fix in this plan (effective per-agent budget, window-aware chunking, overflow policy dispatch) depends on first having a deterministic way to ask "how many tokens does this reviewer's model actually have to work with?" That building block does not exist yet.

## Solution Overview

Implement a static, deterministic model-id → context-window table in a new file `internal/payload/contextwindow.go`. The table maps known model identifiers to their full context-window sizes in tokens. Unknown models receive a conservative default window. The resolver is intentionally **not** configurable per the original requirements: a per-model context-window config field on `AgentConfig`/`Provider` and a global `model_context_windows` mapping are explicitly out of scope for this sprint (see `original-requirements.md`:Out of Scope). A static table keeps the hot-path sizing logic a pure function of `(entries, model, config)` with no network calls and no config drift risk, and it is the single source of truth until Epic 19.7 (Live Model Resolution) feeds live values later.

The exported resolver `ContextWindowTokens(model string) int` keys on the same model-id string already used by `diffCacheKey`, `AgentStatus.Model`, and the persona roster (`AgentConfig.Model`).

## Technical Implementation

### Steps

1. Create `internal/payload/contextwindow.go` in the `payload` package, alongside `internal/payload/budget.go`. Follow the same doc-comment style (what/why/caveats).
2. Define a package-private static map from model id to token window. Seed it with at least the models known from the 19.6 roster and any other models already referenced in `personas/`:
   - `dax` → 32768
   - `otto` → 144941
   Add additional models as needed so that the AC1 regression (every model in `personas/` is either present or receives the conservative default) passes. Keep the table clearly the single source of truth with a prominent comment.
3. Define a conservative default constant for unknown models. Per the original requirements this must be a safely small value (e.g., 32768) so an unlisted small-window model is never over-filled. Document why the default is conservative.
4. Export `func ContextWindowTokens(model string) int` that:
   - Normalizes the model id if necessary (e.g., exact string match; no provider/version prefix stripping unless the roster uses such prefixes),
   - Looks up the model in the static map,
   - Returns the map value if found, otherwise the conservative default.
   The function must never return zero or an error.
5. Write `internal/payload/contextwindow_test.go` mirroring `internal/payload/budget_test.go`:
   - `TestContextWindowTokens_KnownModels` — known models resolve to their windows.
   - `TestContextWindowTokens_UnknownDefaults` — an unmapped model returns the conservative default, never zero.
   - `TestContextWindowTokens_AllPersonasCoveredOrDefault` — walk `personas/` (or a stable test fixture) and assert every configured model is either in the table or gets the default (AC1 guard).

## Files to Create/Modify

- [NEW] `internal/payload/contextwindow.go` – static model-id → token-window table + `ContextWindowTokens`
- [NEW] `internal/payload/contextwindow_test.go` – unit tests for known models, unknown-model default, and persona coverage

## Documentation Links

- [Context-Window Resolver](../documentation/context-window-resolver.md)

## Related Files (from codebase-discovery.json)

- `internal/payload/budget.go` – sibling file; doc-comment style and `FileEntry`/`Truncation` primitives this resolver sits alongside (untouched by this task)
- `internal/payload/budget_test.go` – test-pattern source (same-package tests, testify, `TestXxx_Scenario` naming)
- `internal/registry/config.go:298` – `AgentConfig.Model` field; the string this resolver is keyed on, shared with `diffCacheKey` and `AgentStatus.Model`
- `personas/*.md` – roster persona files whose `model:` frontmatter must be covered by the static table or the conservative default

## Success Criteria

- [ ] `internal/payload/contextwindow.go` exists with an exported `ContextWindowTokens(model string) int`.
- [ ] `ContextWindowTokens` returns the correct window for every known model in the static table.
- [ ] `ContextWindowTokens` returns a conservative default (e.g., 32768) for any unmapped model, never zero and never an error.
- [ ] No config-schema changes are made to `AgentConfig`, `Provider`, `ProjectConfig`, `Registry`, or `Settings` for context-window lookup — the table is purely static.
- [ ] Unit tests cover known models, unknown-model default, and persona coverage (AC1).
- [ ] `go build ./...` succeeds and `go test ./...` passes.

## Manual Code Review

- [ ] Codebase has been reviewed

## Test Strategy

**Unit Tests:**

- `TestContextWindowTokens_KnownModels` — verifies `dax` → 32768 and `otto` → 144941.
- `TestContextWindowTokens_UnknownDefaults` — verifies an arbitrary unmapped model id returns the conservative default.
- `TestContextWindowTokens_AllPersonasCoveredOrDefault` — AC1 regression asserting every model referenced in `personas/` is either present in the table or receives the conservative default.

**Integration Tests:**

- None — pure function, no integration surface.

**Test Files:**

- `internal/payload/contextwindow_test.go`

## Risk Mitigation

- **Static table drift for newly released models.** Frontier models added after this sprint will receive the conservative default, potentially under-filling them. This is an accepted trade-off per the original requirements; Epic 19.7 (Live Model Resolution) is the planned future replacement. Mitigate by documenting the table as the single source of truth and by making the default conservative rather than wrong.
- **Key mismatch with `AgentConfig.Model`.** Mitigated by using the identical model string resolved by the registry and already used in `diffCacheKey`/`AgentStatus.Model`.
- **Temptation to add config overrides.** The original requirements explicitly defer per-model window config to a future epic. Resist adding `context_window` on `AgentConfig` or `model_context_windows` in project config — doing so would expand scope, introduce precedence logic, and violate the Determinism/Conservatism NFRs for this sprint.

## Dependencies

- None — this is the foundational task other tasks (F2/F3) build on.

## Definition of Done

- [ ] `internal/payload/contextwindow.go` created with `ContextWindowTokens(model string) int` exported from package `payload`
- [ ] `internal/payload/contextwindow_test.go` created covering known-model, unknown-model-default, and persona-coverage cases
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes
- [ ] No changes made to `internal/payload/budget.go`, `internal/registry/config.go`, `internal/registry/project.go`, `internal/registry/precedence.go`, or any other existing file — this task is purely additive
