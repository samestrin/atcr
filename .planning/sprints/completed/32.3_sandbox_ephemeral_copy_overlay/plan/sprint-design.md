# Sprint Design: Sandbox Writable Overlay for Polyglot Auto-Fix

**Created:** July 21, 2026 10:50:42AM
**Plan:** [Sandbox Writable Overlay for Polyglot Auto-Fix](plan.md)
**Plan Type:** Feature
**Status:** Design Complete

---

## Original User Request

> Fix the `EROFS` (Read-Only File System) limitation in the ATCR Docker sandbox by implementing an opt-in ephemeral copy strategy, unlocking `--auto-fix` validation for non-Go ecosystems (Node, Rust, Python) that require write access to the project directory during testing. `dockerRunArgs`/`Run` (`internal/sandbox/docker.go`) are shared infrastructure also used by `--exec` (Epic 11.0), which documents and pins a hard read-only-`/work` guarantee — the fix is therefore opt-in per `RunSpec`, not a global behavior change: `--exec` keeps its exact current read-only-`/work` mount and semantics untouched; only `--auto-fix`'s validation call site opts into the writable ephemeral copy.

**Referenced Resources:**
- [Current Sandbox Guarantees](documentation/current-sandbox-guarantees.md)
  - **Summary**: Contract map of every existing doc/code-comment claim about `--exec`'s read-only `/work` guarantee, each annotated PRESERVE (must stay true for `Writable:false`) or UPDATE (T6 docs-parity scope).
  - **Key Points**: `docs/execution.md` and the `internal/sandbox` package doc's hard MUST are PRESERVE anchors; `docs/auto-fix.md`'s three stale passages plus `autofix_exec.go`'s duplicate doc comment are the UPDATE set; a new implicit `/bin/sh` + `cp -a` image-capability constraint must be documented.
- [Docker tmpfs and Read-Only Mounts](documentation/docker-tmpfs-and-read-only-mounts.md)
  - **Summary**: Official Docker reference excerpts for `--tmpfs` syntax (`rw,exec,size=`) and the global `--read-only` rootfs flag — the exact mechanism the `/work` writable overlay mirrors from the existing `/scratch` mount.
  - **Key Points**: tmpfs mounts are memory-backed and ephemeral (die with the container); a path on a `--read-only` rootfs has no writable backing unless explicitly given one via `--tmpfs` or a non-`:ro` volume.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Sandbox Writable Overlay
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 8 days
**Phases:** 4
**Pattern:** Foundation → Core Mechanism → Injection & Auto-Fix Wiring → Regression & Docs Parity

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
Docker tmpfs writable overlay pattern
shell wrap argv injection safety
opt-in RunSpec configuration field
read-only rootfs ephemeral copy
sandbox regression test byte-identical
```

---

## Complexity Breakdown

- **Architecture:** 2/3 - New mechanism (shell-wrap payload injection via `exec "$@"`, conditional `/src`+`/work` mount split) layered onto an existing, mirrored idiom (`ScratchSize`/`--tmpfs` pattern already proven by `/scratch`); not a wholesale architectural overhaul.
- **Integration:** 2/3 - Three components touched (`internal/sandbox`, `internal/verify`, `docs/`) but all single-repo and funneled through one pure choke point (`dockerRunArgs`), not a multi-system integration.
- **Story/Task & Test:** 3/3 - 5 stories, 15 ACs, 4 high-complexity ACs (02-02, 02-03, 03-01, 03-03), spanning unit, integration, and documentation-review test types.
- **Risk/Unknowns:** 2/3 - Risks are numerous (shared-infrastructure blast radius, shell-injection surface, distroless-image incompatibility, `WorkSize` sizing) but each already has a specified mitigation from two prior `/refine-epic --deep` passes — minor residual unknowns, not open-ended ones.

**Time Formula:** Sum of per-story effort estimates (S=1 day, M=2 days), sequenced by hard dependency chain (Story 1 → 2 → 3 → 4 → 5)
**Calculation:** S(1: Config Surface) + M(2: Conditional Mount) + M(2: Setup Injection) + S(1: Auto-Fix Opt-In) + M(2: Regression & Docs) = 8 days

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** false (not strongly gated)
**Suggested command:** `/create-sprint @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3 (met: 9/12, 4 phases); gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days (met: 9/12 and 8 days); strong gated at complexity >= 10/12 (not met).

---

## Phase Structure

