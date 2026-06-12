# Concept: Model-Eval Engine + Public Leaderboard

**Status:** Conceptual
**Created:** 2026-06-11
**Priority:** High

## Problem

There is no trusted, reproducible answer to a question every team building with LLMs now asks: **which model is actually good at code review, and at what cost?**

The existing options are weak:
- Generic leaderboards (lmarena, MMLU, SWE-bench) measure task-solving, not *review* quality — finding real defects in a diff is a different skill from writing a patch.
- Vendor benchmarks are self-reported and non-reproducible.
- "Vibes" — teams pick a model because it's the default in their tool.

Meanwhile ATCR is **already generating the exact data that answers this** and throwing it away after each run. Every reconcile records which model/persona raised which finding, and which findings were corroborated by independent reviewers. Corroboration-by-other-models is a cheap, imperfect, but real quality proxy. The byproduct of doing the work *is* an evaluation dataset.

## Solution

Treat ATCR as a **model-evaluation engine** and expose the accumulated signal as a leaderboard.

### Per-run scorecard (the foundation)

After reconcile, emit a normalized eval record per reviewer per run:

```json
{
  "run_id": "2026-06-11T10:00:00Z-abc123",
  "model": "claude-sonnet-4-6",
  "persona": "bruce",
  "findings_raised": 12,
  "findings_corroborated": 7,
  "findings_solo": 5,
  "corroboration_rate": 0.58,
  "cost_usd": 0.04,
  "tokens": 18200,
  "latency_ms": 9100
}
```

The reconciler already computes everything needed — this is a *persistence + projection* step, not new analysis.

### Quality axes (improve as the roadmap lands)

The proxy gets sharper as the agent ladder advances:
- **Stage 1 (today):** corroboration rate — caught-by-2+ vs. solo.
- **Stage 3 (adversarial verification):** *survived-a-skeptic* rate — a far stronger label than corroboration. "This model's findings survive hostile refutation 80% of the time" is near-ground-truth.
- **Stage 5 (executing reviewers):** *demonstrated* rate — findings backed by a failing repro command are objectively true. This is real ground truth.

So the leaderboard's credibility compounds with the roadmap already planned — it is not a side quest.

### Derived metrics

- **Precision proxy** — corroborated / raised (and later survived / raised).
- **Cost-per-corroborated-finding** — the metric teams actually care about (`$/real bug`).
- **Persona×model matrix** — which combinations agree, which flake, which are redundant.
- **Recall proxy** — share of the panel's union of corroborated findings that each model caught.

### Surfaces

- `atcr leaderboard` — local CLI over the user's own accumulated runs ("for *my* codebase, sonnet beats gpt on cost-per-finding").
- Public leaderboard — opt-in submission format + aggregation; a static site. Methodology is published and reproducible (the reconciler is deterministic and OSS).
- Private team leaderboards — "did our review quality regress after the model upgrade?" (overlaps team-edition).

## Key Features

| Feature | Description | Effort |
|---------|-------------|--------|
| Per-run scorecard emitter | Normalized eval record per reviewer, written alongside artifacts | 1 week |
| Local `atcr leaderboard` | Aggregate scorecards into per-model/persona metrics | 1 week |
| Survived-skeptic axis | Wire Stage 3 verdicts into the scorecard (after Epic 3.0) | 3 days (post-3.0) |
| Public submission format | Versioned, anonymizable run-summary schema | 1 week |
| Public leaderboard site | Static site rendering aggregated submissions | 2 weeks |

## Revenue Model

**OSS-first; revenue is downstream and optional:**
- The local scorecard + `atcr leaderboard` are free OSS — they make ATCR stickier and self-evidently useful.
- The public leaderboard is a top-of-funnel attention magnet (the lmarena-of-code-review), not a paid product.

**Optional later:**
- Sponsored evals (a vendor pays to have a new model run through the standard suite).
- Private team leaderboards / regression tracking (folds into team-edition).
- Benchmark-as-a-service: BYO private review suite, scored by the standard methodology.

The asset sold is **a trusted measurement**, not infrastructure — and it falls out of the architecture already built.

## Engineering Effort

| Component | Effort | Notes |
|-----------|--------|-------|
| Scorecard emitter | 1 week | Persistence + projection over existing reconcile output |
| Local leaderboard CLI | 1 week | Aggregation + table/json rendering |
| Public submission + site | 3 weeks | Schema, anonymization, static site |
| **Total** | **2 weeks (local) / ~5 weeks (full public)** | Local half is independently valuable; public half is phase 2 |

## Moat / Differentiation

- **The reconciler is the moat.** Anyone can call N models; almost nobody has a deterministic, reproducible way to score whose findings were *right*. ATCR already is that.
- **Compounds with the roadmap** — survived-skeptic (3.0) and demonstrated-bug (5.0) turn a proxy into near-ground-truth, raising leaderboard credibility for free.
- **Standards gravity** — if "the ATCR review benchmark" becomes the cited number, ATCR is the reference, the way SWE-bench is for agents.
- **Differentiated from every other concept** — this sells a measurement that emerges from the design, not a feature bolted on.

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Corroboration is a biased proxy (popular-but-wrong findings agree) | High | High | Be explicit it is a proxy at Stage 1; lead with survived-skeptic (3.0) and demonstrated (5.0) as the headline metrics; never claim ground truth before earning it |
| Correlated reviewers inflate agreement | Medium | Medium | Track persona×model independence; down-weight near-duplicate reviewers in the metric |
| Public leaderboard gets gamed / cherry-picked submissions | Medium | Medium | Standard fixed diff suite for the public board; only reproducible runs count |
| Vendor model churn makes the board stale fast | High | Low | Automate the standard-suite run; date-stamp every row |

## Open Questions

- **Public suite contents?** A fixed, curated set of diffs with known defects (best, but labor) vs. crowd-submitted runs (cheap, noisier)?
- **Anonymization?** What must be stripped from a run summary before it can be published?
- **Where does the local scorecard live?** Alongside each review dir, or a rolled-up `~/.config/atcr/eval/`?
- **Headline metric?** Lead with cost-per-corroborated-finding (pragmatic) or survived-skeptic rate (rigorous)?

## References

- SWE-bench: became the cited agent benchmark by being reproducible; the methodology is the brand
- lmarena (Chatbot Arena): pairwise-comparison leaderboard, top-of-funnel for the whole space
- MTEB: embedding leaderboard — a measurement that anchored a category
- Related ATCR concepts: depends conceptually on `reconciler-library` data, sharpened by Epics 3.0/5.0
