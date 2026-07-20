# DockerBackend Implementation (Run, Preflight, dockerRunArgs) `[CRITICAL]`

## Overview

`DockerBackend` (internal/sandbox/docker.go) is the sole `Backend` implementation used to run commands and scripts inside an ephemeral, network-isolated, resource-capped, non-root container with the snapshot mounted read-only.

> Source: internal/sandbox/docker.go — package doc comment for `DockerBackend`: "DockerBackend runs commands and scripts inside an ephemeral, network-isolated, resource-capped, non-root container with the snapshot mounted read-only."

The backend is configured via `DockerConfig`, whose zero value is not safe to use directly — callers should start from `DefaultDockerConfig()` and override only the fields they need. `NewDockerBackend` fills in any remaining unset fields (`DockerPath`, `MaxOutputBytes`, `Timeout`, `MaxConcurrent`) before returning a ready `*DockerBackend`, and allocates a semaphore channel (`sem`) sized to `cfg.MaxConcurrent` to bound the number of containers running at once across the backend.

> Source: internal/sandbox/docker.go — `DockerConfig` doc comment: "DockerConfig parameterizes the Docker backend. Zero values are not safe; use DefaultDockerConfig and override fields as needed."

The container invocation itself is built by the standalone `dockerRunArgs(cfg DockerConfig, spec RunSpec) ([]string, error)` function, which is also reused by `Preflight` to validate the real execution path end-to-end (not just a simplified probe), per the plan's grounded codebase-discovery note.

## Key Concepts

- **`DockerConfig` fields govern isolation and resource caps.** Fields include `DockerPath` (docker binary to invoke, resolved on PATH by default, injectable for test shims), `Image` (base image; "must already be present locally (Preflight verifies this) and must carry whatever toolchain run_tests needs"), `Memory`/`CPUs`/`PidsLimit` (resource caps mapped to `docker --memory`/`--cpus`/`--pids-limit`), `User` (non-root uid:gid via `docker --user`), `ScratchSize` (bounds the writable tmpfs scratch overlay via `docker --tmpfs size`), `Timeout` (default per-run wall-clock budget when `RunSpec.Timeout` is 0), `MaxOutputBytes` (truncates captured combined stdout+stderr), and `MaxConcurrent` (bounds concurrent containers across the backend).
  > Source: internal/sandbox/docker.go:`DockerConfig` struct field comments.

- **`dockerRunArgs` builds the full `docker run` argv for a `RunSpec`.** It first calls `spec.validate()` and returns early on error. It then assembles isolation and resource flags: `--rm`, `--network none`, `--read-only`, `--cap-drop ALL`, `--security-opt no-new-privileges`, `--user <cfg.User>`, `--memory <cfg.Memory>`, `--cpus <cfg.CPUs>`, `--pids-limit <cfg.PidsLimit>`, a writable tmpfs at `/scratch` sized from `cfg.ScratchSize`, environment redirection (`HOME`, `TMPDIR`, `XDG_CACHE_HOME`, `GOCACHE`, `GOTMPDIR` all pointed into `/scratch`), `--workdir /work`, and a read-only bind mount `-v <spec.SnapshotDir>:/work:ro`. If `spec.Script` is set it appends `-i <cfg.Image> /bin/sh -s` (script piped over stdin); otherwise it appends the image followed by `spec.Command...`.
  > Source: internal/sandbox/docker.go:103-143 (`dockerRunArgs` function body).

- **The snapshot mount is hardcoded read-only.** `RunSpec` has no mount-mode option — the `-v spec.SnapshotDir:/work:ro` flag in `dockerRunArgs` is fixed. This is the integration point a writable-mount mode would need to touch if a design requires in-tree writes (e.g., coverage output, codegen).
  > Source: internal/sandbox/docker.go:133 (per plan's codebase-discovery integration-gap note, grounded in the `-v spec.SnapshotDir + ":/work:ro"` line of `dockerRunArgs`).

- **Go build-cache environment variables already work against the read-only mount.** Because `HOME`, `TMPDIR`, `XDG_CACHE_HOME`, `GOCACHE`, and `GOTMPDIR` are all redirected into the writable `/scratch` tmpfs by `dockerRunArgs`, commands like `go build ./...` succeed against the read-only `/work` tree without additional configuration. Commands that instead write output files INTO the tree itself (e.g., `go test -coverprofile=...`, codegen, `--fix` lint modes) remain the cases not covered by this default.
  > Source: internal/sandbox/docker.go:127-131 (`-e HOME=/scratch`, `-e TMPDIR=/scratch`, `-e XDG_CACHE_HOME=/scratch/.cache`, `-e GOCACHE=/scratch/.gocache`, `-e GOTMPDIR=/scratch`), per plan's architecture note.

- **`Run` treats Docker runtime failures as backend faults, not program results.** Per `Run`'s implementation comments: it spawns `docker run`, tracks a named container so a timeout can `docker kill` it directly, treats docker exit codes 125-127 and signal deaths (128+N) as backend faults/Go errors (rather than as the executed program's own exit status), reports a timeout as `TimedOut` with the conventional exit code 124, and bounds concurrent spawns with a semaphore sized to `cfg.MaxConcurrent` — lazily allocated via `semOnce` so a struct-literal-constructed backend still enforces the cap.
  > Source: internal/sandbox/docker.go:154-265 (`Run` body — named-container timeout kill, exit-code taxonomy, `semOnce` semaphore; the doc comment itself is only `// Run implements Backend.`).

