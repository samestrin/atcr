# Vision

`atcr` exists so that a code change is reviewed by a panel of different models, not by one, and the panel's findings arrive as one deduplicated, confidence-scored report instead of N walls of prose. It serves the developers, CI pipelines and agent hosts that review code, and it treats every caller the same way: an agent platform such as Paperclip is a customer, not a special case. It owns two things: reviewing a change with a panel, and tracking the technical debt that review finds.

## The merge is deterministic

This rule sits above every other rule here. Prompts orchestrate; the binary decides. Clustering, deduplication, merging and confidence are Go code, and the same inputs give the same report. Every handoff between stages is a machine-readable file on disk, so any stage can be rerun or checked on its own. Confidence comes from agreement between independent reviewers, and a disagreement is kept and shown, never flattened into a false consensus.

## Many models, by design

The panel is heterogeneous on purpose: different models, different providers, different lenses. A single API key still yields two or more sources, because the host model joins as one more reviewer. A provider that fails or runs out of quota degrades coverage and says so; it does not fail the panel or silently shrink it.

## The user picks the depth

A plain panel review is the default. Each step past it is opt-in: skeptics that verify the findings, a debate with a judge for disputed ones, and a fixer that applies fixes. Each step is its own persona and its own stage, so a user can stop at any depth. A fix runs in a sandbox and always arrives as a pull request for a person to review, never as a direct push.

## Local first, your keys

atcr runs where the user runs it, with the user's own keys; there is no hosted service in this repo. A key is never written to atcr's config, and a project-defined provider receives a key only after the user trusts it. A fully local panel, with no code leaving the machine, is a supported setup, not an afterthought. `atcr doctor` proves every configured endpoint before a real review spends anything.

## One engine, three faces

One Go binary is a CLI, an MCP server over the same engine, and an Agent Skill. Review logic lands in the engine once, and each face exposes it; a face adds a way in, not its own version of the logic.

## Debt lives here, in one store

Technical debt found by review has one store and one schema, owned by atcr. Callers use plain `atcr debt` commands, and nothing specific to one caller enters atcr.

## The public core

atcr is the product, and everything a non-paying user gets lives here, including auto-fix, browser testing, the GitHub and Linear integrations (and GitLab when it is built), the audit log with its report, and the debt store with its locks and reports. `atcr-enterprise` is a closed, paid layer that imports this core and adds an audit-grade record for regulated teams, plus integrations only larger teams need, such as Jira and Bitbucket. When `atcr-enterprise` needs something from this repo's internals, it is exported here first as a small, general seam that a public user could use too, never named for a paid feature. A free capability never moves behind the license.

## Scope

atcr is not a single-model reviewer, a linter, or a hosted service.

A change aligns when it makes reconciled findings more accurate, more reproducible or easier to trust, adds a useful reviewer lens, provider or opt-in stage, lowers the cost of a full panel, or makes the debt store more reliable.

A change should be resisted when it lets a model decide what the reconciler should decide, hides a disagreement or a failed reviewer, makes a deeper stage run without being asked, pushes a fix without a pull request, writes or sends a key where the user did not choose, puts review logic in one face instead of the engine, adds caller-specific behavior, adds a seam named for a paid feature, or moves a free capability behind the `atcr-enterprise` license.
