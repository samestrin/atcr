# When the Auditor Asks "Who Reviewed This PR?", Can You Answer for the AI?

**Status:** Draft
**Target:** Compliance Leads, CISOs, Engineering Directors in regulated environments
**Publication Phase:** 3 — Enterprise Trust & Security
**Grounded in:** Epic 19.1 (tamper-evident audit ledger + `audit-report`), 19.4 (time-sharded, version-controllable findings history)

## The Hook

An auditor points at a merged pull request from eight months ago and asks a simple question: what reviewed this, on what commits, and what did it find? For human reviewers you have the PR approval trail. For your AI reviewer — the one that's increasingly gating merges — most teams have nothing. The review happened in a terminal, the output scrolled by, and the evidence is gone.

"An AI reviewed it" is not an answer an audit accepts. "Here is the append-only record of every run" is.

## The Technical Challenge

Compliance evidence has requirements ordinary logs don't. It has to be **append-only** and durable, written **regardless of run flags** (an engineer shouldn't be able to make the evidence disappear by changing an output directory). It has to be **attributable** to specific commits and pull requests. And trend data — are findings going up or down over releases? — has to be **shareable across a team** without a single ever-growing file turning into a merge-conflict magnet or bloating the repository on every run.

## The ATCR Solution

ATCR writes the compliance trail as a side effect of doing the review — there's nothing extra to remember to run.

- **Tamper-evident audit ledger (19.1):** every `atcr review` run appends one record — run timestamp, resolved base/head SHA, PR number, and a findings-by-severity summary — to an append-only `.atcr/audit.log.jsonl`. It's a repo-level accumulator written *regardless of* `--output-dir`, and resumed reviews (`--resume`) record too, so the ledger can't be sidestepped by a flag.
- **Per-PR compliance report:** `atcr audit-report --pr <n>` renders a one-page markdown report of a pull request's recorded review runs — SHAs, timestamps, findings summary. A PR with no recorded runs exits non-zero with a clear message, so "no evidence" is itself a loud, detectable state.
- **Shareable trend history (19.4):** findings history is sharded by month into `.planning/history/YYYY-MM.jsonl`, derived from run time in UTC. Once a month rolls over its shard stops receiving writes — so old shards stop churning new git blobs — and the directory is version-controlled, so a whole team can commit and share trend data. `atcr history` queries transparently across every shard before applying `--since` / `--package` filters.

The result: when the question comes, the answer is a command, not a shrug.

## Call to Action

Before your next audit, run `atcr audit-report --pr <n>` against a recent PR and see what a real AI-review evidence trail looks like. If your current tool can't produce one, you're carrying a compliance gap you haven't been asked about *yet*. Close it before the auditor opens it.

## Next Steps (drafting notes)

- [ ] Include a sample `audit-report` markdown output.
- [ ] Clarify the honest scope of "tamper-evident" (append-only ledger, CLI-hook scope).
- [ ] Show a findings-history trend chart built from the monthly shards.
