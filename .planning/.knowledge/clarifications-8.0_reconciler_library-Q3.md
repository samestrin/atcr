---
id: mem-2026-06-23-6d8756
question: "Branch-protection required status checks deferred as external action — should source-tree files (CI YAML, dod-completion-summary) be modified when marking [/]?"
created: 2026-06-23
last_retrieved: ""
sprints: [8.0_reconciler_library]
files: [.planning/sprints/active/8.0_reconciler_library/dod-completion-summary.md, .github/workflows/ci.yml, .github/workflows/reconcile-module.yml]
tags: [clarifications, sprint-8.0_reconciler_library, process, CI-CD, branch-protection, resolve-td, dod-completion-summary]
retrievals: 0
status: active
type: clarifications
---

# Branch-protection required status checks deferred as externa

## Decision

Mark the TD row [/] (deferred). No source-tree change is needed beyond the TD table marking when dod-completion-summary.md already contains the complete manual step. In this sprint, dod-completion-summary.md:17-30 names both incomplete DoD items (AC 01-06, AC 06-02), explains they are GitHub UI repo-admin actions (not source changes), confirms CI deliverables are complete and verified, and gives the exact branch-protection step (add "Go Lint & Test / Go Lint & Test", "Go Lint & Test / Reconcile Module Test", and "Reconcile Module CI" as required status checks on the main protection rule). Do NOT add pending-action TODO comments to the CI YAML files — they create a second tracking location that must be updated when the action is done. dod-completion-summary.md is the designated tracking artifact; the TD row cites it. The two DoD items (AC 01-06 and AC 06-02) are the same single GitHub UI action; one [/] marking closes both.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .planning/sprints/active/8.0_reconciler_library/dod-completion-summary.md
- .github/workflows/ci.yml
- .github/workflows/reconcile-module.yml
