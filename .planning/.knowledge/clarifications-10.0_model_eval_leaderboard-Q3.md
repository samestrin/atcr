---
id: mem-2026-06-24-850129
question: "Where should the atcr tool version constant live and what value should it start at?"
created: 2026-06-24
last_retrieved: ""
sprints: []
files: [/Users/samestrin/Documents/GitHub/atcr/go.mod, /Users/samestrin/Documents/GitHub/atcr/internal/scorecard/export.go]
tags: [clarifications, epic-10.0_model_eval_leaderboard, versioning, infrastructure, ldflags]
retrievals: 0
status: active
type: clarifications
---

# Where should the atcr tool version constant live and what va

## Decision

Create a new `internal/version/version.go` package with `var Version = "0.0.0"` overridable via ldflags: `go build -ldflags "-X github.com/samestrin/atcr/internal/version.Version=1.2.3"`. The spec's "0.4.1" is not a real version — it is a placeholder in the example JSON. The repo has no git tags, go.mod has no semver directive, and no version file or ldflags pattern exists anywhere. "0.0.0" is the correct neutral placeholder; "0.1.0" is acceptable if a pre-release signal is preferred. Import the package in internal/scorecard/export.go to stamp into ExportEnvelope.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- /Users/samestrin/Documents/GitHub/atcr/go.mod
- /Users/samestrin/Documents/GitHub/atcr/internal/scorecard/export.go
