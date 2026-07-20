# Sprint Design: Sandboxed Auto-Fix Validation

**Created:** July 19, 2026
**Plan:** [Plan 32.0: Sandboxed Auto-Fix Validation](plan.md)
**Plan Type:** ✨ Feature
**Status:** Design Complete

---

## Original User Request

> Build a secure, containerized execution sandbox (e.g., via Docker SDK or ephemeral containers) to safely run validation steps (`go build`, `npm test`) on LLM-generated code. This prevents malicious or hallucinated code from compromising the host machine or CI runner during the `--auto-fix` lifecycle.
>
> **Proposed Solution:** (1) Opt-Out Fallback — a `--no-sandbox` flag or config option for environments where Docker isn't available, heavily warned against. (2) Integration with Epic 17.0 — wire the existing `internal/sandbox.Backend` executor (built during Epic 11.0) into the validation step of the Auto-Fix pipeline so that all compiler/linter/test commands are intercepted and routed through the sandbox.

**Referenced Resources:**

- [Sandbox Backend Interface](documentation/sandbox-backend-interface.md)
  - **Summary**: Documents `internal/sandbox`'s `Backend`/`RunSpec`/`RunResult` triad — the interface the auto-fix validation call must route through.
  - **Key Points**: Every `Backend` MUST guarantee no network, read-only snapshot, resource caps, non-root; `RunSpec.SnapshotDir` is hardcoded read-only with no mount-mode option; `RunResult` has combined `Output` (no stdout/stderr split), `ExitCode`, `TimedOut` — no `Duration`.
- [DockerBackend Implementation](documentation/docker-backend-implementation.md)
  - **Summary**: The sole `Backend` implementation — `docker run` isolation flags, `Preflight`, and the writable `/scratch` overlay that already satisfies Go's build-cache needs against a read-only tree.
  - **Key Points**: `HOME`/`TMPDIR`/`GOCACHE`/`GOTMPDIR` redirected to `/scratch`, so `go build ./...` needs no mount-mode change; Docker exit codes 125-127 and signal deaths surface as Go errors, not program exit codes.
- [Resolver Pattern — ResolveExecBackend](documentation/resolver-pattern-resolveexecbackend.md)
  - **Summary**: The exact resolve-and-preflight shape (`internal/verify/exec.go:24-57`) this plan's new resolver mirrors, including the gating posture to invert.
  - **Key Points**: Off-by-default for `--exec` (`execEnabled=false` → no-op); this plan needs the inverse — sandboxed-by-default, explicit `--no-sandbox` to disable; refuse-on-preflight-failure discipline must be preserved either way.
- [Auto-Fix Gate & Config Surface](documentation/autofix-gate-and-config.md)
  - **Summary**: `validateAutoFixBackend`'s all-or-nothing gate, the `autoFixBackend` carrier struct, and the `SandboxConfig`/`AutoFixConfig` registry surface tension.
  - **Key Points**: `proj.Sandbox` already in scope at the gate (no new plumbing); `SandboxConfig.Validate()` unconditionally requires `Image`+`TestCommand` — an `--exec`-only assumption that must not be silently loosened; no existing `--no-*` flag prints a warning today.
- [Auto-Fix Validation Contract](documentation/autofix-validation-contract.md)
  - **Summary**: `RunConfiguredValidation`/`ValidationResult`'s host-path contract that the sandboxed path must translate into byte-for-byte, including the full `sandbox.RunResult` → `verify.ValidationResult` translation-gap table.
  - **Key Points**: Non-zero exit is not a Go error; `StartError` is reserved for cannot-start faults; `Passed()` is the sole conservative gate; sole production caller is `cmd/atcr/autofix.go:252`.
- [Sandbox Testing Patterns](documentation/sandbox-testing-patterns.md)
  - **Summary**: The `fakeDocker` POSIX shell shim and `dockerRunArgs` argv-assertion patterns used to test sandbox code hermetically, without a live Docker daemon.
  - **Key Points**: Reuse `writeFakeDocker` for the new resolver's tests; `internal/verify/localvalidate_test.go` pins the host-path contract the sandboxed adapter must not silently diverge from.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Sandboxed Auto-Fix Validation
