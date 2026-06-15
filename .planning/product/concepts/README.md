# ATCR Product Concepts — Ranked Index

**Last Updated:** 2026-06-14
**Concepts:** 12

This directory holds productization concepts for ATCR. Each file is a standalone analysis (problem, solution, effort, moat, risks). This README is the **source of truth for ranking** — where an individual file's `Priority` field and this table disagree, this table wins.

---

## Executive Summary

ATCR's defensible asset is the **deterministic reconciler** — cluster → dedupe → confidence-by-cross-model-agreement, with disagreements preserved instead of flattened. Every concept below should be judged by one question: *does it sell something that emerges from that architecture, or does it bolt on infrastructure anyone could build?*

### The Monetization Landscape

The concepts fall into three categories:

1. **OSS-native features that surface existing data** (cheap, high talkability, zero direct revenue): Disagreement Radar, local Model-Eval Leaderboard, Persona Ecosystem. These are marketing, not monetization. Build them because they're cheap and demoable.

2. **Services plays that monetize expertise** (fast revenue, doesn't scale): Model Selection Consulting, Enterprise Persona Development. These fund OSS development early on. You have unique knowledge; sell it.

3. **Product plays that monetize infrastructure** (slow revenue, scales): Review-as-a-Service API, Team Edition + Compliance, Finding Remediation, Review Intelligence Analytics. These are the long-term business.

### The Fastest Roads to Revenue

If you need money in the next 90 days:

1. **Model Selection Consulting** (1-2 wks, $5k-20k/engagement) — The leaderboard data answers "which model should I use?" Sell the evaluation as a service. Every team building with LLMs asks this question.

2. **Enterprise Persona Development** (1-2 wks/persona, $10k-50k/persona) — Custom personas for domain-specific needs. Teams don't want to write prompts; they want working reviewers.

3. **Review-as-a-Service API** (4-6 wks, per-review or subscription) — Hosted API: POST a diff, get back a multi-model reconciled review. The SaaS path.

If you need money in the next 6 months:

4. **Team Edition + Compliance Wrapper** (6-10 wks, enterprise license) — The self-hosted enterprise product. Add the compliance reporting (SOC 2, HIPAA) and sell to regulated industries.

5. **"Survived-a-Skeptic" Certification** (2-3 wks post-3.0, per-certification) — Once adversarial verification lands, sell a "certified" badge. "This codebase survived adversarial AI review."

If you need money in the next 12 months:

6. **Finding Remediation** (4-6 wks, premium tier) — Auto-fix findings. Detection is half the problem; remediation is the other half.

7. **Review Intelligence Analytics** (4-6 wks, subscription) — Aggregate finding data across repos to answer: "What are the most common bugs in Go codebases?" Data product, not tool.

### Structural Findings

1. **Two near-free wins are sitting in the existing output.** *Disagreement Radar* (~1.5 wks) and the local half of the *Model-Eval Leaderboard* (~2 wks) are mostly projections over `ambiguous.json` and reconcile records that already exist. They are the most ATCR-native ideas, the cheapest, and the most "OSS fun" — they make the core thesis *visible* and demoable.

2. **The eval/leaderboard angle is the genuine differentiator and it compounds with the roadmap.** Corroboration is a weak proxy today, but the planned agent ladder turns it into near-ground-truth for free: *survived-a-skeptic* (Epic 3.0) and *demonstrated-by-repro* (Epic 5.0) are real quality labels. No competitor that flattens output can produce this measurement.

3. **Services fund OSS, product scales.** Consulting and custom persona development generate revenue now. SaaS and enterprise licenses generate revenue later. Both are needed.

4. **Revenue concepts should be sequenced behind adoption, not ahead of it.** *Team Edition* has the highest revenue ceiling but the longest build, longest sales cycle, and least architectural moat — it is the eventual monetization, not the next move. *CI Integration* is not a differentiator at all; it is table-stakes distribution that gates adoption of everything else, so a thin slice of it should land early.

**Recommended near-term sequence:** Disagreement Radar → local Model-Eval scorecard/CLI → a thin CI slice (GitHub Action + `--fail-on`) to unblock adoption → **start selling consulting/custom personas** → revisit Reconciler Library / public Leaderboard once there are real users → **launch Review-as-a-Service API** → Team Edition + Compliance only after OSS pull is proven.

---

## Ranked by ROI

ROI = strategic payoff ÷ engineering cost, weighted toward OSS-native ideas that leverage what already exists. "Depends on" notes architectural or sequencing prerequisites.

| # | Concept | Dev Time | ROI | Type | Depends on | One-line rationale |
|---|---------|----------|-----|------|------------|--------------------|
| 1 | [Disagreement Radar](disagreement-radar.md) | ~1.5 wks | **Very High** | OSS feature | reconciler (built) | Projection over data already kept; most ATCR-native; cheapest; high talkability |
| 2 | [Model-Eval Leaderboard](model-eval-leaderboard.md) | 2 wks local / ~5 full | **High** | OSS feature → optional revenue | reconciler (built); sharpened by 3.0/5.0 | The real differentiator; a trusted measurement that falls out of the design; top-of-funnel magnet |
| 3 | [Review-as-a-Service API](review-as-a-service.md) | 4-6 wks | **High (fast revenue)** | SaaS revenue | core (built) | Hosted API, per-review or subscription; fastest path to scalable revenue if you can get traction |
| 4 | [Model Selection Consulting](model-selection-consulting.md) | 1-2 wks | **High (immediate)** | Services | leaderboard data | Sell the leaderboard as a service; every team asks "which model should I use?"; funds OSS development |
| 5 | [Enterprise Persona Development](enterprise-persona-dev.md) | 1-2 wks/persona | **High (immediate)** | Services | persona ecosystem | Custom personas for domain-specific needs; high-margin consulting; doesn't scale but funds product |
| 6 | [Reconciler Library](reconciler-library.md) | 5-7 wks | **High (long-term)** | OSS infra / standards play | reconciler extract | The core innovation as the reference impl; standards gravity, but heavier and adoption-by-others is speculative |
| 7 | [CI Integration](ci-integration.md) | 4-5 wks | **Medium (prerequisite)** | OSS distribution | — | Not a differentiator — table stakes that gate adoption; ship a thin slice early, defer history/dashboard |
| 8 | [Persona Ecosystem](persona-ecosystem.md) | 3-4 wks | **Medium** | OSS ecosystem | community participation | Network-effect/adoption play, no direct revenue; complements the leaderboard (more personas → richer eval matrix) |
| 9 | [Team Edition + Compliance](team-edition.md) | 6-10 wks | **Medium (high ceiling, slow)** | Revenue | history, CI, audit plumbing | Highest revenue ceiling but longest build + sales cycle + least moat; add compliance wrapper for regulated industries |
| 10 | [Finding Remediation](finding-remediation.md) | 4-6 wks | **Medium (hard)** | Premium feature | 2.0 tool-using reviewers | Auto-fix findings; detection is half the problem; competing with Copilot/Cursor; hard but valuable |
| 11 | ["Survived-a-Skeptic" Certification](survived-a-skeptic-cert.md) | 2-3 wks post-3.0 | **Medium (depends on 3.0)** | Compliance add-on | Epic 3.0 adversarial verification | Sell a "certified" badge; "This codebase survived adversarial AI review"; creating a new market |
| 12 | [Review Intelligence Analytics](review-intelligence-analytics.md) | 4-6 wks | **Medium (data product)** | Subscription | opt-in data aggregation | Aggregate finding data across repos; "state of code quality" report; crowded market but you have the data |

### Tiering

- **Tier 1 — Do next (OSS, cheap, architecture-leveraged):** Disagreement Radar, Model-Eval Leaderboard (local half).
- **Tier 2 — Immediate revenue (services):** Model Selection Consulting, Enterprise Persona Development.
- **Tier 3 — Adoption enablers:** CI Integration (thin slice), Persona Ecosystem.
- **Tier 4 — Scalable revenue (product):** Review-as-a-Service API, Team Edition + Compliance, Finding Remediation.
- **Tier 5 — Bigger bets, sequence behind adoption:** Reconciler Library, public Leaderboard, Review Intelligence Analytics.

---

## The Revenue Path

### Phase 1: Build the top-of-funnel (0-3 months)
- **Disagreement Radar** (1.5 wks) — the talk hook, the demo, the blog post
- **CI Integration thin slice** (1 wk) — GitHub Action + `--fail-on` (prerequisite for adoption)
- **Model-Eval Leaderboard local half** (2 wks) — the per-run scorecard, the `atcr leaderboard` CLI

**Goal:** Get users. Get attention. The leaderboard is the magnet.

**Revenue:** $0. This is investment.

### Phase 2: Monetize the expertise (3-6 months)
- **Model Selection Consulting** — sell the leaderboard data as a service ($5k-20k/engagement)
- **Enterprise Persona Development** — sell custom personas ($10k-50k/persona)
- **Review-as-a-Service API** — the SaaS path (per-review or subscription)

**Goal:** Generate revenue to fund product development. Consulting funds the OSS.

**Revenue:** $10k-100k (services + early SaaS).

### Phase 3: The enterprise play (6-12 months)
- **Team Edition + Compliance Wrapper** — the self-hosted enterprise product ($10k-50k/year per org)
- **"Survived-a-Skeptic" Certification** — the compliance badge ($500-2000/report)
- **Reconciler Library** — the standards play (if you have traction)

**Goal:** Enterprise revenue. Long sales cycles, but high ACV.

**Revenue:** $50k-500k (enterprise licenses + certifications).

### Phase 4: The moonshots (12+ months)
- **Finding Remediation** — auto-fix (premium tier, if you can crack it)
- **Review Intelligence Analytics** — the data product (if you have opt-in)
- **Public Leaderboard** — the lmarena-of-code-review (top-of-funnel, not revenue)

**Goal:** Category creation. This is where you become a platform, not a tool.

**Revenue:** $100k-1M+ (platform revenue).

---

## Cross-concept dependencies

- **Model-Eval Leaderboard** consumes the same per-finding reviewer-attribution the **Reconciler Library** would expose, and its headline metrics only become rigorous once **Epic 3.0 (adversarial verification)** and **Epic 5.0 (executing reviewers)** land — so its credibility tracks the roadmap.
- **Disagreement Radar** is the input queue for **Epic 4.0 (cross-examination)**: the radar *surfaces* tension, 4.0 *resolves* it.
- **Persona Ecosystem** widens the model×persona matrix the **Leaderboard** scores — more personas make the eval richer.
- **Team Edition** reuses **CI Integration's** finding-history and audit primitives; building CI history first de-risks it.
- **Review-as-a-Service API** is a thin wrapper around the existing CLI; it's the SaaS realization of the core.
- **Model Selection Consulting** and **Enterprise Persona Development** are services plays that fund OSS development; they don't scale but they generate immediate revenue.
- **"Survived-a-Skeptic" Certification** depends on Epic 3.0 landing; it's a compliance add-on that creates a new market.
- **Finding Remediation** builds on 2.0 tool-using reviewers; it's the auto-fix premium tier.
- **Review Intelligence Analytics** aggregates data from the **Team Edition** finding history; it's a data product, not a tool.

## See also

- `../roadmap/README.md` — the engineering ladder (Epics 1.0→5.0) these concepts ride on top of.
- `competitive-analysis.md` — competitive landscape for each concept, showing where ATCR differentiates.
