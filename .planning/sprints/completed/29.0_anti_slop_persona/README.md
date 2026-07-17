# Sprint 29.0: Anti-Slop Persona Simon

**Status:** Active — Ready for Execution
**Type:** ✨ Feature
**Complexity:** 4/12 (MODERATE)
**Branch:** `feature/29.0_anti_slop_persona`
**Execution Mode:** Continuous | Adversarial Review: ENABLED 🎯 (inline: CRITICAL/HIGH, defer: MEDIUM/LOW)

---

## Overview

Ship a new community-registry persona, `simon`, hyper-focused exclusively on hunting down, flagging, and stripping out AI-generated code bloat — tautological comments, unnecessary abstractions, defensive-programming overkill, and dead/hallucinated code paths. The persona is fully wired into the existing fixture, roster, and index test gates. Pair it with a verified/refreshed marketing outline positioning ATCR's free community persona as the automated alternative to paid "slop cleanup" services.

---

## Timeline

**4 working days, 4 phases:**

**Pattern:** Story 1: RGR → Story 2: RGR → Story 3: Content Refresh → Validation

| Phase | Focus | Duration |
|-------|-------|----------|
| 1 | Author the `simon` persona unit (`simon.yaml`/`simon.md`) | 1 day |
| 2 | Fixture authoring + roster/index test-gate integration | 1 day |
| 3 | Blog post outline verification & refresh | 0.5 day |
| 4 | Full validation & adversarial review | 1 day |

---

## Expected Outcomes

- `personas/community/simon.yaml` + `personas/community/simon.md` — a 14th community persona, structurally modeled on `sonny.yaml`/`sonny.md`
- `personas/community/testdata/simon_fixture.patch` + `simon` registered in the `communityPersonas` Go roster and `personas/community/index.json`
- `go test ./personas/... ./internal/personas/... ./internal/registry/...` fully green with `simon` included
- `.planning/product/content/blog/slopfix-ai-code-bloat.md` refreshed — invalid `--persona` CTA replaced with verified `atcr personas install/test simon` commands

---

## Risk Summary (Top 3)

1. **Category word or Jaccard similarity collision** — `simon`'s Role+Focus language could drift above the 0.85 Jaccard ceiling against an existing persona (especially `sonny`), or its category word could collide with one of the 13 claimed values. Mitigation: cross-check the full claimed-word list before writing the roster/index rows; ground every Focus sentence in AI-authorship-specific artifacts rather than generic code-quality language.
2. **Partial roster/index registration** — a roster row without a matching `index.json` entry (or vice versa) fatally breaks the shared `personas` test package (`require.Len`) for any concurrent branch work. Mitigation: land the fixture, roster row, and `index.json` entry as one atomic commit (Phase 2 constraint).
3. **Blog CTA drift** — the outline cites a CLI invocation that doesn't exist. Mitigation: Phase 3's scripted grep verification against the shipped `simon` command surface (`atcr personas install/test simon`), confirmed again in Phase 4.

Full risk analysis: [plan/sprint-design.md](plan/sprint-design.md#risk-analysis).

---

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Executable phase-by-phase TDD plan (continuous) |
| [metadata.md](metadata.md) | Plan + sprint tracking, single source of truth |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge entries created/referenced |
| [plan/](plan/) | Archived plan source (stories, ACs, design, docs) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [plan/original-requirements.md](plan/original-requirements.md) | Source of truth |

---

**Next:** `/refine-sprint @.planning/sprints/active/29.0_anti_slop_persona/` then `/execute-sprint`
