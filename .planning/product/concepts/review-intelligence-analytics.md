# Concept: Review Intelligence Analytics

**Status:** Conceptual
**Created:** 2026-06-14
**Priority:** Medium (data product)

## Problem

ATCR generates a massive amount of labeled data: every review is a set of findings with severity, category, file, line, model, persona, corroboration status, and (after 3.0) survived-skeptic status.

This data is currently thrown away after each run. But aggregated across many repos, it answers questions every engineering leader asks:

- What are the most common bugs in Go codebases?
- Which packages have the highest defect density?
- Which model/persona combinations catch the most real bugs?
- How does code quality trend over time across our codebase?
- What categories of bugs are most expensive to fix?

No one else has this data. CodeClimate, SonarQube, and other static analysis tools have aggregate data, but they lack the multi-model consensus and adversarial verification that ATCR provides.

## Solution

**Aggregate finding data across many repos (with permission) and sell access to the analytics.** This is a data product, not a tool.

### What it looks like

A dashboard that answers:

- **State of Code Quality** — aggregate metrics across all participating repos
- **Bug Taxonomy** — most common categories, severities, languages
- **Model Performance** — which models/personas catch the most real bugs
- **Trend Analysis** — how code quality changes over time
- **Benchmarking** — how your codebase compares to similar repos

### Why this works

- You're sitting on a goldmine of labeled data.
- Every review is a data point.
- Aggregated, this becomes a "state of code quality" report.
- Teams will pay for benchmarking + trend analysis.

### Pricing

- **Free tier:** Access to aggregate reports (state of code quality, bug taxonomy)
- **Paid tier:** Access to benchmarking + trend analysis ($99-499/mo)
- **Enterprise:** Custom pricing for private dashboards + API access

## Key Features

| Feature | Description | Effort |
|---------|-------------|--------|
| Data aggregation | Aggregate finding data across repos (with permission) | 2 weeks |
| Anonymization | Strip identifying info before aggregation | 1 week |
| Dashboard | Web UI for analytics | 2 weeks |
| Benchmarking | Compare your codebase to similar repos | 1 week |
| Trend analysis | How code quality changes over time | 1 week |

## Revenue Model

**Data product, subscription:**
- Free tier: aggregate reports (state of code quality, bug taxonomy)
- Paid tier: benchmarking + trend analysis ($99-499/mo)
- Enterprise: custom pricing for private dashboards + API access ($1k-10k/mo)

**Why this works:**
- You have the data (every review is a data point).
- Aggregated, this becomes a "state of code quality" report.
- Teams will pay for benchmarking + trend analysis.
- Recurring revenue (subscriptions).

## Engineering Effort

| Component | Effort | Notes |
|-----------|--------|-------|
| Data aggregation | 2 weeks | Aggregate finding data across repos (with permission) |
| Anonymization | 1 week | Strip identifying info before aggregation |
| Dashboard | 2 weeks | Web UI for analytics |
| Benchmarking | 1 week | Compare your codebase to similar repos |
| Trend analysis | 1 week | How code quality changes over time |
| **Total** | **4-6 weeks** | |

## Moat / Differentiation

- **You have the data.** Every ATCR review is a data point; aggregated, this is unique.
- **Multi-model consensus.** CodeClimate/SonarQube lack this; their data is single-tool.
- **Adversarial verification (3.0).** The survived-skeptic label is a strong quality signal.
- **Benchmarking is valuable.** Teams want to know how they compare.

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Privacy concerns | High | High | Strict anonymization; opt-in only; publish the anonymization methodology |
| Low opt-in rate | High | Medium | Offer incentives (free premium tier for participants) |
| Crowded market | Medium | Medium | CodeClimate/SonarQube have aggregate data, but lack multi-model consensus |
| Data quality issues | Medium | High | Strict validation; only aggregate data from verified runs |
| High infrastructure costs | Low | Medium | Use efficient aggregation; archive old data |

## Open Questions

- **How do you get opt-in?** Incentivize with free premium tier?
- **How do you anonymize?** Strip file names? Repo names? Or just aggregate at a higher level?
- **What's the data schema?** What fields are aggregated, what's stripped?
- **How do you benchmark?** By language? By size? By industry?
- **How do you handle privacy?** Opt-in only? GDPR compliance?

## Why This Is a Long-Term Bet

1. **You need opt-in data.** You can't aggregate without permission.
2. **Privacy is hard.** Anonymization is tricky; GDPR is strict.
3. **The market is crowded.** CodeClimate, SonarQube already have aggregate data.
4. **But you have unique data.** Multi-model consensus + adversarial verification is a differentiator.

**Time to first dollar:** 6-12 months (if you can get opt-in + build the dashboard).

## Relationship to Other Concepts

- **Team Edition** — the finding history is the data source; Team Edition users opt-in to aggregation.
- **Model-Eval Leaderboard** — the model performance data is a subset of the analytics.
- **CI Integration** — the finding history is the data source; CI users opt-in to aggregation.
- **Review-as-a-Service API** — API users opt-in to aggregation.

## References

- CodeClimate, SonarQube — competitors (static analysis, aggregate data)
- Stack Overflow Developer Survey — the analogy (aggregate data as a product)
- State of JS, State of CSS — the analogy (annual reports as a product)
