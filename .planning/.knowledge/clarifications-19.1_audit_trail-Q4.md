---
id: mem-2026-07-05-d11efd
question: "CWD-relative audit ledger writer vs repoRoot()-relative reader — intended fix scope"
created: 2026-07-05
last_retrieved: ""
sprints: []
files: [cmd/atcr/review.go, cmd/atcr/resume.go, cmd/atcr/audit_report.go, cmd/atcr/autofix.go, cmd/atcr/root.go]
tags: [clarifications, epic-19.1_audit_trail, architecture, scope, td-019, cwd-relative]
retrievals: 0
status: active
type: clarifications
---

# CWD-relative audit ledger writer vs repoRoot()-relative read

## Decision

Leave the writer/reader as-is (CWD-relative for review/resume's audit and history hooks, repoRoot()-relative for audit-report/history reads) and treat "run atcr from the repo root" as the documented operating assumption. Do not expand epic 19.1 (or any single-package fix) to make review/resume fully repo-root-relative — that is a pre-existing, separately tracked tech-debt item (TD-019), not new scope any one epic should absorb.

Justification:
- Verified as fact: cmd/atcr/review.go:262 calls gitrange.Resolve(ctx, ".", ...), cmd/atcr/review.go:275 calls fanout.LoadReviewConfig(".", cliOverrides(cmd)), and cmd/atcr/review.go:301-302 set Repo: ".", Root: "." on ReviewRequest — config load and range resolution are CWD-relative today. cmd/atcr/resume.go:85,96,104-106 repeat the identical pattern.
- This is an already-accepted limitation, not a new discovery: cmd/atcr/autofix.go:118,143 explicitly document "runReview passes repoRoot=\".\"... a subdirectory apply_target is not supported" citing TD-019; .planning/technical-debt/items/2026-07-03_17.0_auto_merged_fixes.yaml:45 records the same CWD==repo-root deferral for the auto-fix apply target.
- cmd/atcr/root.go's repoRoot() doc comment says "This helper can be adopted by other subcommands to make atcr cwd-independent" — a recognized future migration direction, not an expectation that any single epic solves it as a side effect.
- The convention `// repo root = CWD; validate finding file paths (Epic 5.0)` is baked into reconcile, review, and resume (review.go:432, resume.go:240) — a partial single-package fix would leave the majority of CWD-relative call sites untouched and inconsistent.
- Recommended remediation when this surfaces again: add a one-line note to README.md / CLI --help ("run atcr from the repository root") rather than a code change, and point to TD-019 for the broader CWD-independence migration.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/review.go
- cmd/atcr/resume.go
- cmd/atcr/audit_report.go
- cmd/atcr/autofix.go
- cmd/atcr/root.go
