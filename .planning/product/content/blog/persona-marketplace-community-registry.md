# A Marketplace for Code-Review Expertise, Without the Marketplace

**Status:** Draft
**Target:** Developer Relations, Open Source Maintainers, Platform Teams
**Publication Phase:** 2 — Education & Architecture
**Grounded in:** Epic 19.6 (canonical community registry + model-indexed persona library), 19.9 (`personas submit` via gh fork/PR + two-tier curation)

## The Hook

Every team's code review has a house style. One shop is paranoid about secrets and data egress. Another cares most about state-consistency invariants in a concurrent system. A third just wants someone to hunt duplication across a sprawling monorepo. A single generic "review my code" prompt serves none of them well.

ATCR treats that expertise as something you can package, share, and pull from a library — a persona — without standing up a marketplace, a website, or a hosted registry to do it.

## The Technical Challenge

A real persona ecosystem has two hard requirements that pull in opposite directions. It has to be **open** — anyone should be able to contribute a persona tuned for their niche — and it has to be **trustworthy** — you should not be running an unvetted stranger's reviewer prompt against your proprietary diff without a gate. Most "plugin marketplaces" solve openness and then bolt on trust as an afterthought (or never). And most compile their catalog into the binary, so adding a persona means shipping a new release.

## The ATCR Solution

ATCR's community persona channel is **canonical and fetched, not compiled in** (19.6). The library is pulled from `samestrin/atcr` (fetch-and-pinned, advanced with `atcr personas upgrade`), so new personas land without a binary release, and an `--offline` flag still scaffolds from embedded built-ins when there's no network.

- **Discover by model, not just by name:** personas carry structured provider/model metadata. `atcr personas search --model` / `--provider` filters the library and shows Provider/Model columns, so you can find the persona someone already tuned for the exact model you run.
- **Contribute with the tools you already have (19.9):** `atcr personas submit <name>` runs the local fixture gate, then forks `samestrin/atcr` and opens a pull request through *your own* `gh` CLI session. No account to create, no marketplace to onboard to — it's a GitHub PR.
- **Two-tier curation for trust:** a submission carries a `submitted` status — fixture-passing but unvetted — orthogonal to its provenance. It stays clearly separated from the vetted `personas/community/` library until a maintainer graduates it. Openness and trust, both, without one defeating the other.

The effect is an app-store-shaped experience — browse, search by model, install, contribute — running entirely on Git and fixtures, with nothing proprietary in the middle.

## Call to Action

Have a review lens your team relies on that isn't in the library yet? Author it, let the fixture gate prove it works, and `atcr personas submit` it as a pull request. The best code-review expertise is the kind that's already been battle-tested on real diffs — share yours.

## Next Steps (drafting notes)

- [ ] Walk through authoring a persona end to end, from `.yaml` to a merged PR.
- [ ] Screenshot `personas search --model` output.
- [ ] Explain the `submitted` → graduated curation lifecycle with a diagram.
