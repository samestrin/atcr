---
id: mem-2026-07-09-18350f
question: "Why did the original single-trailing-hyphen-segment version extraction in internal/personas (drift.go / upgrade.go) fail for qwen/glm/gpt-mini-style slugs, and what's the general fix pattern?"
created: 2026-07-09
last_retrieved: ""
sprints: [19.7_live_model_resolution]
files: [internal/personas/upgrade.go, internal/personas/drift.go]
tags: [sprint-learning, 19.7_live_model_resolution, correctness, version-parsing]
retrievals: 0
status: active
type: sprint-learning
---

# Why did the original single-trailing-hyphen-segment version 

## Decision

The original version-extraction logic assumed a model slug's version was always the LAST hyphen-delimited segment (e.g. "anthropic/claude-opus-4.8" → last segment "4.8"). This breaks for two distinct slug shapes: (1) vendor-glued versions where the version is fused inside a segment rather than standalone ("qwen3-coder-plus" → no segment is purely numeric; "glm-5v-turbo" → "5v" isn't a clean version token) — fixed in `versionFromSlug` (internal/personas/upgrade.go:233) with a two-pass approach: Pass 1 scans ALL segments right-to-left for a standalone `^v?\d+(\.\d+)*$` match (not just the last one), Pass 2 falls back to extracting an embedded version fused inside a segment via `embeddedVersionRe`. (2) non-trailing standalone version segments where the version sits in the MIDDLE of the slug, not at the end ("openai/gpt-5.4-mini" → segments ["gpt","5.4","mini"], version "5.4" is at index 1) — `deriveFamilyPrefix` (internal/personas/drift.go:176) needed the same right-to-left multi-segment scan (mirroring versionFromSlug's Pass 1) instead of checking only the last segment, so it correctly strips "5.4" and rejoins to "gpt-mini" rather than treating the whole unstripped slug as its own family (which silently disables newer-member drift detection for that tier forever). General lesson: any slug-parsing helper in this package that assumes "version is the last segment" needs to scan all segments, not just the last, because different vendors glue/position their version tokens differently.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/upgrade.go
- internal/personas/drift.go
