---
id: mem-2026-06-22-9b65af
question: "Should inline comments in the Epic 7.3 GitHub Action default to ON or OFF?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [.planning/epics/active/7.3_github_action_pr_integration.md]
tags: [clarifications, epic-7.3_github_action_pr_integration, UX, github-action, inline-comments]
retrievals: 0
status: active
type: clarifications
---

# Should inline comments in the Epic 7.3 GitHub Action default

## Decision

Default OFF (opt-in). Use an `inline-comments` input defaulting to `false`; set to `true` to enable. The Goal section's "optionally inline comments" is the decisive signal — "optionally" positions inline comments as an enhancement, not the baseline. Least-surprise for new adopters: they get the PR check and artifact on first use without unexpected comment noise. The AC4 wording "a toggle disables inline comments" is slightly ambiguous but does not override the Goal section language.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .planning/epics/active/7.3_github_action_pr_integration.md
