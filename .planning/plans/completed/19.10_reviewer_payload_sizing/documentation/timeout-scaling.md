# Timeout Scaling (F6)

**Priority: IMPORTANT**

## Overview

The existing 600-second timeout wall (`DefaultTimeoutSecs = 600` in `internal/registry/config.go:25`) is too tight for slow local backends under oversized payloads. Plan 19.10 scales the request timeout with payload size and chunk count so a small-window model fanned into many chunks on a slow local backend no longer hits the wall.

> Source: [plan.md](../plan.md):Objectives:F6, [original-requirements.md](../original-requirements.md):Problem Statement:4

This is load-bearing under the chunk-to-fit design: a 32k model fanned into 6+ chunks multiplies wall-clock. Timeout scaling and chunking must be co-designed.

> Source: [original-requirements.md](../original-requirements.md):Technical Approach:Timeout coupling

## Key Concepts

- **Read the already-resolved timeout.** `internal/registry/config.go` exposes `AgentConfig.EffectiveTimeoutSecs()` (line ~948). Per the plan constraints, F6 reads this resolved value in `internal/fanout` and scales it; it does **not** modify the registry resolver or add a new config-schema timeout field.

  > Source: codebase-discovery.json:semantic_matches:AgentConfig.EffectiveTimeoutSecs, [original-requirements.md](../original-requirements.md):Constraints

- **Scale at the deadline-setting seam.** The deadline is applied in:
  - `internal/fanout/engine.go:610` (`invokeAgent`, defined at `:604`; the `context.WithTimeout` deadline seam is `:610`)
  - `internal/fanout/review.go:516`

  > Source: `internal/fanout/engine.go:610` (`invokeAgent` deadline), `internal/fanout/review.go:516`; matches plan.md "engine.go:610"

- **Scaling inputs.** The scaled timeout should account for:
  - chunk count (more chunks → more wall-clock, especially in serial lanes)
  - payload size / effective budget
  - a raised ceiling for slow local backends

  > Source: [plan.md](../plan.md):Objectives:F6, [original-requirements.md](../original-requirements.md):Requirements:F6

- **`EffectiveTimeoutSecs` remains the base.** The resolver itself is untouched; fanout applies a multiplier or ceiling to the resolved base value based on the derived chunk plan.

## Implementation Guidance

- Compute the scaled timeout in `internal/fanout` where the chunk plan is already known (near the per-agent effective-budget/chunk-plan derivation in `review.go` or at `engine.go:610`).

- Keep the scaling deterministic from `(entries, model, config)` — no live/network inputs on the hot path.

- Add coverage in AC6 that the previously-timed-out agents (`greta`, `vera`, `brad`) complete on a large-but-valid multi-chunk payload without hitting the wall.

  > Source: [original-requirements.md](../original-requirements.md):Acceptance Criteria:AC6

## Related Documentation

- [Per-Agent Budget & Chunking](per-agent-budget-and-chunking.md) — supplies chunk count used for scaling (F2/F3)
- [on_overflow Policy](on-overflow-policy.md) — chunk/truncate/fallback dispatch (F4)
- `internal/registry/config.go` — `DefaultTimeoutSecs`, `EffectiveTimeoutSecs`
- `internal/fanout/engine.go` — `invokeAgent` deadline application
- `codebase-discovery.json` — discovery findings for F6
