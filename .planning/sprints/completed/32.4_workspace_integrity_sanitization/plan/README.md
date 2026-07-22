## Overview
Plan 32.4 hardens ATCR against Indirect Sandbox Escape / Host Trust Transposition attacks: a strict path-protection guard blocks `--auto-fix` from writing to host-execution config paths (`.git/`, `.githooks/`, `.github/workflows/`, `.vscode/`, `.idea/`, `.env*`), a new shared `internal/gitexec` wrapper hardens every host git subprocess ATCR spawns, and a non-blocking risk flag surfaces executable-bit/build-script changes in the generated PR body. Routed through `/init-plan` (not `/execute-epic`) because the source epic's own `/refine-epic` pass measured 6 tasks / 10 components — over `/execute-epic`'s ≤6 tasks / ≤2 components scope guard.

## Workflow Status
- [x] **Plan Created**
- [x] **Tasks** - `/create-tasks @.planning/plans/active/32.4_workspace_integrity_sanitization/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/32.4_workspace_integrity_sanitization/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/32.4_workspace_integrity_sanitization/`

## Timeline & Milestones
Estimated 2 days (per the source epic plan's own estimate), split roughly: T1 (pathguard) + T2 (apply.go gate) + T4 (flag + docs) as one cluster, T3 (gitexec + six-site migration) as the largest single unit, T5 (tests) and T6 (non-blocking flag) closing out.

## Resource Requirements
Single backend/core engineer familiar with `internal/autofix`, `cmd/atcr`, and Go's `os/exec`/`path/filepath` stdlib. No new external dependencies — go-gitdiff is already vendored.

## Expected Outcomes
- `internal/security/pathguard.go` — `IsProtectedPath` (blocking) and `FlagsForReview` (non-blocking) checks.
- `internal/gitexec/` — shared, hardened git subprocess wrapper used by all six production git call sites.
- `--allow-config-edits` CLI flag plus `docs/security.md`.
- `--auto-fix` PRs that touch an executable-bit or build-script change carry a visible warning section.

## Risk Summary
Primary risks are scope creep on `--allow-config-edits` becoming a habitual bypass, a missed git-exec migration site leaving the exact gap this plan exists to close, and the non-blocking build-script list either over- or under-warning. See `plan.md`'s Risk Mitigation section for the mitigations (mandatory warning text, a CI-enforced regression grep, and keeping the soft list advisory-only).

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Tasks](tasks/)
- [Sprint Design](sprint-design.md)
