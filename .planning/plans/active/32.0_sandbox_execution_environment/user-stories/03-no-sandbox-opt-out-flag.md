# User Story 3: `--no-sandbox` Opt-Out Flag with CLI Security Warnings

**Plan:** [32.0: Sandboxed Auto-Fix Validation](../plan.md)

## User Story

**As an** ATCR operator running `--auto-fix` in an environment where Docker is unavailable (e.g. a minimal CI runner or a locked-down workstation)
**I want** an explicit `--no-sandbox` flag that bypasses the sandboxed validation gate and falls back to direct host execution, paired with an unmissable stderr warning on every run that uses it
**So that** I can still use `--auto-fix` when containerization isn't an option, while never being able to forget — or let a teammate silently inherit — that validation is running unsandboxed against untrusted, LLM-generated code

## Story Context

- **Background:** Story 1 routes `--auto-fix`'s post-apply validation step through `internal/sandbox` by default; Story 2 builds the resolver/Preflight gate that makes sandboxing a hard requirement unless explicitly bypassed. Without an opt-out, any environment lacking Docker (or a working `sandbox:` config) simply cannot use `--auto-fix` at all — a legitimate need per the epic's "Opt-Out Fallback" requirement. This story delivers that fallback, but as a loud, explicit, per-run decision rather than a silent default.
- **Assumptions:** Sandboxing is on by default with no other bypass path — `--no-sandbox` is the *only* way validation reaches the pre-Story-1 direct `os/exec` path. The flag is CLI-only for this story; a config-level counterpart (e.g. `auto_fix.no_sandbox`) is an open design question deferred to `/design-sprint`, not in scope here. Story 2's resolver/Preflight gate already exists and is what this flag skips.
- **Constraints:** The warning must print to stderr on every single `--no-sandbox` invocation, not once per session/machine — CI logs must show it on every run, per the plan's Risk Mitigation ("`--no-sandbox` could become a de facto default if the warning is too easy to ignore in CI logs"). No other existing `--no-*` opt-out in this codebase (`--no-ignore`, `--no-cache`, `--no-scorecard`, `--no-local-debt`) prints any warning today — this flag must be strictly louder than all of them, not modeled on their silence. The flag must not alter behavior at all unless `--auto-fix` is also passed (it is meaningless without it).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | Story 2 (sandbox resolver + Preflight gate in `validateAutoFixBackend`) — this story bypasses that gate, so the gate must exist first |

## Success Criteria (SMART Format)

- **Specific:** `addAutoFixFlags` (`cmd/atcr/autofix.go:43`) registers a new `cmd.Flags().Bool("no-sandbox", false, ...)` flag with security-warning help text; `validateAutoFixBackend` skips Story 2's sandbox resolution/Preflight gate entirely when the flag is set, and `runAutoFix`'s validation call uses the direct `os/exec` path instead of the sandboxed one; every run with `--no-sandbox` set prints a strict stderr warning before validation executes.
- **Measurable:** A test asserts the flag is registered and defaults to `false`; a test asserts `validateAutoFixBackend` with `--no-sandbox=true` never invokes the sandbox resolver (no Preflight call, no Docker requirement) and instead returns a backend that resolves to direct execution; a test captures stderr across two consecutive `--no-sandbox` runs in the same process and asserts the warning string appears both times (proving it is not a one-time/session-scoped warning).
- **Achievable:** Purely additive flag-registration and a conditional branch in already-identified call sites (`cmd/atcr/autofix.go:43`, `validateAutoFixBackend`, `runAutoFix`); no new packages, no sandbox internals touched.
- **Relevant:** Delivers the epic's Acceptance Criterion 2 verbatim ("A `--no-sandbox` flag exists to bypass the isolation, accompanied by strict security warnings") and is the only way `--auto-fix` remains usable in Docker-less environments once Story 1/2 make sandboxing the default gate.
- **Time-bound:** Completed within this sprint alongside Stories 1, 2, and 4; unblocked once Story 2's resolver/gate lands.

## Acceptance Criteria Overview

1. `--no-sandbox` is registered as a boolean flag (default `false`) on the `review` command via `addAutoFixFlags`, with cobra help text that states plainly that passing it disables container isolation and runs LLM-generated validation commands directly on the host.
2. When `--no-sandbox` is passed alongside `--auto-fix`, `validateAutoFixBackend` bypasses Story 2's sandbox resolution/Preflight requirement entirely (Docker need not be installed, no `sandbox:` config need exist) and the resolved backend causes `runAutoFix`'s validation step to execute via the pre-existing direct `os/exec` path.
3. Every run that takes the `--no-sandbox` path prints a clearly-marked security warning to stderr (not stdout) before the validation command executes, and this warning prints on every invocation — never suppressed after the first, never gated behind a "seen once" flag/env var/state file.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/32.0_sandbox_execution_environment/`_

## Technical Considerations

- **Implementation Notes:** Add the flag next to the existing `--auto-fix` bool at `cmd/atcr/autofix.go:43-52`, following the same help-text density/warning style already used there (the multi-line security caveats baked into the `--auto-fix` help string itself). Thread a `noSandbox bool` value from the flag into `validateAutoFixBackend`'s signature (or read it via `cmd.Flags().GetBool("no-sandbox")` at the top of that function, matching how the function already reads other flags), and short-circuit Story 2's resolver call with an early branch before it would otherwise run. The warning print should be a small dedicated helper (e.g. `warnNoSandbox(out io.Writer)`) called unconditionally on every `--no-sandbox` code path — do not reuse `telemetryEnabledFromEnv`'s read-once memoization shape from `cmd/atcr/main.go:348`, since that precedent is explicitly a *one-time* warning and this one must not be.
- **Integration Points:** `cmd/atcr/autofix.go` (`addAutoFixFlags`, `validateAutoFixBackend`, `runAutoFix`); reads but does not modify Story 2's resolver function signature — it calls around it, not into it. No changes to `internal/sandbox`, `internal/verify`, or `internal/autofix` package internals.
- **Data Requirements:** None — no new config schema field in this story (config-level opt-out is explicitly out of scope per the plan's grounding facts); the flag's boolean state lives entirely in the cobra `*Command` for the duration of one invocation.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| The warning is implemented as a one-time/session-memoized print (mirroring the `ATCR_TELEMETRY` precedent at `cmd/atcr/main.go:348`) and gets missed in scrollback on repeated CI runs | High | Explicitly require and test that the warning prints on every invocation with no memoization; call out in code comments that this diverges intentionally from the one-time-warning precedent it is modeled on for mechanism only |
| `--no-sandbox` becomes a silent default because operators add it once to a CI config and never revisit it | Medium | Warning text explicitly names the risk ("validation is running WITHOUT container isolation") so it reads as an active decision every time logs are reviewed; documentation (Story 4) reinforces this is a last-resort, not a normal mode |
| Flag interacts incorrectly with Story 2's all-or-nothing gate (e.g. still requires Docker config to be present even though it's supposed to bypass it) | Medium | Test explicitly asserts no sandbox/Docker preflight call occurs when `--no-sandbox` is set, independent of whether a `sandbox:` config block exists |

---

**Created:** July 19, 2026
**Status:** Draft - Awaiting Acceptance Criteria
