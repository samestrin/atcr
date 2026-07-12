---
id: mem-2026-07-11-7cda68
question: "Can external `go install github.com/samestrin/atcr/...@latest` be fixed locally, or does it require the repo to go public first?"
created: 2026-07-11
last_retrieved: ""
sprints: [20.0_standalone_skill_release]
files: [go.mod, .planning/epics/active/33.2_public_launch.md]
tags: [clarifications, sprint-20.0_standalone_skill_release, scope, release-engineering, epic-33.2_public_launch]
retrievals: 0
status: active
type: clarifications
---

# Can external `go install github.com/samestrin/atcr/...@lates

## Decision

No local fix is possible. Repo privacy is a binding constraint independent of the go.mod `replace` directive — a private GitHub repo's module is not on the public Go proxy (proxy.golang.org 404s it) regardless of go.mod contents. Dropping the replace directive is necessary but not sufficient. This is explicitly Epic 33.2's (Public Launch) job: make the repo public, publish the `reconcile` submodule, drop the replace directive, and deliver install.sh + a real-install integration test. Any sprint/TD item that hits this wall should defer to Epic 33.2 rather than attempting a workaround.

Justification:
- go.mod carries `replace github.com/samestrin/atcr/reconcile => ./reconcile`; Go refuses to `go install` a module whose published go.mod has a replace directive.
- Live-verified via `gh repo view --json isPrivate` -> true at time of writing.
- .planning/epics/active/33.2_public_launch.md exists and explicitly owns this exact fix chain.
- A prototype (tag reconcile/v0.1.0, drop replace, go.sum, local go.work) was already run end-to-end and proven to work "the instant the repo is public" — the only remaining gap is the repo-visibility flip, not an unsolved technical problem.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- go.mod
- .planning/epics/active/33.2_public_launch.md