### Phase 1: Foundation — Config Surface (1 day)
- **Items:** Story 1 (Opt-In Writable Configuration Surface)
- **Focus:** RED/GREEN/REFACTOR on `RunSpec.Writable bool` (`internal/sandbox/sandbox.go`) and `DockerConfig.WorkSize string` + its `DefaultDockerConfig()` default (`internal/sandbox/docker.go`), mirroring the `ScratchSize` pattern exactly. Purely additive — no branching logic yet. Full existing `internal/sandbox` suite and both `--exec` call sites must pass unmodified as the zero-value proof.

### Phase 2: Core Mount Mechanism (2 days)
- **Items:** Story 2 (Conditional Writable /work Mount)
- **Focus:** Branch `dockerRunArgs` on `spec.Writable`: `false` path stays textually untouched (`-v SnapshotDir:/work:ro`, pinned by `TestDockerRunArgs_HardeningFlagsPresent`); `true` path mounts `SnapshotDir:/src:ro` and adds `--tmpfs /work:rw,exec,size=<cfg.WorkSize>`. This phase makes the writable backing exist; it does not yet populate it — the populating setup step is Story 3's (Phase 3). Accordingly, Story 2's AC 02-03 closes in two parts: its argv/stdin-level assertions (setup step present only when `Writable:true`, `renderCommand` unaffected) land here with the mount branch, while its end-state integration scenarios (a payload observably writing under `/work`, `ExitCode 0` with no `EROFS`) are verified in Phase 3 once Story 3's injection exists — no Phase 2 exit gate requires them.

### Phase 3: Setup Injection & Auto-Fix Wiring (3 days)
- **Items:** Story 3 (Ephemeral-Copy Setup Injection), Story 4 (`--auto-fix` Opts Into the Writable Overlay)
- **Focus:** Story 3 wraps Command-mode argv in `/bin/sh -c 'cp -a /src/. /work/ && cd /work && exec "$@"' -- <command...>` (positional expansion, no interpolation) and prepends the same copy step to Script-mode stdin content, gated strictly on `spec.Writable`. Story 4 is the one-line payoff: `RunSandboxedValidation` sets `Writable: true`, reaching the actual `--auto-fix` code path, with a new pinning assertion (`fb.gotSpec.Writable`) added to the existing routing test. Sequenced together because Story 4 has no observable effect without Story 3's injection mechanism.

### Phase 4: Regression Proof & Docs Parity (2 days)
- **Items:** Story 5 (Regression Proof and Documentation Parity)
- **Focus:** Test-and-docs-only closing phase — no new production code. Adds `Writable:true` argv/stdin shape assertions for both modes, a `writeFakeDocker`-based functional proof that a mock script can write under `/work`, an explicit `Writable:false` byte-identical regression case, and confirms `TestDockerRunArgs_HardeningFlagsPresent` / `TestResolveAutoFixSandbox_BuildsAndPreflights` stay green unmodified. Rewrites `docs/auto-fix.md`'s three stale passages and `internal/verify/autofix_exec.go`'s duplicate doc comment in the same pass. This phase also serves as sprint Validation.

---

## Work Decomposition

### Story 1: Opt-In Writable Configuration Surface (S)
- **Testable elements:** `RunSpec.Writable` defaults false (AC 01-01, Unit); `DockerConfig.WorkSize` default set in `DefaultDockerConfig()` (AC 01-02, Unit); full-suite regression proving zero behavior change for existing callers (AC 01-03, Integration).
- **Dependencies:** None — first story.

### Story 2: Conditional Writable /work Mount (M)
- **Testable elements:** `Writable:false` argv byte-identical (AC 02-01, Unit); `Writable:true` mounts `/src:ro` + `/work` tmpfs (AC 02-02, Unit); setup step populates `/work` before payload runs (AC 02-03, Unit/Integration — argv/stdin-level assertions in Phase 2; end-state integration scenarios verified in Phase 3 alongside Story 3's injection, per AC 02-03's own scope note naming Story 3 the mechanism authority).
- **Dependencies:** Story 1.

### Story 3: Ephemeral-Copy Setup Injection (M)
- **Testable elements:** Command-mode shell-wrap injection (AC 03-01, Unit); Script-mode stdin-prepend injection (AC 03-02, Unit); no-interpolation injection-safety invariant (AC 03-03, Unit, adversarial/security-focused).
- **Dependencies:** Story 1, Story 2.

