# Tech Debt Captured — Sprint 32.3 (sandbox ephemeral copy overlay)

Captured during `/execute-sprint`. Read by `/execute-code-review` (pre-seeded, `SOURCE=execute-sprint`).

## TD-001 — WorkSize default (512m) equals the Memory cap (MEDIUM)
**Origin:** Phase 1, task 1.5 GATE review, 2026-07-21
**File:** internal/sandbox/docker.go:71
**Issue:** The default `WorkSize` "512m" equals `DockerConfig.Memory` "512m". A Docker `--tmpfs` counts against the container memory cgroup, so once the mount is wired, seeding `/work` via `cp -a` a full source tree (plus the 64m `/scratch` tmpfs) draws from the same 512m the workload needs — the writable overlay could OOM-kill the run before it does useful work.
**Why accepted:** Phase 1 only declares the field and default; no mount consumes `WorkSize` yet, so there is no runtime OOM surface in this phase. The size-vs-memory interaction is properly a Phase 2 (mount branch) concern, where the `--auto-fix` caller's actual `Memory` cap is known and the two can be reconciled together.
**Fix in:** Phase 2 (Conditional Writable /work Mount) — reconcile `WorkSize` against `Memory` (size it below the cap, or raise `Memory` when `Writable:true`), and document that a tmpfs size counts against `--memory`.
**Re-confirmed:** 2026-07-21, Phase 2 tasks 2.2.A + 2.5 gate. The mount now consumes `WorkSize`, so this is no longer purely latent. The gate traced the full failure path: a `cp -a` of a real tree (Phase 3) plus the build/test working set billed against 512m (512m /work + 64m /scratch tmpfs headroom > the 512m cap) → OOM-kill (exit 137) → `Run` backend fault → `StartError` → `--auto-fix` fails closed and reverts a valid fix — the exact large-write case (npm `dist/`, cargo `target/`) this overlay exists to enable. Deliberately NOT fixed in Phase 2: both reviewers rated MEDIUM (no production caller opts in until Phase 3), the sprint inline-fix bar is CRITICAL/HIGH only, no Phase 2 AC changes the default, and the correct target sizing depends on the auto-fix caller's actual `Memory` (Phase 3). **Escalate at the Phase 3 gate (3.8)** once `Writable:true` is live for `--auto-fix`; couple the fix with TD-005 (operator-reachable sizing).

## TD-002 — Writable doc comment specifies only Command-mode wrapping (MEDIUM)
**Origin:** Phase 1, task 1.5 GATE review, 2026-07-21
**File:** internal/sandbox/sandbox.go:52
**Issue:** The `RunSpec.Writable` doc comment describes only the Command-mode wrap (`/bin/sh -c 'cp -a /src/. /work/ && cd /work && exec "$@"'`) and leaves Script-mode + `Writable` behavior unstated. `--exec`'s `run_script` path (internal/tools/exec_tools.go:215) is Script mode, so a reader of the field alone cannot tell what `Writable:true` does there.
**Why accepted:** Not a real behavior gap — the sprint plan already specifies Script-mode behavior (T3 / AC 03-02: prepend the `cp -a` copy step to the script body before it is fed to `sh -s`). The doc comment is intentionally scoped to what the mechanism does today (nothing) and will be completed by Phase 3 when Script-mode injection actually lands, which owns that doc surface.
**Fix in:** Phase 3 (Ephemeral-Copy Setup Injection) — extend the `Writable` doc comment to state the Script-mode stdin-prepend behavior alongside the Command-mode wrap.

## TD-003 — Image-requirement sentence duplicated across two doc comments (LOW)
**Origin:** Phase 1, task 1.5 GATE review, 2026-07-21
**File:** internal/sandbox/docker.go:46
**Issue:** The `/bin/sh` + `cp -a` image-requirement sentence (alpine/golang vs distroless/scratch) is repeated verbatim on both `DockerConfig.WorkSize` (docker.go:46) and `RunSpec.Writable` (sandbox.go:64), creating a drift risk if the mechanism's requirements change.
**Why accepted:** Deliberate minor duplication for discoverability — a reader of either field sees the constraint without a cross-file jump. Both fields are independently documented today; consolidating is cosmetic and not worth restructuring mid-sprint.
**Fix in:** Deferrable to `/reconcile-code-review` or a later docs pass — state the requirement once (on `Writable`) and cross-reference from `WorkSize` if drift becomes a concern.

## TD-004 — cfg.WorkSize is interpolated into the tmpfs mount spec without validation (LOW)
**Origin:** Phase 2, task 2.2.A ADVERSARIAL review, 2026-07-21
**File:** internal/sandbox/docker.go:163
**Issue:** `cfg.WorkSize` is interpolated into `--tmpfs /work:rw,exec,size=<WorkSize>` with no grammar check. An empty value yields a malformed `size=` flag (deferred to Docker at run time, per `TestDockerRunArgs_WritableTrueEmptyWorkSize`); a value containing `,` or `:` could append or alter tmpfs mount options (e.g. `1m,noexec`). It is a single argv token, so it can neither inject a new argv element nor mount a host path, and it mirrors the pre-existing unvalidated `ScratchSize` — risk is low and operator-scoped (config-controlled, not caller/request-controlled).
**Why accepted:** Story 1 documented a deliberate "no new validation layer" decision (AC 02-02 Edge Case 3); `WorkSize` carries the same trust level as the existing unvalidated `ScratchSize`, so adding validation for one and not the other would be inconsistent, and the value is operator-owned rather than attacker-reachable.
**Fix in:** Deferrable — optionally validate both `WorkSize` and `ScratchSize` against the docker size grammar (digits + optional b/k/m/g) at config/Preflight time, reusing the existing `parseDockerMemory` parser, if operator-config hardening becomes a priority.

## TD-005 — ResolveAutoFixSandbox does not plumb WorkSize/ScratchSize from SandboxConfig (LOW)
**Origin:** Phase 2, task 2.5 GATE review, 2026-07-21
**File:** internal/verify/autofix_exec.go:72-90
**Issue:** `ResolveAutoFixSandbox` maps `Image`, `Memory`, `CPUs`, `PidsLimit`, `Timeout`, and `DockerPath` from operator `SandboxConfig` into `DockerConfig`, but not `WorkSize` (nor `ScratchSize`). So an operator can lower `Memory` via config yet cannot raise/lower the `/work` overlay tmpfs — the `Writable:true` overlay is pinned to the hardcoded default `WorkSize` (512m) regardless of the operator's memory budget. The new key is defaulted and back-compat, but not operator-reachable, which makes the TD-001 WorkSize-vs-Memory collision unresolvable through config.
**Why accepted:** No Phase 2 AC requires exposing `WorkSize` through `SandboxConfig`; the config-plumbing surface (`SandboxConfig` → `DockerConfig`) is outside `internal/sandbox` and outside this phase's Components Touched. Adding it now is speculative scope ahead of the Phase 3 auto-fix wiring that establishes the real caller.
**Fix in:** Couple with TD-001 at/after the Phase 3 gate — add a `WorkSize` (and optionally `ScratchSize`) field to `SandboxConfig` with the same `if sc.X != ""` override pattern already used for `Memory`/`CPUs`, OR explicitly document that overlay sizing is fixed and validated against `Memory`.