**Complexity:** 8/12 (COMPLEX)
**Timeline:** 9.5 days
**Phases:** 5
**Pattern:** Foundation → Core → Gate Integration & Opt-Out → Integration Testing → Documentation & Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
sandbox resolver preflight gate pattern
auto-fix validation isolation wiring
Docker backend RunResult adapter translation
CLI all-or-nothing gate design
security opt-out flag stderr warning
```

---

## Complexity Breakdown

- **Architecture:** 1/3 - Mirrors an existing, proven pattern (`ResolveExecBackend`) almost structurally verbatim; no new sandbox backend or RunSpec mount-mode change is required for the golden path (write-into-tree validation is explicitly deferred, not solved here).
- **Integration:** 2/3 - Touches 3+ internal integration points (`internal/verify` resolver+adapter, `cmd/atcr` gate/flag/orchestration, `internal/registry` config-validation tension) plus a `docs/` cross-linking requirement, all self-contained within the monorepo (no external systems).
- **Story/Task & Test:** 3/3 - 4 stories, 11 ACs, spanning unit + integration + docs-audit test types; test-planning-matrix shows 7 unit + 4 integration ACs — extensive coverage for an integration-only feature.
- **Risk/Unknowns:** 2/3 - Several concrete open design questions are explicitly flagged by the plan/stories for resolution in this sprint (config-validation split, timeout precedence, stream-collapse documentation) but the domain itself (mirroring a shipped, tested pattern) is well understood — not "significant unknowns."

**Time Formula:** COMPLEXITY_SCORE 8/12 (COMPLEX) → phase_count 5 → baseline range 8-12 days per the complexity→phase mapping table.
**Calculation:** Picked 9.5 days (low end of the COMPLEX range): the resolver/adapter work is a close structural mirror of already-shipped, already-tested code (`ResolveExecBackend`, `DockerBackend`), which offsets the story/test-count weight that pushed the Story/Task score to 3/3.

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** false (not strong — score is 8/12, strong threshold is 10/12)
**Suggested command:** `/create-sprint @.planning/plans/active/32.0_sandbox_execution_environment/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12.

---

## Phase Structure

### Phase 1: Foundation — Sandbox Resolver & Design Decisions (2 days)
**Items:** Story 2 (partial: AC 02-01, 02-02)
**Focus:** Add `internal/verify/autofix_exec.go` with a resolver mirroring `ResolveExecBackend`'s resolve-and-preflight shape but inverting the default posture (sandboxed-on-by-default, explicit disable signal). Resolve the two open design questions this phase is gated on before any other phase can safely build on it: (a) `SandboxConfig.Validate()`'s unconditional `Image`+`TestCommand` requirement — decide whether to split validation, add a relaxed path, or introduce a parallel block, without weakening `--exec`'s existing contract; (b) timeout precedence — `auto_fix.validate_timeout` must win over `sandbox.timeout_secs` via `RunSpec.Timeout`, never silently shrunk by the backend default.