### Story 4: `--auto-fix` Opts Into the Writable Overlay (S)
- **Testable elements:** Auto-fix validation requests the writable overlay (AC 04-01, Unit); `Writable` flag pinned by test while `--exec`/Preflight stay read-only (AC 04-02, Unit).
- **Dependencies:** Story 1, Story 2, Story 3.

### Story 5: Regression Proof and Documentation Parity (M)
- **Testable elements:** `Writable:true` argv/stdin shape tests for both modes (AC 05-01, Unit); fakeDocker-based write proof under `/work` (AC 05-02, Unit); `Writable:false` regression anchor stays unmodified (AC 05-03, Unit); docs/doc-comment parity rewrite (AC 05-04, Documentation).
- **Dependencies:** Stories 1-4.

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Co-located `_test.go` files (Go convention) — `internal/sandbox/docker_test.go`, `internal/sandbox/sandbox_test.go`, `internal/verify/sandboxvalidate_test.go`, `internal/verify/autofix_exec_test.go`.

**Test File Placement Examples:**
- New `Writable:true`/`Writable:false` mount and argv cases → `internal/sandbox/sandbox_test.go` (sibling cases alongside `TestDockerRunArgs_HardeningFlagsPresent`, `TestDockerRunArgs_ScriptUsesStdinShell` — never edited in place).
- fakeDocker functional write-proof → `internal/sandbox/docker_test.go`, reusing the existing `writeFakeDocker` shim.
- `RunSandboxedValidation` pinning assertion → `internal/verify/sandboxvalidate_test.go` (extends `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec`).

**Unit/Integration/E2E:** 13 ACs unit (argv/stdin/config assertions, daemon-free); 1 AC integration (01-03, full-package regression across `internal/sandbox` + `internal/tools`); 1 AC documentation (05-04, manual/grep-based review, no automated test). `testify` (existing dependency) covers all new assertions; no new test tooling required.

**Test Environment Status:**
- Framework: Go stdlib `testing` + `testify` — already fully wired.
- Execution: `go test ./...` — existing CI command, no new runner needed.
- Coverage Tools: `go test -coverprofile=coverage.out ./...` — existing, baseline 80%.

---

## Architecture

**Primitives:** `RunSpec` (`Command []string`, `Script string`, `SnapshotDir string`, new `Writable bool`); `DockerConfig` (existing `ScratchSize string`, new `WorkSize string`).
**Module Boundaries:** `dockerRunArgs` (pure, I/O-free argv/mount-list builder — the single choke point for both modes); `Run` (executes the `docker` CLI, constructs `cmd.Stdin` for Script mode); `RunSandboxedValidation` (`internal/verify`) — the sole `--auto-fix` caller, Command-mode only; `renderCommand` stays strictly display-only and is never a valid injection site.
**External Dependencies:** `docker` CLI (`--tmpfs`, `-v ...:ro`, global `--read-only`); the validation container image must additionally provide `/bin/sh` and a `cp` supporting `-a` when `Writable:true` (true for `alpine`/`golang`-family images; false for `distroless`/`scratch` — a new, documented implicit constraint, not a runtime-checked one).
**Replaceability:** The `sandbox.Backend` interface is unchanged; all Docker-specific mount/wrap logic stays isolated inside `internal/sandbox/docker.go`, so a future non-Docker backend only needs to satisfy the same `RunSpec`/`RunResult` contract.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| Command-mode shell wrap (`dockerRunArgs`, `Writable:true`) | Injecting `cp -a /src/. /work/ && cd /work && exec "$@"` ahead of `spec.Command` | Shell-metacharacter injection (`;`, `$()`, backticks) if `spec.Command` tokens were ever string-concatenated into the `-c` script text | Fixed Go string-literal script text; `spec.Command` passed exclusively as separate argv elements after `--` (positional `$@` expansion) — never interpolated; unit test asserts a metacharacter-bearing token survives literally |
| Script-mode stdin prepend (`Run`, `Writable:true`) | Prepending the copy-step line to `spec.Script` before it is streamed to `cmd.Stdin` | None new — script body already flows over stdin, never argv/shell-string-built; risk is only accidental scope creep into interpolation | Prepend is a plain string concatenation of a fixed setup line + the untouched script body, delivered over stdin exactly as today |
| Read-only snapshot boundary (`/src` when `Writable:true`) | Ensuring the host-mounted snapshot never becomes writable, only the ephemeral tmpfs copy | Accidental mount-flag regression that drops `:ro` from `/src`, or a `cp` invocation that resolves outside `/work` | `/src:ro` mount flag fixed in code (not caller-configurable); `cp -a /src/. /work/` targets a fixed, hardcoded destination; tmpfs dies with the container, host never mutated |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| `cp -a /src/. /work/` full source-tree copy (setup step, `Writable:true` only) | One full repo-sized copy per `--auto-fix` validation run | Complete within the existing per-run sandbox `Timeout`, without exhausting the `WorkSize` tmpfs cap | Default `WorkSize` sized deliberately larger than `ScratchSize`'s 64m build-cache default (generous headroom for a full source tree); documented as a tunable code constant for operators with unusually large trees |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Undersized `WorkSize` | Full source-tree copy exceeds the tmpfs cap mid-`cp` | Fails with a tmpfs-full error, indistinguishable from a genuine validation failure unless the generous default and doc guidance are followed — flagged as a known Low-impact risk, not solved by new validation logic |
| Shell-less validation image | Operator points `--auto-fix` at a `distroless`/`scratch` image with `Writable:true` in effect | Container start/exec fails (no `/bin/sh`) — documented as a known implicit constraint (T6/AC 05-04), not auto-detected or gracefully degraded |
| `Writable:false` control-group paths | `--exec`'s two call sites, and `ResolveAutoFixSandbox`'s Preflight `RunSpec` | Byte-identical to pre-sprint argv/mounts — proven by `TestDockerRunArgs_HardeningFlagsPresent`, `TestResolveAutoFixSandbox_BuildsAndPreflights`, and AC 01-03/05-03 staying green with zero edits to their existing assertions |
| Metacharacter-bearing command tokens | `spec.Command` contains `;`, `$(...)`, backticks, under `Writable:true`'s shell wrap | Token survives as a literal, non-interpreted argv element passed via `-- "$@"` — never treated as shell script text (AC 03-03) |

