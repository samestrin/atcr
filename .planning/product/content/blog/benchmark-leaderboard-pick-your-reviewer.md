# Stop Picking Your AI Reviewer by Vibes: Benchmark It

**Status:** Draft
**Target:** Tech Leads, Platform Engineers, AI/ML-curious Architects
**Publication Phase:** 2 — Education & Architecture
**Grounded in:** Epic 10.0 (benchmark + public leaderboard schema), 10.2 (`benchmark run` + scoring), 10.3 (checkpoint/resume)

## The Hook

You would never pick a database by reading a vendor's landing page and calling it a day. You'd load-test it. Yet most teams choose the model behind their AI code review the same way they pick a lunch spot — by vibes, by whichever model was trending on X this week, or by whatever their IDE happens to ship.

That's a strange way to make a decision that silently gates every pull request you merge.

## The Technical Challenge

"Which model is best at code review" is not a leaderboard you can borrow from someone else. General reasoning benchmarks don't measure it: a model that tops a math eval can still hallucinate APIs, miss a race condition, or drown you in low-confidence noise on a real diff. The only honest answer is measured against *your* kind of code, with *your* reviewer prompts, and scored on the thing that actually matters — did it catch the planted defect, and did that finding survive scrutiny?

The hard part is making that measurement reproducible. A benchmark you can't re-run identically is an anecdote. A benchmark that costs a full LLM run every time you tweak one case is a benchmark nobody runs twice.

## The ATCR Solution

ATCR ships benchmarking as a first-class command, not a spreadsheet you maintain by hand.

- **`atcr benchmark run --suite-path <dir>`** executes a suite of diffs — each with a planted defect and its `expected_categories` — straight through the real review pipeline, then scores every reviewer's findings against what it was supposed to catch.
- The scorecard reports metrics that map to real trust, not raw volume: **`corroboration_rate`** (do other models agree with this reviewer?), **`survived_skeptic_rate`** (did the finding survive adversarial verification?), **`cost_per_corroborated_finding_usd`** (what did each *useful* finding actually cost?), and **`latency_p50_ms`**.
- **`atcr benchmark verify`** prints a content-based reproducibility hash for the suite, so a run is provably the same suite every time.
- **`atcr benchmark run --checkpoint <path>`** durably records each case's scored outcome before the next case starts. A transient failure halfway through a suite no longer forfeits the completed, already-paid-for work — re-running resumes from the first unscored case with *zero* additional LLM cost, and produces a byte-identical result to an uninterrupted run.

The result: you can rank a paid frontier model against a free local one on your own defect suite, see the real cost-per-useful-finding, and make the call with data instead of a hunch. And because the public leaderboard schema deliberately drops token counts, roles, and internal corroborated/solo counts, you can publish or compare results without leaking how your reviewers actually work.

## Call to Action

Before you standardize your whole org on a review model, spend an afternoon proving it. Build a small suite of diffs with defects you actually care about, run `atcr benchmark run`, and let the `cost_per_corroborated_finding_usd` column end the argument. Pick your reviewer the way you'd pick your database — measured.

## Next Steps (drafting notes)

- [ ] Add a real example suite manifest and a sample scorecard table.
- [ ] Include a head-to-head chart: one frontier model vs. a local model on cost-per-corroborated-finding.
- [ ] Link to `docs/benchmark.md` and the external benchmark-suite repo.
