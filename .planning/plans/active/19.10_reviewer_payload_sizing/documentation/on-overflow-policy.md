# on_overflow Degradation Policy (F4)

**Priority: CRITICAL**

## Overview

Plan 19.10 ships `on_overflow` as a real, configurable degradation policy instead of the current hardcoded shed-to-fit behavior. The policy is a ladder: `chunk` (default), `truncate`, `fallback`, and `fail`.

> Source: [plan.md](../plan.md):Objectives:F4, [original-requirements.md](../original-requirements.md):Requirements:F4, [original-requirements.md](../original-requirements.md):Proposed Solution:on_overflow ladder

For this sprint, `chunk` and `truncate` are implemented; `fallback` and `fail` are recognized config values but error clearly if their prerequisites are unmet. Unimplemented arms must never silently no-op.

> Source: [original-requirements.md](../original-requirements.md):Proposed Solution:on_overflow table, [original-requirements.md](../original-requirements.md):Acceptance Criteria:AC4

## Key Concepts

| Policy | Mechanism | Content | Model identity |
|--------|-----------|---------|----------------|
| `chunk` (default) | Epic 14.3 chunker, window-aware | whole | preserved |
| `truncate` | `ApplyByteBudget` byte-shed, flagged | lossy | preserved |
| `fallback` | litellm any→any fallback | whole | **swapped — must record provenance** |
| `fail` | hard fail, loud | — | — |

> Source: [original-requirements.md](../original-requirements.md):Proposed Solution:on_overflow table

- **`chunk` (default):** zero content loss. The diff is delivered whole across N appropriately-sized chunks per model. This is the preferred degradation path because it preserves both full content and model diversity.

  > Source: [original-requirements.md](../original-requirements.md):Non-Functional:No content loss on the default path

- **`truncate`:** reuses the existing `internal/payload/budget.go` `ApplyByteBudget` shed primitive. The action is recorded explicitly in `summary.json` so truncation is never silent.

  > Source: codebase-discovery.json:existing_patterns:Non-silent degradation record

- **`fallback` / `fail`:** recognized as valid config values but gated. `fallback` requires the provenance-recording work described in F5; without it, the implementation must error clearly rather than silently swap models. `fail` is trivial but must be explicit.

  > Source: [original-requirements.md](../original-requirements.md):Requirements:F4, [original-requirements.md](../original-requirements.md):Acceptance Criteria:AC4

- **Config key surface.** `on_overflow` is parsed from `.atcr/config.yaml` as a plain string policy key with a default of `chunk`. Unlike `max_sprint_plan_bytes` (F9), it does not need a pointer + `Effective*()` resolver.

  > Source: codebase-discovery.json:existing_patterns:Pointer-for-unset-vs-explicit-zero, [plan.md](../plan.md):Objectives:F4

## Implementation Guidance

- Add `internal/fanout/overflow.go` for policy dispatch, following the dispatch style of `internal/fanout/chunker.go`.

  > Source: codebase-discovery.json:files_to_create:internal/fanout/overflow.go

- The dispatch maps a policy value onto the existing primitives:
  - `chunk` → `chunkDiff` with per-model `maxLines`
  - `truncate` → `ApplyByteBudget`
  - `fallback` → return a clear error if F5 plumbing is absent
  - `fail` → return a clear error

- Add `internal/fanout/overflow_test.go` covering the default, the implemented arms, and the error behavior of the gated arms.

  > Source: codebase-discovery.json:files_to_create:internal/fanout/overflow_test.go

- Add the `on_overflow` key to `internal/registry/config.go` `Settings`/`ProjectConfig`/`Registry` and thread it through `internal/registry/precedence.go` `ResolveSettings`.

  > Source: codebase-discovery.json:files_to_modify:internal/registry/config.go, codebase-discovery.json:files_to_modify:internal/registry/precedence.go

- Document the key in `.atcr/config.yaml` and in `internal/registry/project.go` `DefaultProjectConfigYAML` scaffold.

  > Source: codebase-discovery.json:files_to_modify:.atcr/config.yaml, codebase-discovery.json:files_to_modify:internal/registry/project.go

## Related Documentation

- [Per-Agent Budget & Chunking](per-agent-budget-and-chunking.md) — supplies the chunk plan the `chunk` policy consumes (F2/F3)
- [Fallback Provenance](fallback-provenance.md) — required before `fallback` can be enabled (F5)
- [Config YAML Parsing](config-yaml-parsing.md) — YAML strict-mode conventions for the new config key
- `internal/payload/budget.go` — the `truncate` primitive
- `internal/fanout/chunker.go` — the `chunk` primitive
- `codebase-discovery.json` — discovery findings for F4
