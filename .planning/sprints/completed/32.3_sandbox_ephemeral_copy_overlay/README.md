# Sprint 32.3: Sandbox Writable Overlay for Polyglot Auto-Fix

## Plan Metadata
**Created:** 2026-07-20
**Status:** Ready for Execution
**Plan Number:** 32.3
**Plan Type:** feature ✨
**Feature Category:** Sandbox / Auto-Fix Execution
**Priority:** Medium
**Assigned Team:** Backend Team
**Dependencies:** Builds on Epic 32.0 (Sandboxed Auto-Fix Validation) and Epic 11.0 (sandbox.Backend / --exec); shares internal/sandbox/docker.go with --exec, which must remain behaviorally unchanged
**Stakeholders:** Sam Estrin

---

## Overview

Fixes the `EROFS` (Read-Only File System) limitation in the ATCR Docker sandbox by implementing an opt-in ephemeral copy strategy, unlocking `--auto-fix` validation for non-Go ecosystems (Node, Rust, Python) that require write access to the project directory during testing. The fix is strictly opt-in per `RunSpec` — `--exec` keeps its exact current read-only-`/work` mount and semantics untouched; only `--auto-fix`'s validation call site opts into the writable ephemeral copy.

## Timeline

**Complexity:** 9/12 (COMPLEX)
**Timeline:** 8 days
**Phases:** 4
**Pattern:** Foundation → Core Mechanism → Injection & Auto-Fix Wiring → Regression & Docs Parity

| Phase | Focus | Duration |
|-------|-------|----------|
| 1: Foundation — Config Surface | `RunSpec.Writable` / `DockerConfig.WorkSize` fields, purely additive | 1 day |
| 2: Core Mount Mechanism | Conditional `dockerRunArgs` mount branch (`/src:ro` + `/work` tmpfs) | 2 days |
| 3: Setup Injection & Auto-Fix Wiring | `cp -a` setup injection (Command + Script modes) + `RunSandboxedValidation` opt-in | 3 days |
| 4: Regression Proof & Docs Parity | Regression tests, write-proof, docs parity (also serves as sprint Validation) | 2 days |

## Expected Outcomes

- Full existing `internal/sandbox` test suite and both `--exec` call sites pass unmodified — zero behavior change for `Writable:false` (the default)
- Non-Go `validate_command`s (`npm run build`, `cargo build`, Python) succeed under `--auto-fix` instead of failing with `EROFS`
- The `/src` snapshot stays read-only for the container's entire lifetime; only the ephemeral `/work` tmpfs is writable and dies with the container — no host file is ever mutated
- `docs/auto-fix.md` and `internal/verify/autofix_exec.go`'s doc comment updated to drop the stale "effectively Go-only" claim

## Risk Summary (Top 3)

1. **Shell-injection surface in the Command-mode wrap.** Wrapping `spec.Command` in `/bin/sh -c '... && exec "$@"' -- <command...>` reintroduces a shell where none existed. Mitigation: positional `-- "$@"` expansion only, never string-concatenation into the `-c` script text; adversarial unit test (AC 03-03) asserts metacharacter survival.
2. **Widening shared `internal/sandbox` mount/argv logic risks an accidental `--exec` regression.** `dockerRunArgs`/`Run` are shared with `--exec` (Epic 11.0). Mitigation: `Writable` is strictly opt-in (zero value `false`); Story 5's regression tests assert byte-identical `Writable:false` output, anchored by the unmodified `TestDockerRunArgs_HardeningFlagsPresent`.
3. **Undersized `WorkSize` truncates `cp -a`,** producing a failure indistinguishable from a genuine validation error. Mitigation: generous default sizing well above typical repo footprints, documented as a tunable code constant.

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Phase-by-phase TDD execution plan (gated, adversarial) |
| [metadata.md](metadata.md) | Sprint tracking and execution metrics |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge manifest (created/referenced entries) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [plan/original-requirements.md](plan/original-requirements.md) | Source-of-truth original request |
| [plan/user-stories/](plan/user-stories/) | 5 user stories |
| [plan/acceptance-criteria/](plan/acceptance-criteria/) | 15 acceptance criteria |
| [plan/documentation/](plan/documentation/) | Sandbox guarantees + Docker tmpfs reference excerpts |
