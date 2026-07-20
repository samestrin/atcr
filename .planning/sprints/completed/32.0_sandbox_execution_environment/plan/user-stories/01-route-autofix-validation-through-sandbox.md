# User Story 1: Route Auto-Fix Validation Through the Sandbox by Default

**Plan:** [32.0: Sandboxed Auto-Fix Validation](../plan.md)

## User Story

**As an** ATCR maintainer running `atcr --auto-fix` against LLM-generated patches (locally or in an unattended CI runner)
**I want** the post-apply validation command (`go build`, `npm test`, etc.) to execute inside the existing `internal/sandbox` container isolation instead of directly on the host via `os/exec`
**So that** a hallucinated or prompt-injected malicious build/test script cannot compromise my machine or CI runner during the auto-fix lifecycle, without any change to how patches are applied, reverted, or how branches/commits/PRs are created

## Story Context

- **Background:** `runAutoFix` (`cmd/atcr/autofix.go:252`) already applies a patch, then calls `verify.RunConfiguredValidation(ctx, argv, dir, timeout)` to validate it, then reverts on failure or proceeds to GitHub mutation on success. `RunConfiguredValidation` currently shells out directly on the host via `os/exec.CommandContext` — this is the exact untrusted-code execution gap Epic 32.0 exists to close. `internal/sandbox.Backend` (a `DockerBackend` built for Epic 11.0's `--exec` feature) already provides network-isolated, read-only-rootfs, capability-dropped, resource-capped, non-root container execution via `Backend.Run(ctx, RunSpec) (RunResult, error)`.
- **Assumptions:** A `sandbox.Backend` value (already constructed and `Preflight()`-checked) is available to whatever code performs the routing decision — constructing/resolving that backend from configuration is out of scope for this story (delivered by Story 2's resolver). The default Go validation command (`go build ./...` and similar) works correctly against a read-only `/work` mount plus the writable `/scratch` overlay that `DockerBackend` already provides (HOME/TMPDIR/GOCACHE/GOTMPDIR redirected there); commands that need to write into the working tree itself are a known edge case explicitly deferred to Story 2's backend resolution/gating design.
- **Constraints:** Zero behavior change to `ApplyPatch`, `RevertPatch`, `CleanupBackups`, or any branch/commit/PR logic in `runAutoFix` — this story touches only the validation call site. The existing `verr != nil` (cannot validate) vs. `!res.Passed()` (validation ran and failed) failure taxonomy in `runAutoFix` must be preserved byte-for-byte regardless of which execution path (host or sandbox) produced the result, since callers branch on that distinction to decide revert-and-report wording. No new sandbox backend is built and no Docker SDK is introduced — `sandbox.Backend` is consumed as-is through its existing interface.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | None to implement and unit-test against the `sandbox.Backend` interface (a fake/stub backend suffices); full end-to-end production wiring is completed jointly with Story 2's resolver, which supplies the real constructed backend. |

## Success Criteria (SMART Format)

- **Specific:** When sandboxed execution is selected, `RunConfiguredValidation`'s command execution is dispatched through `sandbox.Backend.Run(ctx, RunSpec{Command: argv, Timeout: timeout, SnapshotDir: dir})` instead of `os/exec.CommandContext`, and the returned `sandbox.RunResult` is translated into a `verify.ValidationResult` that preserves `Passed()` semantics (`StartError == nil && !TimedOut && ExitCode == 0`).
- **Measurable:** 100% of existing `internal/verify` and `cmd/atcr` auto-fix unit/integration tests pass unmodified in outcome (same pass/fail/revert behavior) when run against a fake `sandbox.Backend` standing in for Docker; new tests cover the RunResult-to-ValidationResult translation for: exit 0, non-zero exit, `TimedOut: true` (mapped to `ValidationResult.TimedOut`, not surfaced as a fabricated exit code), and a `Run` call returning a Go error (mapped to `StartError`, matching the "cannot validate" branch in `runAutoFix`).
- **Achievable:** Confined to the translation/adapter layer between `sandbox.RunResult` and `verify.ValidationResult`; no changes to `ApplyPatch`, `RevertPatch`, or GitHub-mutation code paths, and no new sandbox backend implementation is required.
- **Relevant:** Delivers the epic's primary acceptance criterion — "the Auto-Fix pipeline routes its validation steps through the existing `internal/sandbox` by default" — this is the actual security boundary the epic exists to create; without this story the epic ships no functional isolation.
- **Time-bound:** Completable within one sprint cycle as a single cohesive unit (adapter function + its unit tests + the call-site swap in the validation path), independent of Story 2's resolver work landing in parallel.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-sandbox-routed-command-dispatch.md) | Sandbox-Routed Command Dispatch | Unit |
| [01-02](../acceptance-criteria/01-02-runresult-to-validationresult-translation.md) | RunResult-to-ValidationResult Translation | Unit |
| [01-03](../acceptance-criteria/01-03-zero-behavior-change-to-runautofix-pipeline.md) | Zero Behavior Change to the runAutoFix Pipeline | Integration |

## Original Criteria Overview

1. When a sandbox backend is supplied to the validation path, the configured validation command runs via `sandbox.Backend.Run` (combined output, no direct `os/exec` invocation on the host) instead of the current host-direct execution.
2. The `sandbox.RunResult` returned by `Run` is translated into a `verify.ValidationResult` whose `Passed()`, `TimedOut`, `StartError`, and exit-code semantics exactly match what `runAutoFix` (`cmd/atcr/autofix.go:252`) already expects — the existing "cannot validate" vs. "validation failed" vs. "validation passed" branches in `runAutoFix` require no changes.
3. All other stages of `runAutoFix` (`ApplyPatch`, `RevertPatch`, `CleanupBackups`, branch/commit/PR creation) are provably unaffected — verified by the existing auto-fix test suite passing unchanged against the new sandboxed path.

## Technical Considerations

- **Implementation Notes:** Add a sandbox-routing execution path (e.g. a new internal function or a `Backend`-aware branch inside/alongside `RunConfiguredValidation`) that builds a `sandbox.RunSpec{Command: argv, Timeout: timeout, SnapshotDir: dir}` and calls `Backend.Run`, then maps the `RunResult` into `ValidationResult`: `RunResult.Output` (combined stdout+stderr) maps into `ValidationResult.Stdout` (with `Stderr` left empty, since the sandbox only returns combined output — document this stream-splitting loss explicitly rather than fabricating a split); `RunResult.TimedOut` maps directly to `ValidationResult.TimedOut`; `RunResult.ExitCode` maps to `ValidationResult.ExitCode` except the conventional timeout code 124 must not be double-reported once `TimedOut` is already true; a Go error returned by `Run` (Docker runtime fault, e.g. exit 125-127 or a signal) maps to `ValidationResult.StartError`, matching the existing "could not even validate" branch in `runAutoFix`. Two further adapter decisions required by the translation-gap table in `documentation/autofix-validation-contract.md`: (1) `Duration` — `sandbox.RunResult` captures no duration, so the adapter measures wall-clock around the `Backend.Run` call itself and populates `ValidationResult.Duration` (never left zero, preserving host-path parity), per AC 01-02 Edge Case 4; (2) `StdoutTruncated`/`StderrTruncated` — the sandbox reports no per-stream truncation signal, so both flags are left `false` rather than guessed (per AC 01-02 Edge Case 3), a documented, acceptable difference from the host path's precise flags.
- **Integration Points:** Sole call site is `runAutoFix` at `cmd/atcr/autofix.go:252`, which currently calls `verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)`. This story either extends that function's signature/behavior to accept an optional `sandbox.Backend` or introduces a parallel sandboxed variant selected by the caller — the exact shape is an implementation decision for `/design-sprint`, but either way `runAutoFix`'s three post-call branches (`verr != nil`, `!res.Passed()`, success) must require zero code changes.
- **Data Requirements:** None (no schema, no persisted state) — this is a pure control-flow and data-translation change between two existing structs (`sandbox.RunSpec`/`RunResult` and `verify.ValidationResult`).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `sandbox.RunResult.Output` is combined stdout+stderr, but `verify.ValidationResult` has split `Stdout`/`Stderr` fields consumed by downstream reporting/logging — collapsing both streams into one risks silently changing what operators see in failure reports. | Medium | Document the stream-collapse explicitly in the adapter and route combined output into `Stdout` only, leaving `Stderr` empty; confirm with `/design-sprint` whether any downstream consumer depends on the split (e.g. PR comment formatting) before finalizing. |
| Docker runtime faults (exit 125-127, killed-by-signal) are surfaced by `sandbox.Backend.Run` as Go errors rather than exit codes, but `runAutoFix` treats `verr != nil` (cannot validate) and `!res.Passed()` (ran and failed) as textually different failure messages — misrouting one into the other changes operator-facing error text even though the epic requires "zero behavior change." | Medium | Write explicit adapter unit tests asserting Docker-runtime-fault results map to `StartError` (the "cannot validate" branch) and only a clean container run with non-zero program exit maps to `ExitCode != 0` (the "validation failed" branch). |
| The default read-only `/work` mount is correct for `go build ./...`, but any validation command that needs to write into the tree itself (coverage files, codegen, `lint --fix`) would silently fail or behave differently once routed through the sandbox — a functional regression disguised as "zero behavior change." | Low | Explicitly scope this story to the default read-only-compatible Go validation path per the epic's grounding facts; flag write-into-tree validation commands as a known limitation resolved by Story 2's backend resolution/gating, not silently patched over here. |

---

**Created:** July 19, 2026
**Status:** Acceptance Criteria Generated
