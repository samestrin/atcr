# Concept: Disagreement Radar

**Status:** Conceptual
**Created:** 2026-06-11
**Priority:** High

## Problem

Every other multi-reviewer tool **flattens** model output — it merges or concatenates, and the disagreements are gone. ATCR is the only one that *structurally preserves* disagreement: the reconciler already writes gray-zone clusters to `ambiguous.json` and keeps severity conflicts inline instead of averaging them away.

But that preserved signal is currently treated as **residue** — the stuff the deterministic merge couldn't resolve, punted to a human or (later) to cross-examination. That undersells it. Where two strong, independent models *disagree* on a diff is exactly where the ambiguity, the subtle risk, and the genuinely interesting design tension live. Nobody else can surface this, because nobody else kept it.

## Solution

Promote disagreement from residue to a **first-class output**: a "radar" that points reviewers at the highest-tension spots in a change.

### What it surfaces

- **Severity splits** — model A says CRITICAL, model B says LOW on the same location. The spread itself is the signal.
- **Caught-by-one** — a finding only one strong model raised (not noise to discard, but a spot the others may have missed).
- **Gray-zone clusters** — the existing `ambiguous.json` contents, presented as insight rather than overflow.
- **Persona tension** — the correctness reviewer and the design reviewer pulling in opposite directions on the same hunk.

### Surfaces

- `atcr report --disagreements` — a focused view: ranked list of the N highest-tension spots in the diff, with each model's position side by side.
- A "Review Radar" section in the standard report.md — top disagreements above the consensus findings, because the disagreements are where human attention is worth the most.
- Feeds Epic 4.0 (cross-examination): the radar is the *queue* of what cross-examination resolves; until 4.0 lands, it is the human's queue.

This is almost entirely a **presentation/projection** over data the reconciler already produces. Low engineering cost, high conceptual payoff, pure OSS.

## Key Features

| Feature | Description | Effort |
|---------|-------------|--------|
| Disagreement scoring | Rank clusters by severity spread + reviewer split | 3 days |
| `--disagreements` report mode | Focused side-by-side view of top-tension spots | 3 days |
| Radar section in report.md | Disagreements surfaced above consensus findings | 2 days |
| Cross-exam handoff format | Stable schema so 4.0 consumes the radar as its queue | 2 days |

## Revenue Model

**None direct — this is a feature, not a product.** Its value is:
- **Talkability** — "the tool that shows you where the AI reviewers *disagree*" is a memorable, demoable hook that gets the repo shared.
- **Differentiation reinforcement** — it makes the disagreement-preservation thesis visible instead of buried in a sidecar file.
- **Sets up Epic 4.0** — cross-examination has an obvious input and a visible before/after story.

## Engineering Effort

| Component | Effort | Notes |
|-----------|--------|-------|
| Disagreement scoring | 3 days | Rank existing clusters; no new analysis |
| Report modes | 1 week | `--disagreements` + report.md section |
| Handoff schema | 2 days | Stable contract for 4.0 |
| **Total** | **~1.5 weeks** | Mostly a view over existing `ambiguous.json` + inline conflicts |

## Moat / Differentiation

- **Only possible because ATCR kept the disagreement.** Competitors that flattened cannot retrofit this without rebuilding their merge.
- **Most ATCR-native idea on the board** — it directly dramatizes the core thesis ("preserve disagreements instead of flattening them").
- **Near-zero new surface area** — the data exists; this is how it's shown.

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Disagreement is mostly noise, not signal | Medium | Medium | Rank by reviewer *strength* and independence, not raw count; only surface splits among trusted reviewers |
| Overlaps/duplicates Epic 4.0 | Medium | Low | Radar *surfaces*; 4.0 *resolves*. Radar remains useful as 4.0's input and for the spots 4.0 doesn't auto-close |
| Adds report noise for simple diffs | Low | Low | Section is empty/collapsed when there are no meaningful disagreements |

## Open Questions

- **Ranking signal?** Severity spread, reviewer-strength-weighted split, or both combined?
- **How many to surface?** Top N, or everything above a tension threshold?
- **Default-on?** Always show the radar section, or opt-in via `--disagreements`?

## References

- ATCR's own thesis: "preserve disagreements instead of flattening them" (README)
- Epic 4.0 Cross-Examination — the resolution stage this concept feeds
- Inter-rater reliability (Cohen's kappa): the academic framing of "where do raters disagree, and does it matter"
