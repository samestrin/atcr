# Concept: Model Selection Consulting

**Status:** Conceptual
**Created:** 2026-06-14
**Priority:** High (immediate revenue)

## Problem

Every team building with LLMs now asks: **which model is actually good at code review, and at what cost?**

The existing options are weak:
- Generic leaderboards (lmarena, MMLU, SWE-bench) measure task-solving, not *review* quality.
- Vendor benchmarks are self-reported and non-reproducible.
- "Vibes" — teams pick a model because it's the default in their tool.

Teams spend weeks evaluating models on their own, often picking the wrong one. They'd pay for expertise + data.

## Solution

**Sell the evaluation as a service.** You already have the leaderboard data (from the Model-Eval Leaderboard concept). Productize it as a consulting engagement:

### What you deliver

1. **Codebase-specific evaluation** — Run the ATCR panel (N models × M personas) on a sample of the client's code (10-20 PRs).
2. **Comparative analysis** — Which model/persona combinations work best for *their* codebase?
3. **Cost-efficiency report** — Cost-per-corroborated-finding, latency, token usage.
4. **Recommendation** — "Use this panel for your use case."

### Why this works

- You have the infrastructure (ATCR + reconciler + scorecard).
- You have the expertise (you built it).
- The client doesn't want to learn ATCR; they want the answer.

### Engagement model

- **Tier 1: Standard evaluation** ($5k-10k) — 10 PRs, 3 models, written report.
- **Tier 2: Deep evaluation** ($15k-20k) — 20 PRs, 10 models, persona tuning, 2-hour walkthrough.
- **Tier 3: Ongoing advisory** ($2k/month) — Quarterly re-evaluation as models change.

## Key Features

| Feature | Description | Effort |
|---------|-------------|--------|
| Evaluation pipeline | Automated script to run ATCR on a client's sample PRs | 3 days |
| Report template | Standardized report format (comparative metrics, recommendation) | 2 days |
| Pricing page | Landing page with tiers, case study, signup | 2 days |
| Client onboarding | Process for collecting sample PRs, running eval, delivering report | 1 day |

## Revenue Model

**Services, not product:**
- Tier 1: $5k-10k per evaluation (1-2 weeks of work)
- Tier 2: $15k-20k per evaluation (2-3 weeks)
- Tier 3: $2k/month retainer (ongoing)

**Why this funds OSS:**
- Consulting generates immediate revenue.
- You're selling expertise, not infrastructure.
- The work is mostly automated (ATCR runs), so margins are high.
- Clients become users of the OSS tool.

## Engineering Effort

| Component | Effort | Notes |
|-----------|--------|-------|
| Evaluation pipeline | 3 days | Script to run ATCR on sample PRs, aggregate scorecards |
| Report template | 2 days | Markdown/PDF report with comparative metrics |
| Pricing page | 2 days | Landing page with tiers, case study, signup form |
| Client onboarding | 1 day | Process doc, intake form, delivery workflow |
| **Total** | **~1.5 weeks** | Mostly automation + documentation |

## Moat / Differentiation

- **You have the data.** The leaderboard is unique; no one else has reproducible model-eval for code review.
- **You have the expertise.** You built ATCR; you know the tradeoffs.
- **It's fast.** 1-2 weeks per engagement, not months.
- **It funds OSS.** Consulting revenue keeps the lights on while you build the product.

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Services don't scale | High | Medium | Use consulting to fund product development; transition to self-serve over time |
| Clients expect custom work | Medium | Medium | Standardize the engagement; say no to scope creep |
| Leaderboard isn't rigorous enough yet | Medium | High | Lead with "survived-a-skeptic" once 3.0 lands; be honest about the proxy |
| Low demand | Low | High | Start with your network; offer one free evaluation as a case study |

## Open Questions

- **How do you find clients?** Network, blog posts, conference talks, Hacker News?
- **What's the deliverable format?** PDF report, live walkthrough, both?
- **How do you price it?** Value-based (client saves $100k in API costs) or cost-plus (your time)?
- **Do you offer a free tier?** One free evaluation to generate a case study?

## Why This Is the Fastest Road to Revenue

1. **You already have the data.** The leaderboard is built; you just need to package it.
2. **You already have the expertise.** You built ATCR; you're the expert.
3. **It's fast.** 1-2 weeks per engagement, not months.
4. **High margins.** The work is mostly automated; you're selling knowledge, not labor.
5. **It funds OSS.** Consulting revenue keeps the lights on while you build the product.
6. **Clients become users.** Every consulting client is a potential OSS advocate.

**Time to first dollar:** 2-4 weeks (if you start today).

## References

- ATCR Model-Eval Leaderboard concept — the data source
- HashiCorp, Grafana — OSS companies that funded early development through services
- lmarena — the model evaluation leaderboard (different market, similar idea)
