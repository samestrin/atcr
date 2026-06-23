---
id: mem-2026-06-22-2178cc
question: "How should readReconciledFindings distinguish absent findings (usage error, exit 2) from present-but-malformed findings (operational error, exit 1)?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [cmd/atcr/report.go, cmd/atcr/github.go]
tags: [td-clarification, td-only, exit-codes, readReconciledFindings, os.ErrNotExist, error-wrapping, github-action]
retrievals: 0
status: active
type: clarifications skill 2026-06-22
---

# How should readReconciledFindings distinguish absent finding

## Decision

Two-step fix: (1) In readReconciledFindings (report.go:181), wrap os.ErrNotExist via %w: change `fmt.Errorf("no reconciled data found: ...")` to `fmt.Errorf("no reconciled data found: run 'atcr reconcile' first: %w", err)` so callers can detect the absent-file case. (2) In github.go:105-108 (and symmetrically in report.go:66), branch on the error: `if errors.Is(err, os.ErrNotExist) { return usageError(err) }; return &codedError{code: exitFailure, err: err}` — absent data is a usage error (exit 2 = run reconcile first), present-but-malformed is operational (exit 1 = transient IO/parse failure). Currently readReconciledFindings already handles ErrNotExist internally but does NOT wrap it via %w, so github.go and report.go callers cannot distinguish the two cases and call usageError() for both.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/report.go
- cmd/atcr/github.go