### Defensive Measures Required

- **Input Validation:** `RunSpec.validate()` is unchanged and not weakened — `Writable` adds no new validation surface or bypass path.
- **Error Handling:** tmpfs-full and shell-less-image failures must remain visible as run failures (not silently swallowed or misreported as a generic validation failure) — mitigated by generous default sizing and explicit documentation rather than new runtime detection.
- **Logging/Audit:** `renderCommand` continues to log only the caller's original command/script for evidence purposes (display-only, unchanged) — the injected setup step is intentionally not surfaced there, consistent with the epic's corrected design.
- **Rate Limiting:** Not applicable — no new external-facing surface.
- **Graceful Degradation:** None added for shell-less images by design; the failure surfaces as an ordinary backend/exec fault, documented so it is diagnosable rather than mysterious.

---

## Risks

**Technical:**
- Risk: A careless edit to the `Writable:false` branch of `dockerRunArgs` silently changes the default-path mount or argv. → Mitigation: keep that branch's code textually untouched; rely on `TestDockerRunArgs_HardeningFlagsPresent` (unmodified) plus the Preflight control group as regression anchors.
- Risk: Shell-wrapping Command mode reintroduces a shell-injection surface. → Mitigation: `-- "$@"` positional expansion only, never string-concatenation of `spec.Command` into the `-c` script text; adversarial unit test (AC 03-03) asserts metacharacter survival.
- Risk: Undersized `WorkSize` truncates `cp -a`, producing a failure indistinguishable from a genuine validation error. → Mitigation: generous default sizing well above typical repo footprints, documented as a tunable.
- Risk: Widening shared `internal/sandbox` mount/argv logic risks an accidental `--exec` regression. → Mitigation: `Writable` is strictly opt-in (zero value `false`); Story 5's regression tests assert byte-identical `Writable:false` output.

**TDD-Specific:**
- Risk: A new table-driven test case is added in a way that requires touching `TestDockerRunArgs_HardeningFlagsPresent`'s existing assertions. → Mitigation: add only additive sibling cases/functions; run that test in isolation before/after each phase to confirm zero diff.
- Risk: The fakeDocker-based write-proof test is flaky or silently skipped on non-POSIX CI runners, giving false confidence. → Mitigation: mirror the existing Windows-skip behavior exactly, and assert on the observable file write (not just argv shape) so a non-skipped run is a genuine functional proof.
- Risk: `docs/auto-fix.md` and `autofix_exec.go`'s doc comment drift out of agreement since they're hand-edited rather than generated from one source. → Mitigation: edit both in the same phase/commit, grep both for "read-only" and "Go-only" post-edit to confirm no stale phrasing survives.

---

**Next:** `/create-sprint @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`
