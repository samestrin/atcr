---
id: mem-2026-06-22-3c2da6
question: "Where should action.yml be placed for the Epic 7.3 GitHub Action publish path?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [action.yml]
tags: [clarifications, epic-7.3_github_action_pr_integration, architecture, github-action, publish-path]
retrievals: 0
status: active
type: clarifications
---

# Where should action.yml be placed for the Epic 7.3 GitHub Ac

## Decision

Top-level `action.yml` at repo root — consumers use `uses: samestrin/atcr@<ref>`. No `action.yml` exists anywhere in the repo yet. Root placement is the GitHub convention for single-action repos and gives the cleanest consumer DX. Go module files and `action.yml` coexist without conflict at the root. A subdirectory path (`uses: samestrin/atcr/action@<ref>`) is only needed for multi-action repos or monorepos — neither applies here.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- action.yml
