---
id: mem-2026-07-10-03b1fa
question: "Does `gh pr view --json files` expose rename status (status/previous_filename), or only `gh api repos/{owner}/{repo}/pulls/{num}/files`?"
created: 2026-07-10
last_retrieved: ""
sprints: [19.8_hermes_maintenance_agents]
files: [.github/workflows/hermes-auto-merge.yml]
tags: [clarifications, sprint-19.8_hermes_maintenance_agents, testing, github-cli, github-actions]
retrievals: 0
status: active
type: clarifications
---

# Does `gh pr view --json files` expose rename status (status/

## Decision

`gh pr view --json files` only exposes `path`, `additions`, `deletions` per file — it never carries `status` or `previous_filename`, so a renamed file (e.g. a .md prompt renamed to .yaml to slip past an allow-list filter) is indistinguishable from a plain add/modify through this command. Detecting a rename requires the REST endpoint `gh api repos/{owner}/{repo}/pulls/{num}/files`, whose response objects include `filename`, `status` (`added`/`modified`/`removed`/`renamed`), and `previous_filename`. Verified empirically against this repo (gh 2.87.3, PR #154): `gh pr view 154 --json files --jq '.files[0]'` → `{additions,deletions,path}` only; `gh api repos/.../pulls/154/files --jq '.[0]'` → includes `filename`, `status`, `previous_filename`.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .github/workflows/hermes-auto-merge.yml