### Phase 2: Core — Sandbox-Routed Validation Dispatch (2 days)
**Items:** Story 1 (AC 01-01, 01-02)
**Focus:** Build the `sandbox.RunResult` → `verify.ValidationResult` adapter per the translation-gap table (combined output → `Stdout` only, `TimedOut` direct-mapped without leaking exit code 124, Docker runtime faults → `StartError`, `Duration` measured by the adapter itself, truncation flags left `false`). Wire the validation call site (`cmd/atcr/autofix.go:252`) to dispatch through a supplied `sandbox.Backend` when present, using a fake/stub backend for unit tests (no dependency on Phase 1's real resolver).

### Phase 3: Gate Integration & Opt-Out (2 days)
**Items:** Story 2 (AC 02-03), Story 3 (AC 03-01, 03-02, 03-03)
**Focus:** Wire Phase 1's resolver into `validateAutoFixBackend` as the gate's fourth checked piece (joining the same `missing []string` collection), threading the resolved backend through `autoFixBackend` into `runAutoFix` per Phase 2's dispatch. Register the `--no-sandbox` flag in `addAutoFixFlags` with security-warning help text, short-circuit the resolver call when set, and add the dedicated (non-memoized) `warnNoSandbox` stderr helper called on every `--no-sandbox` code path.

### Phase 4: Integration Testing & Zero-Behavior-Change Verification (2 days)
**Items:** Story 1 (AC 01-03), Story 2 (AC 02-03 integration leg)
**Focus:** Prove the full `runAutoFix` pipeline is unaffected outside the validation call site — existing auto-fix unit/integration tests pass unmodified in outcome against a fake `sandbox.Backend`; the combined gate error names sandbox failures alongside apply-target/validation-command/GitHub-credential failures in one usage error; `verr != nil` vs `!res.Passed()` branching is provably preserved byte-for-byte regardless of execution path.

### Phase 5: Documentation & Final Validation (1.5 days)
**Items:** Story 4 (AC 04-01, 04-02)
**Focus:** Write/extend `docs/` (either a new `--auto-fix` section in `docs/execution.md` or a new `docs/auto-fix.md` cross-linking it) covering the sandboxed-by-default posture, the `auto_fix:` config block (previously undocumented), and the `--no-sandbox` risk — reconciled against Phases 2-3's final flag name and warning text immediately before merge. Run the existing docs-audit test suite and full Definition of Done validation across all 4 stories.

---

## Work Decomposition

### Story 1: Route Auto-Fix Validation Through the Sandbox by Default
- **01-01** Sandbox-Routed Command Dispatch (Unit) — `sandbox.Backend.Run(ctx, RunSpec{Command, Timeout, SnapshotDir})` replaces direct `os/exec` when a backend is supplied.
- **01-02** RunResult-to-ValidationResult Translation (Unit) — adapter covers exit 0, non-zero exit, `TimedOut`, and `Run` Go-error (→ `StartError`) cases.
- **01-03** Zero Behavior Change to the runAutoFix Pipeline (Integration) — existing auto-fix test suite passes unmodified in outcome against a fake backend.

### Story 2: Sandbox Resolution and Preflight Gate for Auto-Fix
- **02-01** Resolver Builds and Preflights a Sandbox Backend (Unit) — mirrors `ResolveExecBackend`'s field-override + `Preflight` shape.
- **02-02** Inverted Default Posture and `SandboxConfig.Validate()` Tension (Unit) — sandboxed-on-by-default signature; surfaces (does not silently resolve) the `Image`+`TestCommand` validation tension.
- **02-03** Gate Integration — Sandbox Resolution as the Fourth Piece of `validateAutoFixBackend` (Integration) — combined `missing []string` usage error; backend rides `autoFixBackend` without re-resolution.

### Story 3: `--no-sandbox` Opt-Out Flag with CLI Security Warnings
- **03-01** `--no-sandbox` Flag Registration and Help Text (Unit) — boolean flag, default `false`, security-warning help text.
- **03-02** `--no-sandbox` Bypasses Story 2's Resolver/Preflight Gate (Unit/Integration) — no Preflight call, no Docker requirement, when set.
- **03-03** Every-Run (Non-Memoized) stderr Security Warning (Unit) — warning prints on every invocation, never gated behind a "seen once" state.

### Story 4: Document the Auto-Fix Sandbox Security Posture and `--no-sandbox` Risk
- **04-01** Auto-Fix Sandboxed-by-Default Posture and `auto_fix:` Config Block Are Documented (Docs Audit + Manual).
- **04-02** `--no-sandbox` Risk Is Documented and Cross-Linked, Verified Accurate Against Shipped CLI Behavior (Docs Audit + Manual).

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Colocated `*_test.go` files in the same package directory as the code under test (Go standard convention; confirmed by 375 existing `*_test.go` files in-repo).

**Test File Placement Examples:**
- `internal/verify/autofix_exec_test.go` (new) — resolver tests mirroring `exec_test.go`'s shape via the `fakeDocker` shim: refuses-without-backend, builds-and-preflights, no-sandbox is a no-op.
- `internal/verify/localvalidate_test.go` (extended) or a new sibling file — `RunResult`→`ValidationResult` adapter translation cases.
- `cmd/atcr/autofix_test.go` (extended) — gate/flag/orchestration tests with call-recording fakes, including `--no-sandbox` bypass and stderr warning assertions.
- `docs/` — existing automated docs-audit Go test suite (no new test framework).

**Unit/Integration/E2E:** 7 ACs unit-tested (01-01, 01-02, 02-01, 02-02, 03-01, 03-02, 03-03), 4 ACs integration-tested (01-03, 02-03, plus the docs-audit suite covering 04-01/04-02), 0 ACs require a full E2E harness (03-03 notes an optional subprocess case only if an existing CLI subprocess-test harness already exists). All hermetic — no live Docker daemon required, per the `fakeDocker` POSIX shell shim pattern.

**Test Environment Status:**
- Framework: Go standard `testing` library + `testify` (assert/require) — established, no new tooling.
- Execution: `go test ./...` (project test command).
- Coverage Tools: `go test -coverprofile=coverage.out ./...`, 80% baseline.

---

## Architecture

**Primitives:**
- `sandbox.RunSpec{Command []string, Timeout time.Duration, SnapshotDir string}` / `sandbox.RunResult{Command string, ExitCode int, Output string, TimedOut bool}` (existing, Epic 11.0, unmodified).
- `verify.ValidationResult{ExitCode int, Stdout, Stderr string, Duration time.Duration, TimedOut bool, StartError error, StdoutTruncated, StderrTruncated bool}` (existing, Sprint 17.0, unmodified contract).
- `registry.SandboxConfig` / `registry.AutoFixConfig` (existing registry blocks; validation-surface tension is this sprint's Phase 1 design decision).
- `autoFixBackend` (`cmd/atcr/autofix.go:59`) — extended with the resolved `sandbox.Backend` field as the carrier into `runAutoFix`.

**Module Boundaries:**
- New: `internal/verify/autofix_exec.go` (resolver, mirrors `exec.go`'s `ResolveExecBackend`).
- New/extended: adapter function translating `sandbox.RunResult` → `verify.ValidationResult` (co-located with `internal/verify/localvalidate.go` or a sibling file).
- Extended: `cmd/atcr/autofix.go` (`addAutoFixFlags`, `validateAutoFixBackend`, `autoFixBackend`, `runAutoFix`).
- New/extended: `docs/execution.md` or new `docs/auto-fix.md`.

**External Dependencies:** None new — `internal/sandbox` continues to shell out to the `docker` CLI directly (no SDK); this sprint is pure integration work reusing `internal/sandbox`, `internal/verify`, `internal/registry`, and `cmd/atcr` as-is.

**Replaceability:** The routing decision (sandboxed vs. direct `os/exec`) stays a `Backend`-presence branch at the call site, not baked into `runAutoFix`'s control flow — so a future backend swap (e.g. Podman) or the `--no-sandbox` bypass path both remain drop-in changes that never touch `runAutoFix`'s three post-validation branches (`verr != nil`, `!res.Passed()`, success).

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| Sandbox-routed validation dispatch (Story 1) | `internal/verify` validation call site | Hallucinated/prompt-injected malicious build or test script attempting to compromise host or CI runner | Reuses `DockerBackend`'s existing containment guarantees as-is: no network, read-only rootfs, non-root, dropped capabilities, resource caps — no new isolation logic introduced |
| Sandbox resolution/Preflight gate (Story 2) | `validateAutoFixBackend` gate | Silent fallback to unsandboxed execution if resolver construction or `Preflight` fails | Fail-closed: any resolution/preflight failure is a hard refusal joined into the all-or-nothing gate's combined error — never a silent degrade to host execution |
| `--no-sandbox` bypass path (Story 3) | `cmd/atcr/autofix.go` flag handling | Flag becomes a de facto default via CI config that's set once and never revisited, silently normalizing unsandboxed execution | Unconditional (non-memoized) stderr warning on every single invocation, strictly louder than any existing `--no-*` opt-out in the codebase; behavior only activates when `--auto-fix` is also passed |
| `SandboxConfig.Validate()` reuse (Story 2) | `internal/registry/sandbox.go` | Relaxing the unconditional `Image`+`TestCommand` requirement to accommodate auto-fix could inadvertently weaken `--exec`'s existing validation guarantee | Explicit design decision (not silent): split validation, add a parallel light-validation path, or extend the block — `--exec`'s existing contract must not be loosened as a side effect |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| Sandboxed validation command execution (Story 1) | One run per `--auto-fix` invocation, sequential (not concurrent within a single run) | Complete within the configured `auto_fix.validate_timeout` (default 2m) despite container startup overhead | `RunSpec.Timeout` always carries the auto-fix value (Phase 1 timeout-precedence decision), never silently shrunk by the sandbox backend's own 60s default; reuses `DockerBackend`'s existing `MaxConcurrent` semaphore |
| Preflight check on the auto-fix gate (Story 2) | Once per CLI invocation (not per validation run) | Sub-second local checks (docker binary/daemon/image presence) — must not noticeably slow `--auto-fix` startup vs. today's host-direct path | Mirrors `ResolveExecBackend`'s already-proven preflight cost profile for `--exec`; no new preflight logic introduced |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Timeout precedence collision | Configured `sandbox.timeout_secs` (default 60s) is less than `auto_fix.validate_timeout` (default 2m) | `RunSpec.Timeout` carries the auto-fix value unconditionally; the sandbox backend default never silently shrinks the operator's validation budget |
| Docker runtime faults vs. program failures | `docker run` exits 125-127, or the process is killed by signal (128+N), or spawn fails entirely | Mapped to `ValidationResult.StartError` (the "cannot validate" branch), never surfaced as a program exit code (`!Passed()` branch) |
| Output stream collapse | Validation command produces distinct stdout/stderr content consumed downstream (e.g. PR comment formatting) | Combined `sandbox.RunResult.Output` routes to `ValidationResult.Stdout` only; `Stderr` is left empty and this collapse is documented explicitly, not silently fabricated as a split |
| `--no-sandbox` passed without `--auto-fix` | Operator passes `--no-sandbox` alone | Flag is a no-op — meaningless without `--auto-fix`, per Story 3's explicit constraint |
| No `sandbox:` config block configured | Project has no `[sandbox]` block at all, sandboxing is on by default | Gate hard-refuses `--auto-fix` (fail-closed) unless `--no-sandbox` is explicitly passed — a documented behavior change for existing `--auto-fix` users who never configured Docker |

### Defensive Measures Required

- **Input Validation:** Argv-only command dispatch preserved end-to-end (no shell interpolation) from both `RunSpec.validate()` and `RunConfiguredValidation`'s existing contract; `SnapshotDir` absolute-path/no-colon validation reused as-is, no relaxation.
- **Error Handling:** `verr != nil` (cannot validate) vs. `!res.Passed()` (validation failed) taxonomy preserved byte-for-byte regardless of execution path; Docker runtime faults map to the `StartError`/`verr` channel exclusively.
- **Logging/Audit:** `--no-sandbox` prints an unconditional, non-memoized stderr warning on every invocation — explicitly diverges from the one-time-warning precedent (`ATCR_TELEMETRY` at `cmd/atcr/main.go:348`) it is structurally modeled on.
- **Rate Limiting:** Not newly introduced — `DockerBackend`'s existing `MaxConcurrent` semaphore (default 4) already bounds concurrent containers across the backend.
- **Graceful Degradation:** Explicitly NOT graceful by design — sandboxing-expected-but-unavailable is a hard fail-closed refusal, never a silent fallback to unsandboxed execution.

---

## Risks

**Technical:**
- **Risk:** `SandboxConfig.Validate()`'s unconditional `Image`+`TestCommand` requirement forces every `--auto-fix` operator to configure a `test_command` they never use. **Mitigation:** Phase 1 resolves this explicitly (split validation or parallel light-validation path) rather than deferring it into implementation ad hoc.
- **Risk:** Combined `sandbox.RunResult.Output` cannot be re-split into `Stdout`/`Stderr`, risking a silent change to what operators see in failure reports. **Mitigation:** Phase 2's adapter documents the merge explicitly and routes combined output to `Stdout` only; confirmed no downstream consumer depends on the split before finalizing.
- **Risk:** Sandboxing on-by-default is a behavior change for existing `--auto-fix` users without Docker. **Mitigation:** Story 3 (`--no-sandbox`) must ship in the same sprint, not deferred to a later release — Phase 3 sequences it immediately after Phase 1's gate lands.

**TDD-Specific:**
- **Risk:** Adding the resolved-backend field to `autoFixBackend` in Phase 1/3 before Phase 2's routing consumes it risks an `unused`-field lint flag or silent drift if the shape changes. **Mitigation:** A test asserts the field is populated by the gate even before Phase 2's dispatch consumes it; field type/name kept aligned with `internal/sandbox.Backend` (no wrapper type).
- **Risk:** Story 4's documentation is drafted in parallel with Phases 2-3 and could ship inaccurate flag names/warning text if not reconciled. **Mitigation:** Phase 5 explicitly requires a final accuracy pass against the merged CLI help text and warning strings before the docs change merges — sequenced as this sprint's last phase, not parallel-and-forgotten.

---

**Next:** `/create-sprint @.planning/plans/active/32.0_sandbox_execution_environment/`
