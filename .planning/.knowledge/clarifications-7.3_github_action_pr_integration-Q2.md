---
id: mem-2026-06-22-cdbe88
question: "How does the Epic 7.3 GitHub Action obtain the atcr binary?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [.github/workflows/ci.yml, go.mod, cmd/atcr]
tags: [clarifications, epic-7.3_github_action_pr_integration, CI-CD, github-action, build]
retrievals: 0
status: active
type: clarifications
---

# How does the Epic 7.3 GitHub Action obtain the atcr binary?

## Decision

Option (a): `actions/setup-go` + `go build ./cmd/atcr`. No goreleaser config exists and no GitHub releases exist. The existing CI workflow at `.github/workflows/ci.yml` already uses `actions/setup-go@v5` with `go-version: '1.25'` and `cache: true` — the action replicates this step verbatim. Release artifacts (option c) are out of this epic's scope.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .github/workflows/ci.yml
- go.mod
- cmd/atcr
