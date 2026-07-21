# Tech Debt Captured — Sprint 32.3 (sandbox ephemeral copy overlay)

Captured during `/execute-sprint`. Read by `/execute-code-review` (pre-seeded, `SOURCE=execute-sprint`).

## TD-001 — WorkSize default (512m) equals the Memory cap (MEDIUM)
**Origin:** Phase 1, task 1.5 GATE review, 2026-07-21
**File:** internal/sandbox/docker.go:71
**Issue:** The default `WorkSize` "512m" equals `DockerConfig.Memory` "512m". A Docker `--tmpfs` counts against the container memory cgroup, so once the mount is wired, seeding `/work` via `cp -a` a full source tree (plus the 64m `/scratch` tmpfs) draws from the same 512m the workload needs — the writable overlay could OOM-kill the run before it does useful work.
**Why accepted:** Phase 1 only declares the field and default; no mount consumes `WorkSize` yet, so there is no runtime OOM surface in this phase. The size-vs-memory interaction is properly a Phase 2 (mount branch) concern, where the `--auto-fix` caller's actual `Memory` cap is known and the two can be reconciled together.
**Fix in:** Phase 2 (Conditional Writable /work Mount) — reconcile `WorkSize` against `Memory` (size it below the cap, or raise `Memory` when `Writable:true`), and document that a tmpfs size counts against `--memory`.

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
