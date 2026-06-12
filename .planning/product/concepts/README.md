# ATCR Product Concepts — Ranked Index

**Last Updated:** 2026-06-11
**Concepts:** 6

This directory holds productization concepts for ATCR. Each file is a standalone analysis (problem, solution, effort, moat, risks). This README is the **source of truth for ranking** — where an individual file's `Priority` field and this table disagree, this table wins.

---

## Executive Summary

ATCR's defensible asset is the **deterministic reconciler** — cluster → dedupe → confidence-by-cross-model-agreement, with disagreements preserved instead of flattened. Every concept below should be judged by one question: *does it sell something that emerges from that architecture, or does it bolt on infrastructure anyone could build?*

That lens produces a clear pattern. **The highest-ROI ideas surface data the engine already produces and throws away** — they are cheap, pure-OSS, and uniquely possible *because* of the reconciler. The lowest-ROI-per-week ideas build new infrastructure (dashboards, hosted services, enterprise plumbing) that carries real revenue ceilings but long timelines, maintenance burden, and little architectural moat.

Three structural findings:

1. **Two near-free wins are sitting in the existing output.** *Disagreement Radar* (~1.5 wks) and the local half of the *Model-Eval Leaderboard* (~2 wks) are mostly projections over `ambiguous.json` and reconcile records that already exist. They are the most ATCR-native ideas, the cheapest, and the most "OSS fun" — they make the core thesis *visible* and demoable.

2. **The eval/leaderboard angle is the genuine differentiator and it compounds with the roadmap.** Corroboration is a weak proxy today, but the planned agent ladder turns it into near-ground-truth for free: *survived-a-skeptic* (Epic 3.0) and *demonstrated-by-repro* (Epic 5.0) are real quality labels. No competitor that flattens output can produce this measurement.

3. **Revenue concepts should be sequenced behind adoption, not ahead of it.** *Team Edition* has the highest revenue ceiling but the longest build, longest sales cycle, and least architectural moat — it is the eventual monetization, not the next move. *CI Integration* is not a differentiator at all; it is table-stakes distribution that gates adoption of everything else, so a thin slice of it should land early.

**Recommended near-term sequence:** Disagreement Radar → local Model-Eval scorecard/CLI → a thin CI slice (GitHub Action + `--fail-on`) to unblock adoption → revisit Reconciler Library / public Leaderboard once there are real users → Team Edition only after OSS pull is proven.

---

## Ranked by ROI

ROI = strategic payoff ÷ engineering cost, weighted toward OSS-native ideas that leverage what already exists. "Depends on" notes architectural or sequencing prerequisites.

| # | Concept | Dev Time | ROI | Type | Depends on | One-line rationale |
|---|---------|----------|-----|------|------------|--------------------|
| 1 | [Disagreement Radar](disagreement-radar.md) | ~1.5 wks | **Very High** | OSS feature | reconciler (built) | Projection over data already kept; most ATCR-native; cheapest; high talkability |
| 2 | [Model-Eval Leaderboard](model-eval-leaderboard.md) | 2 wks local / ~5 full | **High** | OSS feature → optional revenue | reconciler (built); sharpened by 3.0/5.0 | The real differentiator; a trusted measurement that falls out of the design; top-of-funnel magnet |
| 3 | [Reconciler Library](reconciler-library.md) | 5-7 wks | **High (long-term)** | OSS infra / standards play | reconciler extract | The core innovation as the reference impl; standards gravity, but heavier and adoption-by-others is speculative |
| 4 | [CI Integration](ci-integration.md) | 4-5 wks | **Medium (prerequisite)** | OSS distribution | — | Not a differentiator — table stakes that gate adoption; ship a thin slice early, defer history/dashboard |
| 5 | [Persona Ecosystem](persona-ecosystem.md) | 3-4 wks | **Medium** | OSS ecosystem | community participation | Network-effect/adoption play, no direct revenue; complements the leaderboard (more personas → richer eval matrix) |
| 6 | [Team Edition](team-edition.md) | 6-10 wks | **Medium (high ceiling, slow)** | Revenue | history, CI, audit plumbing | Highest revenue ceiling but longest build + sales cycle + least moat; the eventual monetization, not the next move |

### Tiering

- **Tier 1 — Do next (OSS, cheap, architecture-leveraged):** Disagreement Radar, Model-Eval Leaderboard (local half).
- **Tier 2 — Adoption enablers:** CI Integration (thin slice), Persona Ecosystem.
- **Tier 3 — Bigger bets, sequence behind adoption:** Reconciler Library, public Leaderboard, Team Edition.

---

## Cross-concept dependencies

- **Model-Eval Leaderboard** consumes the same per-finding reviewer-attribution the **Reconciler Library** would expose, and its headline metrics only become rigorous once **Epic 3.0 (adversarial verification)** and **Epic 5.0 (executing reviewers)** land — so its credibility tracks the roadmap.
- **Disagreement Radar** is the input queue for **Epic 4.0 (cross-examination)**: the radar *surfaces* tension, 4.0 *resolves* it.
- **Persona Ecosystem** widens the model×persona matrix the **Leaderboard** scores — more personas make the eval richer.
- **Team Edition** reuses **CI Integration's** finding-history and audit primitives; building CI history first de-risks it.

## See also

- `../roadmap/README.md` — the engineering ladder (Epics 1.0→5.0) these concepts ride on top of.
