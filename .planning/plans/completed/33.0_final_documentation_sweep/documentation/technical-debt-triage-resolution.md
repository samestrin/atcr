# Technical Debt Triage & Resolution Cycle
**Priority: [CRITICAL]**

## Overview

This plan (33.0, tech-debt type) runs a comprehensive code review over the production codebase, fixes CRITICAL/HIGH findings directly, and routes MEDIUM/LOW findings into `.planning/technical-debt/README.md`. That file exists as the sharded destination for exactly this kind of triage:

> Source: codebase-discovery.json > existing_patterns ("Sharded technical debt README format") — ".planning/technical-debt/README.md holds all tracked technical debt sharded by severity, group, and status ([ ] open, [/] deferred, [x] resolved)." follow_for: "Destination for MEDIUM/LOW findings triaged during the codebase review pass per AC1."

Once findings are captured in that store, resolution is not ad hoc — it follows a defined, repeatable cycle. The `atcr-debt-resolve` skill (`skill/debt-resolve/SKILL.md`) describes the on-demand resolution route for the local, `.atcr/`-scoped technical-debt store that `atcr reconcile` accumulates across review runs. It resolves items autonomously through a per-item **RED → GREEN → ADVERSARIAL → REFACTOR** cycle, followed by one cumulative adversarial pass over the whole run:

> Source: skill/debt-resolve/SKILL.md — "It reads the local, `.atcr/`-scoped technical-debt store that `atcr reconcile` accumulates across review runs and autonomously fixes items through a per-item RED → GREEN → ADVERSARIAL → REFACTOR cycle, followed by one cumulative adversarial pass over the whole run."

This grounds both Task 3 ("Findings triage") — where MEDIUM/LOW findings from the review pass are written into the technical-debt README as the recognized destination — and Task 7 ("Technical debt capture") — where the resolution discipline (RED/GREEN/ADVERSARIAL/REFACTOR, branch safety, non-overridable adversarial gate) defines how captured items get worked off later.

## Key Concepts

**Store access is CLI-only.** Items are previewed, filtered, and resolved through `atcr debt resolve` subcommands rather than direct file edits.
> Source: skill/debt-resolve/SKILL.md > Store Access (CLI-only)

**Deterministic item selection.** Scope is open items only (an item stays open until a resolution record folds it out); sort is severity descending (`CRITICAL` > `HIGH` > `MEDIUM` > `LOW`), then `ts` ascending (oldest first) within a severity; the cap is the first `N=10` matching items per invocation unless overridden.
> Source: skill/debt-resolve/SKILL.md > Item Selection

**Destination file for triaged findings.** MEDIUM/LOW review findings are recorded into `.planning/technical-debt/README.md`, the designated integration point for this kind of output.
> Source: codebase-discovery.json > integration_points (location ".planning/technical-debt/README.md", type "debt-triage", description "Target file for recording MEDIUM/LOW review findings (AC1)")

**Files touched by triage.** The README is the modification target for this plan's Task 3 work, scoped as a major change to the file.
> Source: codebase-discovery.json > files_to_modify (path ".planning/technical-debt/README.md", reason "Record MEDIUM/LOW findings triaged from the code review pass", scope "major")

**Per-item resolution cycle.** Each selected item runs through four ordered stages — a pre-fix evaluation gate, then RED, GREEN, ADVERSARIAL, and REFACTOR — before being recorded as resolved.
> Source: skill/debt-resolve/SKILL.md > Resolution Cycle (per item)

**Non-overridable adversarial gate.** The ADVERSARIAL stage flags test-only changes, weakened/deleted assertions, lint/type suppressions, or stubbed/empty bodies as `NEEDS_REVIEW`; an item so flagged is never marked resolved, regardless of other checks passing.
> Source: skill/debt-resolve/SKILL.md > Resolution Cycle (per item) — "This verdict is non-overridable: an item flagged NEEDS_REVIEW is never marked resolved."

**Cumulative adversarial pass.** After the per-item loop completes, the entire set of changes is reviewed together to catch cross-item integration issues (conflicting fixes, regressions introduced into another item's area, patterns of over-simplification). Any CRITICAL/HIGH issue found here is fixed before the run is done; MEDIUM/LOW issues are reported to the user.
> Source: skill/debt-resolve/SKILL.md > Cumulative Adversarial Pass

**Branch safety.** Autonomous fixes never land unreviewed on a default branch: if the current branch is the repository's default (e.g. `main`/`master`), a dedicated `debt-resolve/<date>` branch is created first (fixed template, never interpolating finding text); if already on a non-default feature/working branch, fixes land in place on that branch.
> Source: skill/debt-resolve/SKILL.md > Branch Safety

## Code Examples

```
atcr debt resolve --list
atcr debt resolve --json
atcr debt resolve --severity <CRITICAL|HIGH|MEDIUM|LOW>
atcr debt resolve --max <N>
atcr debt resolve --resolve <id>
atcr debt resolve --resolve <id> --status wontfix --reason "<why this is a false positive/accepted pattern>"
```
> Source: skill/debt-resolve/SKILL.md > Store Access (CLI-only)

## Quick Reference

| Stage | What it does |
|-------|---------------|
| 0. Pre-fix evaluation | Confirms the finding still applies before touching code — checks still-exists, clear-fix, and safe-scope; if the finding no longer reproduces, marks it resolved (stale) and skips the cycle. |
| 1. RED | Reproduces or confirms the problem — writes or identifies a failing test (or concrete reproduction) demonstrating the defect; does not proceed until the failure is real and observed. |
| 2. GREEN | Applies the minimal fix that makes RED pass — nothing speculative, touches only what the finding requires. |
| 3. ADVERSARIAL | Runs an over-simplification / reward-hack gate over the diff (equivalent to `/resolve-td`'s non-overridable `llm_support_diff_smell` hard verdict), flagging test-only changes, weakened/deleted assertions, lint/type suppressions, or stubbed/empty bodies as `NEEDS_REVIEW`; this verdict is non-overridable. |
| 4. REFACTOR | With the fix verified and the adversarial gate clear, cleans up names, removes dead scaffolding, and tidies the test; re-runs tests to confirm still green. |

> Source: skill/debt-resolve/SKILL.md > Resolution Cycle (per item)

## Related Documentation

- [skill/debt-resolve/SKILL.md](../../../../../skill/debt-resolve/SKILL.md) — the `atcr debt resolve` skill definition
- [.planning/technical-debt/README.md](../../../../technical-debt/README.md) — the sharded technical-debt store this plan's Task 3 writes into
- [docs/technical-debt-format.md](../../../../../docs/technical-debt-format.md) — the format specification for technical-debt README entries
