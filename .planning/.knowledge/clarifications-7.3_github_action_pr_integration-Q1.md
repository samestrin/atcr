---
id: mem-2026-06-22-ac636a
question: "Where does the PR-rendering logic live in Epic 7.3 GitHub Action?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [cmd/atcr/main.go, internal/reconcile, cmd/atcr/reconcile.go]
tags: [clarifications, epic-7.3_github_action_pr_integration, architecture, github-action]
retrievals: 0
status: active
type: clarifications
---

# Where does the PR-rendering logic live in Epic 7.3 GitHub Ac

## Decision

Option (a): a new Go subcommand (e.g. `atcr github` or `atcr pr-comment`) wrapped by a thin composite `action.yml`. Every existing concern has a `cmd/atcr/<name>.go` + `<name>_test.go` pair; this keeps all logic inside `go test ./...` coverage and reuses `internal/reconcile` for findings.txt parsing (already imported by `cmd/atcr/reconcile.go`). The thin action.yml shell installs the binary and invokes the subcommand — no logic in bash.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/main.go
- internal/reconcile
- cmd/atcr/reconcile.go
