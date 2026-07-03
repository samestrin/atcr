---
id: mem-2026-07-02-8b089e
question: "Scaffolded quickstart workflow: why keep `go install ...@latest` instead of pinning?"
created: 2026-07-02
last_retrieved: ""
sprints: []
files: [internal/quickstart/workflow.go, action.yml, .github/workflows/]
tags: [clarifications, epic-16.0_quick_start, scope, process, release-engineering, github-actions]
retrievals: 0
status: active
type: clarifications
---

# Scaffolded quickstart workflow: why keep `go install ...@lat

## Decision

Keep `go install ...@latest` (and unpinned `actions/checkout@v4` / `actions/setup-go@v5`) in the scaffolded consumer workflow, with the rationale documented in a code comment; do not force a v1 release or the composite action into scope to fix this.

- The composite action route is foreclosed independent of any release-tag blocker: action.yml makes `openrouter-api-key` a required input with no synthetic-key alternative, and using `samestrin/atcr@v1` in the scaffolded workflow was already explicitly rejected in this epic's own recorded decisions for exactly that reason. Modifying the published `action.yml` public contract is out of scope for feature epics.
- Cutting a first release is not a cheap workflow-template fix: the repo has no release automation (no goreleaser config, no tag-triggered release workflow) anywhere under .github/workflows/, and `git tag`/`gh release list` are both empty. Establishing a first release is a repo-wide lifecycle decision, not something a single epic's task list should absorb.
- The scaffolded workflow is intentionally a minimal, non-merge-gating review-only template (unlike the composite action's full pipeline), so an unpinned install-tool version carries materially lower risk than an unpinned merge gate.
- Pattern for future TD/epic decisions: don't let a workflow-template versioning nit force release-engineering scope into an unrelated epic — log it as its own follow-up item instead.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/quickstart/workflow.go
- action.yml
- .github/workflows/
