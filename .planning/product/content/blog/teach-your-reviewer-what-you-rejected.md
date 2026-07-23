# The Fastest Way to Lose Trust in an AI Reviewer: Repeat Yourself

**Status:** Draft
**Target:** Engineering Teams, Tech Leads
**Publication Phase:** 2 — Education & Architecture
**Grounded in:** Epic 24.0 (`debt resolve --status wontfix` + `--reason`, dedup-by-id suppression)

## The Hook

There is one behavior that will make a team abandon an AI reviewer faster than false positives, slowness, or cost: **re-surfacing a finding they already looked at and consciously rejected.** You triage it, you decide it's a won't-fix, you move on — and next review loop it's back, top of the list, as if the conversation never happened. Do that twice and your developers learn to ignore the tool entirely.

An AI reviewer that can't remember your decisions isn't a reviewer. It's a stuck record.

## The Technical Challenge

"Just suppress it" is harder than it sounds when findings are regenerated fresh every run. The reviewer re-reads the diff, re-detects the same issue, and — unless something durably remembers that *this specific finding* was dismissed — re-persists it into the open backlog. You need a terminal decision that (a) survives across review loops, (b) is distinguishable from "fixed" (a won't-fix is a judgment call, not a resolution), and (c) carries *why*, so the next person doesn't re-litigate it.

## The ATCR Solution

ATCR gives a dismissal the same permanence as a fix (24.0):

- **`atcr debt resolve --resolve <id> --status wontfix`** marks a finding as won't-fix / false-positive. `wontfix` is a *terminal* status: it folds the item out of the open backlog (`--list` / `--json`) exactly like `resolved`, so it stops cluttering triage.
- **The suppression is enforced where re-detection happens:** the existing `atcr reconcile` dedup-by-id path suppresses re-persisting a dismissed finding when the same finding is detected again. The decision doesn't just hide the row — it stops the finding from coming back.
- **`--reason "<text>"`** records *why* it was dismissed, populating the record's `Justification` field, so the rationale travels with the decision. (Pass an empty reason and any existing justification is preserved, not blanked.)
- **No silent no-ops:** supplying `--status` / `--reason` without `--resolve <id>` is now a usage error instead of being quietly ignored — so you never *think* you dismissed something that's still open.

Dismiss a finding once, with a reason, and ATCR remembers. Your backlog reflects the decisions your team actually made.

## Call to Action

Audit your AI reviewer with one question: when you reject a finding, does it stay rejected? If it doesn't, your team is already tuning it out. With ATCR, `debt resolve --status wontfix --reason` turns "this finding was reviewed and dismissed on purpose" into a decision the tool honors — permanently.

## Next Steps (drafting notes)

- [ ] Show a before/after backlog with a wontfix item folded out.
- [ ] Contrast `resolved` vs `wontfix` semantics in a small table.
- [ ] Note how `--reason` justifications read in an audit report.
