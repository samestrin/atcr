# You're Paying an LLM to Review Your Lockfiles

**Status:** Draft
**Target:** FinOps, DevOps, Engineering Managers watching an AI bill
**Publication Phase:** 2 — Education & Architecture
**Grounded in:** Epic 26.0 (`.gitignore` + repo-root `.atcrignore` review-payload exclusion; `--no-ignore`)

## The Hook

Open the diff of a routine dependency bump. Ten lines of real change — and forty thousand lines of regenerated `package-lock.json`, a vendored `go.sum`, maybe a checked-in build artifact. If your AI reviewer reads the whole diff, you just paid a per-token LLM bill to have a model "review" a lockfile it can't meaningfully review, and you crowded out its attention — and its context window — with noise.

The cheapest tokens are the ones you never send.

## The Technical Challenge

Naively, you could truncate large diffs — but truncation is dumb: it might cut your ten real lines and keep the lockfile. The right fix is to exclude the files that should *never* have been reviewed in the first place, and to do it **early** — before the byte-budget pass, before truncation, before a single token reaches a reviewer — so the savings are real and the pre-flight file counts don't lie to you about what's actually being reviewed. And it has to be *committed* config, so it applies identically on every workstation and CI runner, not a local flag someone forgets.

## The ATCR Solution

ATCR filters ignored files out of the review payload before the diff ever reaches the byte-budget pass or a reviewer (26.0):

- **Respects `.gitignore`:** files matched by the repository's `.gitignore` are excluded from the review payload automatically.
- **Adds a dedicated `.atcrignore`:** a repo-root, additive ignore file (one `.gitignore`-syntax pattern per line) for files that *are* committed to git but should never reach the AI reviewer — `go.sum`, `package-lock.json`, generated code, `docs/`. It's committed config, so every runner and teammate gets the same exclusions.
- **Filters early, honestly:** ignored files are removed from the changed-file list *ahead of* the byte-budget truncation pass, and the pre-flight changed-file counts reflect the same filtered set — so what you see is what's actually reviewed.
- **Escape hatch when you need it:** `atcr review --no-ignore` bypasses filtering to review normally-excluded files on demand, and skipped files are logged at debug level so nothing is invisibly dropped.

The reviewer spends its tokens — and its attention — on the ten lines that matter, not the forty thousand that don't.

## Call to Action

Add a three-line `.atcrignore` to your repo — `go.sum`, `package-lock.json`, `docs/` — and watch your per-review token count (and bill) drop, while the findings get *sharper* because the model isn't wading through generated noise. The best token-optimization is the file you never send.

## Next Steps (drafting notes)

- [ ] Add a real before/after token-count and cost comparison on a dependency-bump PR.
- [ ] Provide a starter `.atcrignore` for common ecosystems (Go, Node, Python).
- [ ] Clarify `.atcrignore` (never reach reviewer) vs. sprint-plan scoping (soft suppression).
