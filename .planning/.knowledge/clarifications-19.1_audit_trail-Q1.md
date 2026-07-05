---
id: mem-2026-07-05-2ada61
question: "PR number source for atcr review audit records"
created: 2026-07-05
last_retrieved: ""
sprints: []
files: [cmd/atcr/github.go, cmd/atcr/autofix.go, internal/ghaction/client.go, internal/fanout/review.go]
tags: [clarifications, epic-19.1_audit_trail, architecture, cli-conventions]
retrievals: 0
status: active
type: clarifications
---

# PR number source for atcr review audit records

## Decision

Add a `--pr <n>` flag to `atcr review`, threaded onto `ReviewRequest`, falling back to parsing `GITHUB_REF` (format `refs/pull/<n>/merge`) when unset. This mirrors atcr's established `envOr(flagValue, "ENV_VAR")` convention already used for --repo/GITHUB_REPOSITORY, --token/GITHUB_TOKEN, --sha/GITHUB_SHA, --api-url/GITHUB_API_URL (cmd/atcr/github.go:55-71, cmd/atcr/autofix.go:52-54,168-180) — flag wins, env is the fallback, no API call needed. Note: internal/ghaction's PR lookup (FindOpenPullRequest, client.go:406-424) is a live GitHub API call keyed on branch, not an env-var read — do not assume it auto-detects from CI env vars.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/github.go
- cmd/atcr/autofix.go
- internal/ghaction/client.go
- internal/fanout/review.go
