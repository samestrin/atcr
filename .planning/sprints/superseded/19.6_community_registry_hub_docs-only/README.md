# Sprint 19.6: Community Registry Hub

**Status:** Active
**Complexity:** 2/12 (SIMPLE)
**Timeline:** 1 day
**Phases:** 2
**Execution Mode:** Continuous (no adversarial review, no gating — see sprint-design.md's Recommended Flags)

## Overview

Ships the single in-repo task of Plan 19.6: updating `docs/personas-install.md` and `README.md` to recommend installing a curated pack of 3 default, model-tuned reviewer personas (Anthropic Claude, OpenAI GPT, Google Gemini) via the existing `atcr personas install` community channel. The persona content itself (Stories 1-2 — YAML + prompt templates + fixtures, publication to `index.json`) is authored and published in the separate `atcr/personas` repo and is tracked here only as an externally-verified dependency; this sprint's own execution loop implements and verifies only Story 3 (the in-repo doc update).

## Timeline

Single-session sprint (~1-2 hours of hands-on editing across 2 phases): draft the insertion content (Phase 1), then apply and validate the additive markdown edits (Phase 2). No fixed calendar date — gated only on whether Stories 1-2 have published real persona/bundle names in the external repo by execution time (if not, generic placeholder language ships instead, per the story's documented risk mitigation).

## Expected Outcomes

- `docs/personas-install.md`'s "Quick walkthrough" section recommends the default persona pack with a runnable install example.
- `README.md`'s "## Quickstart" section adds a step recommending the default pack alongside `atcr init`/provider setup.
- Both files' existing numbered steps are preserved in order and command text — the edit is purely additive.

## Risk Summary (top 3)

1. **Sequencing:** Story 3 is best implemented after Stories 1-2 publish real persona/bundle names in the external `atcr/personas` repo, so the doc can cite a concrete install command. Mitigation: use generic placeholder language if names aren't ready yet; tighten later.
2. **Structural disruption:** Inserting the new step could disrupt the existing "Quick walkthrough" or numbered Quickstart structure. Mitigation: treat the edit as strictly additive — insert one step, do not reorder or rewrite existing steps; verify via `git diff`.
3. **No automated test gate:** This sprint has no automated test to gate correctness (docs-only, 0/9 plan ACs require a new Go test). Mitigation: Definition of Done is verified via `git diff` scoped to the 2 files plus manual checklist review against AC 03-01/03-02.

## Sprint Assets

- [Sprint Plan](sprint-plan.md)
- [Metadata](metadata.md)
- [Sprint Knowledge Manifest](sprint-knowledge.yaml)
- [Plan Bundle](plan/) — original-requirements.md, plan.md, sprint-design.md, user-stories/, acceptance-criteria/, codebase-discovery.json, documentation/
