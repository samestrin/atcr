# From Bug Found to PR Opened, With No Hands on the Keyboard — Safely

**Status:** Draft
**Target:** Engineering Managers, DevOps, Staff Engineers
**Publication Phase:** 2 — Education & Architecture
**Grounded in:** Epic 17.0 (`--auto-fix`: apply → validate → revert-on-failure → branch/commit/PR)

## The Hook

Most AI code tools stop at the interesting-but-useless step: they tell you what's wrong and hand you a diff. Now *you* apply it, *you* run the tests, *you* make the branch, *you* open the PR. The AI did the fun 20% and left you the tedious 80% — and the tedium is exactly where fixes go to die in a backlog.

The reason tools stop there is fear, and the fear is legitimate: letting an agent write to your repo and open PRs unattended is how you get a bot that confidently commits broken code. ATCR closes the loop anyway — because it refuses to open a PR for a fix it hasn't already proven passes.

## The Technical Challenge

"Auto-fix" is easy to demo and terrifying to ship. A model-generated diff might not apply cleanly. It might apply and then break the build. It might try to clobber a file it shouldn't, or escape the repo through a symlink. And the moment you give an agent GitHub write credentials, any bug in the ordering — mutate GitHub *before* you've validated locally — becomes a public mistake with your name on the commit.

Safety here is entirely about *ordering and reversibility*: validate before you mutate anything remote, and be able to undo everything local if validation fails.

## The ATCR Solution

`atcr review --auto-fix` is opt-in and off by default, and its whole design is the safe ordering (17.0):

1. **Parse and apply locally** — the model's diff is applied to the working tree over `atomicfs`, with per-file backups, a symlink-escape guard at the write boundary, and a refusal to clobber an existing target on a create-diff.
2. **Validate** — a configurable local validation command runs against the patched tree (conservative exit-code-only pass/fail gate, with timeout handling).
3. **Revert on failure** — if validation fails, every touched file is fully restored from backup. The bad fix never leaves your machine.
4. **Only then, touch GitHub** — a branch, commit, and pull request are created through the GitHub Git Data API, with an open-PR existence check so it won't open a duplicate. No GitHub-mutating call is even *reachable* before local validation passes, and the flow is guarded by an all-or-nothing refuse-without-backend gate. The commit message is redacted before it's sent.

So you get the whole loop — detection to a ready-to-review PR — but the only fixes that ever reach GitHub are the ones that already applied cleanly and passed your validation command.

## Call to Action

Stop copy-pasting AI diffs and babysitting them through your test suite. Turn on `--auto-fix`, point it at your validation command, and let ATCR deliver fixes as PRs you can review — where the ones you see are, by construction, the ones that already passed. Then decide what to merge.

## Next Steps (drafting notes)

- [ ] Show the config stanza (`auto_fix`) from the `atcr init` template.
- [ ] Add a sequence diagram of apply → validate → revert / PR.
- [ ] Clarify the trust boundary: what runs locally vs. what hits the GitHub API.
