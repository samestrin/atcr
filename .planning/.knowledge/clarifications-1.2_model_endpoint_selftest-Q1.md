---
id: mem-2026-06-11-ebdcc3
question: "What is the correct subcommand name for the model endpoint self-test feature — `atcr doctor`, `atcr check`, or `atcr selftest`?"
created: 2026-06-11
last_retrieved: ""
sprints: []
files: [cmd/atcr/main.go, cmd/atcr/report.go]
tags: [clarifications, epic-1.2_model_endpoint_selftest, architecture, cli-design]
retrievals: 0
status: active
type: clarifications
---

# What is the correct subcommand name for the model endpoint s

## Decision

Use `atcr doctor`. It carries universal recognition from `brew doctor` and `flutter doctor`, sets the right user expectation (environment health check, not merely a self-test), and does not collide with any name in the existing command tree. `atcr check` risks a false-friend confusion with the existing `--format checklist` output mode in `atcr report`; `atcr selftest` is verbose with no precedent advantage. Existing subcommands: review, reconcile, report, range, status, init, serve — none of which is `doctor`. See cmd/atcr/main.go:99-107 (subcommand roster) and cmd/atcr/report.go:14,19,23 (`--format checklist` false-friend risk).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/main.go
- cmd/atcr/report.go
