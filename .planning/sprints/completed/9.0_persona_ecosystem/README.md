# Sprint 9.0: Persona Ecosystem

**Status:** Active — Ready for Execution
**Type:** ✨ Feature
**Complexity:** 10/12 (VERY COMPLEX)
**Branch:** `feature/9.0_persona_ecosystem`
**Execution Mode:** Gated 🚧 | Adversarial Review: ENABLED 🎯 (inline: CRITICAL/HIGH)

---

## Overview

Expand ATCR's reviewer panel beyond the 6 generalist built-in personas by shipping curated domain-specific bonus personas with the binary and adding an `atcr personas` CLI for installing, listing, searching, removing, testing, and upgrading community-contributed personas. Personas gain a `language` scope field that drives language-aware skeptic routing, and per-persona corroboration scores make persona quality visible.

This is the **in-repo** half of Epic 9.0 (T1, T2, T5, T6, T8 + in-repo docs). The community-repo tasks (T3/T4 + community half of T7) are descoped to a separate work item.

---

## Timeline

**17 working days across two sprints, 6 phases:**

| Sprint | Phases | Days | Focus |
|--------|--------|------|-------|
| A — Registry + Verify Internals | 1-3 | 1-7 | T8 Language field → T8 routing → T1 bonus personas |
| B — Surface Layer | 4-6 | 8-17 | T2 CLI → T5 bundles + T6 scores → T7-in-repo docs |

**Pattern:** Foundation → Core Routing → Built-in Personas → CLI Surface → Bundles+Scores → Docs+Validation

---

## Expected Outcomes

- 3 bonus built-in personas (`sentinel`, `tracer`, `idiomatic`) with CI-tested, network-free fixtures
- `atcr personas` CLI with 6 subcommands; root exposes 15 subcommands
- `AgentConfig.Language` field + language-aware two-partition `SelectEligibleSkeptics` routing with silent fallback
- Domain bundles `bundle/django` and `bundle/go-production` via embedded YAML manifests
- `atcr personas list --scores` wired to scorecard corroboration data
- In-repo install guide, authoring template, registry/example updates
- `Names()` returns 9; coverage ≥80% for new `internal/personas/` package; zero live network in CI

---

## Risk Summary (Top 3)

1. **Path traversal on persona install** — user-supplied name flows to filesystem. Mitigation: validate `name` against `[a-zA-Z0-9_/-]+`, reject `..`/absolute; validate community YAML via `validateAgent` before any disk write.
2. **CI failure windows from non-atomic commits** — `TestNames_ReturnsAllNine` and `TestRootCmd_HasExactlyFifteenSubcommands`. Mitigation: atomic-commit rules (test bump + implementation in the same GREEN commit).
3. **`SelectEligibleSkeptics` caller / `normalizeExt` consistency** — single-caller assumption + dual-path canonicalization. Mitigation: `grep -r` before implementing; shared `normalizeExt` helper covered by dedicated dot/dotless test.

Full risk analysis: [plan/sprint-design.md](plan/sprint-design.md#risk-analysis).

---

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Executable phase-by-phase TDD plan (gated) |
| [metadata.md](metadata.md) | Plan + sprint tracking, single source of truth |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge entries created/referenced |
| [plan/](plan/) | Archived plan source (stories, ACs, design, docs) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [plan/original-requirements.md](plan/original-requirements.md) | Source of truth |

---

**Next:** `/refine-sprint @.planning/sprints/active/9.0_persona_ecosystem/` then `/execute-sprint`
