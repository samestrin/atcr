# Plan 32.3: Sandbox Writable Overlay for Polyglot Auto-Fix

## Overview
Fixes the `EROFS` limitation in the ATCR Docker sandbox that makes `--auto-fix` validation effectively Go-only. Adds an opt-in ephemeral writable overlay (`RunSpec.Writable`) so non-Go `validate_command`s (npm, cargo, python, etc.) can write into their working directory during validation, while `--exec`'s existing hard read-only-`/work` guarantee is left provably untouched (`Writable` defaults to `false`).

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`
- [x] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`

## Timeline & Milestones
Originally estimated 2-3 days as an Epic Plan; routed to the full plan pipeline via `/init-plan` after exceeding `/execute-epic`'s <=2-component scope guard (touches `internal/sandbox`, `internal/verify`, and `docs`). Semi-Complex, ~5 user stories.

## Resource Requirements
Backend Team (Go). No new external dependencies — pure stdlib (`os/exec`) plus existing `docker` CLI invocation and `testify` for tests.

## Expected Outcomes
- `--auto-fix` validates successfully for Node, Rust, and Python projects that write into their working tree during build/test, instead of silently discarding a valid fix.
- `--exec`'s documented read-only `/work` guarantee is unchanged, proven by a byte-identical regression test.
- `docs/auto-fix.md` and `internal/verify/autofix_exec.go`'s duplicate doc comment no longer claim `--auto-fix` sandboxing is "effectively Go-only."

## Risk Summary
Shared-infrastructure blast radius (`internal/sandbox/docker.go` also serves `--exec`) is the primary risk, mitigated by making the writable overlay strictly opt-in per `RunSpec` and pinning a regression test on the `Writable:false` default. See `plan.md`'s Risk Mitigation section for the full list.

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Documentation](documentation/) — sandbox mount semantics + guarantee contract map
- [User Stories](user-stories/)
- [Acceptance Criteria](acceptance-criteria/)
- [Test Planning Matrix](test-planning-matrix.md)
- [Sprint Design](sprint-design.md) — Complexity 9/12 (COMPLEX), 4 phases, 8 days