- **`Preflight` validates the real execution path, not a simplified stand-in.** Per its doc comment, it verifies, in order: the docker binary is runnable, the daemon is reachable, the base image is present, and a trivial network-isolated container runs to completion. The body adds a resource-cap check between the image check and the trivial run (step 2.5, `validateHostCaps`): the configured memory/CPU caps must fit the host the daemon reports (via `docker info` `MemTotal`/`NCPU`), and the trivial container runs using the SAME `dockerRunArgs` as real executions.
  > Source: internal/sandbox/docker.go:267-305 (`Preflight` doc comment and body) and internal/sandbox/docker.go:335-358 (`validateHostCaps`).

## Code Examples

Isolation and resource flags assembled by `dockerRunArgs` (internal/sandbox/docker.go:103-131):

```go
args := []string{
	"run", "--rm",
	"--network", "none",
	"--read-only",
	"--cap-drop", "ALL",
	"--security-opt", "no-new-privileges",
	"--user", cfg.User,
	"--memory", cfg.Memory,
	"--cpus", cfg.CPUs,
	"--pids-limit", strconv.Itoa(cfg.PidsLimit),
	"--tmpfs", "/scratch:rw,exec,size=" + cfg.ScratchSize,
	"-e", "HOME=/scratch",
	"-e", "TMPDIR=/scratch",
	"-e", "XDG_CACHE_HOME=/scratch/.cache",
	"-e", "GOCACHE=/scratch/.gocache",
	"-e", "GOTMPDIR=/scratch",
	"--workdir", "/work",
	"-v", spec.SnapshotDir + ":/work:ro",
}
```

Command vs. script dispatch (internal/sandbox/docker.go:135-141):

```go
if spec.Script != "" {
	args = append(args, "-i", cfg.Image, "/bin/sh", "-s")
} else {
	args = append(args, cfg.Image)
	args = append(args, spec.Command...)
}
```

`DefaultDockerConfig` (internal/sandbox/docker.go):

```go
func DefaultDockerConfig() DockerConfig {
	return DockerConfig{
		DockerPath:     "docker",
		Image:          "alpine:3.20",
		Memory:         "512m",
		CPUs:           "1.0",
		PidsLimit:      256,
		User:           "65534:65534", // nobody:nogroup
		ScratchSize:    "64m",
		Timeout:        60 * time.Second,
		MaxOutputBytes: 64 * 1024,
		MaxConcurrent:  4,
	}
}
```

## Quick Reference

`DockerConfig` fields and their `DefaultDockerConfig()` values:

| Field | Default (`DefaultDockerConfig`) | Purpose |
|---|---|---|
| `DockerPath` | `"docker"` | Docker binary invoked (resolved on PATH; test shims inject a fake path here) |
| `Image` | `"alpine:3.20"` | Base image runs execute in; must already be present locally (`Preflight` verifies this) |
| `Memory` | `"512m"` | Container memory cap (`docker --memory`) |
| `CPUs` | `"1.0"` | Container CPU cap (`docker --cpus`) |
| `PidsLimit` | `256` | Process count cap (`docker --pids-limit`) |
| `User` | `"65534:65534"` (nobody:nogroup) | Non-root uid:gid the container runs as (`docker --user`) |
| `ScratchSize` | `"64m"` | Size bound for the writable `/scratch` tmpfs overlay (`docker --tmpfs size`) |
| `Timeout` | `60 * time.Second` | Default per-run wall-clock budget when `RunSpec.Timeout` is 0 |
| `MaxOutputBytes` | `64 * 1024` | Truncation limit for captured combined stdout+stderr |
| `MaxConcurrent` | `4` | Max containers running at once across the backend (sizes the `sem` channel) |

## Related Documentation

- Plan overview: [../plan.md](../plan.md)
- Related category file in this same `documentation/` directory: `sandbox-backend-interface.md` — covers the `Backend` interface and `RunSpec` that `DockerBackend` implements and consumes.
- Related category file in this same `documentation/` directory: `resolver-pattern-resolveexecbackend.md` — covers how the `--auto-fix` pipeline resolves and selects a `Backend` (e.g., `DockerBackend`) at runtime, including the `--no-sandbox` opt-out path.
- Related category file in this same `documentation/` directory: `autofix-validation-contract.md` — covers the `verify.ValidationResult` contract the sandboxed `Run` result must be translated back into for the auto-fix path.
