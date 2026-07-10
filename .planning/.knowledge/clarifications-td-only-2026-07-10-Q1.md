---
id: mem-2026-07-10-571a25
question: "What is the intended gh pr view caching scope and fallback when a PR becomes inaccessible mid-workflow (hermes-auto-merge.yml)?"
created: 2026-07-10
last_retrieved: ""
sprints: []
files: [.github/workflows/hermes-auto-merge.yml]
tags: [clarifications, td-clarification, td-only, testing, github-actions, error-handling]
retrievals: 0
status: active
type: clarifications
---

# What is the intended gh pr view caching scope and fallback w

## Decision

In GitHub Actions workflows using multiple `gh pr view` calls under `set -euo pipefail`: cache identity data (PR number, head SHA) once via step outputs and reuse everywhere — but any *subsequent* `gh pr view` call that re-fetches mutable PR state (files, labels) still needs its own explicit error guard, because the PR can be closed/deleted between calls. The correct fallback mirrors a no-op convention already used elsewhere in the same script: catch a non-zero exit, log a clear "no longer accessible" message, set the step's match/proceed output to false, and `exit 0` — never let `set -e` propagate the failure into a hard job abort. Ref: .github/workflows/hermes-auto-merge.yml:59-81 (correct caching pattern for PR number/SHA via step outputs) vs. lines 108-109 (ungated second/third gh pr view calls that lack this guard).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .github/workflows/hermes-auto-merge.yml
