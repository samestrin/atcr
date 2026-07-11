# Context-Window Resolver (F1)

**Priority: CRITICAL**

## Overview

Plan 19.10 replaces the single global byte budget applied identically to every reviewer with per-model token awareness. The first building block is a static, deterministic resolver that maps a model id to its context-window size in tokens.

> Source: [plan.md](../plan.md):Objectives:F1, [original-requirements.md](../original-requirements.md):Requirements:F1

The resolver must be named distinctly from the existing per-chunk diff-line budget (`MaxContextLines`) so the two concepts are never confused. The recommended name is `ContextWindowTokens(model string) int`.

> Source: codebase-discovery.json:architecture_notes:3

## Key Concepts

- **Static table, no hot-path network calls.** The resolver ships a hard-coded map from model id to token window. Unknown models fall back to a conservative default window. This satisfies the Determinism NFR: sizing is a pure function of `(entries, model, config)`.

  > Source: [original-requirements.md](../original-requirements.md):Non-Functional:Determinism, [plan.md](../plan.md):Success Criteria

- **Conservative default for unknown models.** Frontier models added after this sprint will receive the conservative default rather than failing or over-filling. An AC1 regression should assert every model currently in `personas/` is either present in the table or receives the default.

  > Source: codebase-discovery.json:integration_gaps:Static context-window table maintenance

- **Keys on `AgentConfig.Model`.** The same string already keys `diffCacheKey`, `AgentStatus.Model`, and the persona roster. Keeping the resolver consistent with that string avoids mismatches between cache, status, and sizing.

  > Source: codebase-discovery.json:architecture_notes:2, codebase-discovery.json:semantic_matches:AgentConfig.Model

## Implementation Guidance

- Add a new file `internal/payload/contextwindow.go` in the `payload` package, alongside `internal/payload/budget.go`.

  > Source: codebase-discovery.json:files_to_create:internal/payload/contextwindow.go

- Expose a single function:

  ```go
  package payload

  // ContextWindowTokens returns the model's full context-window size in tokens.
  // Unknown models receive a conservative default.
  func ContextWindowTokens(model string) int
  ```

- Use the existing `internal/payload/budget.go` doc-comment style (what, why, and caveats).

  > Source: codebase-discovery.json:files_to_create:internal/payload/contextwindow.go:based_on

## Quick Reference

| Model Window | Example Models (from 19.6 roster) |
|--------------|-----------------------------------|
| 32,768       | `dax`                             |
| 144,941      | `otto`                            |
| conservative default | any unlisted model        |

## Related Documentation

- [Per-Agent Budget & Chunking](per-agent-budget-and-chunking.md) — consumes `ContextWindowTokens` to derive the effective input budget and chunk plan (F2/F3)
- [Config YAML Parsing](config-yaml-parsing.md) — config surface for the plan
- `internal/payload/budget.go` — sibling file and shared `FileEntry` primitive
- `internal/registry/config.go` — `AgentConfig.Model` field
- `codebase-discovery.json` — discovery findings for F1
