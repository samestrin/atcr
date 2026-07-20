# Sandbox Testing Patterns (fakeDocker shim, argv assertions) `[IMPORTANT]`

## Overview

The `internal/sandbox` package tests Docker-backed sandbox execution without requiring a real Docker daemon in CI or on developer machines. It does this by writing a small POSIX shell script that impersonates the `docker` CLI binary, then pointing the backend's `DockerPath` config at that script instead of the real `docker` executable. This lets tests exercise `Run`/`Preflight` code paths deterministically — including error conditions like specific exit codes or host resource constraints — by controlling the fake script's behavior through environment variables and its body content.

A second, complementary pattern in this package is argv assertion: rather than executing a container, tests build the `docker run` argument list via `dockerRunArgs` and assert on substrings within the joined argv to verify security hardening flags (network isolation, read-only rootfs, dropped capabilities, resource caps) are present without ever invoking Docker at all.

> Source: codebase-discovery.json `files_to_create` and `reusable_components` ("fakeDocker test shim") — the fakeDocker shim shown in `sandbox_test.go`/`docker_test.go` (and already used in `internal/verify/exec_test.go`) is the pattern to reuse for the planned `internal/verify/autofix_exec_test.go` resolver tests.

## Key Concepts

### 1. The fakeDocker shell shim

`writeFakeDocker` writes an executable shell script to a `t.TempDir()` that impersonates the `docker` CLI. It skips on Windows since the shim is POSIX-only shell. The script's behavior is driven by the first non-flag argument it sees after `"run"`/`"image"`/`"version"` and by env vars the test sets (e.g. `DOCKER_EXIT_CODE`).

> Source: internal/sandbox/sandbox_test.go:writeFakeDocker

### 2. Argv assertions via `dockerRunArgs`

Instead of running a container, `TestDockerRunArgs_HardeningFlagsPresent` calls `dockerRunArgs(cfg, spec)` directly and asserts on substrings of the joined argument list — verifying hardening flags like `--network none`, `--read-only`, `--cap-drop ALL`, `--security-opt no-new-privileges`, non-root `--user`, resource caps (`--memory`, `--cpus`, `--pids-limit`), the read-only snapshot mount, tmpfs scratch space, and the `run --rm` ephemeral-container flag — all without a daemon.

> Source: internal/sandbox/sandbox_test.go:TestDockerRunArgs_HardeningFlagsPresent

### 3. Exercising runtime exit codes through the shim

`TestDockerBackendRun_RuntimeExitCodesAreBackendErrors` uses `writeFakeDocker(t, fakeDockerExitBody())` combined with `t.Setenv("DOCKER_EXIT_CODE", ...)` to drive the fake docker binary through exit codes 125, 126, and 127, then asserts `b.Run` returns an error whose message contains "runtime error" for each.

> Source: internal/sandbox/docker_test.go:TestDockerBackendRun_RuntimeExitCodesAreBackendErrors

### 4. Exercising host preflight constraints through the shim

`TestDockerBackend_Preflight_RejectsMemoryExceedingHost` uses `writeFakeDocker(t, fakeDockerInfoBody(1<<30, 2))` to simulate a host reporting 1 GiB of memory and 2 CPUs, then configures `cfg.Memory = "2g"` (exceeding host capacity) and asserts `b.Preflight(ctx)` returns an error mentioning "memory".

> Source: internal/sandbox/docker_test.go:TestDockerBackend_Preflight_RejectsMemoryExceedingHost

### 5. `RunSpec.validate()` table-driven cases

`TestRunSpec_Validate` is a table-driven test covering valid/invalid `RunSpec` combinations: command-only and script-only are valid; neither or both set is invalid; missing snapshot dir is invalid; relative snapshot dir is invalid; and a snapshot dir containing a colon (mount-injection risk, e.g. `/s:ro,z`) is invalid.

> Source: internal/sandbox/sandbox_test.go:TestRunSpec_Validate

## Code Examples

```go
// writeFakeDocker writes an executable shell script that impersonates the docker
// CLI so Run/Preflight can be exercised deterministically without a real daemon.
// The script's behavior is driven by the first non-flag argument it sees after
// "run"/"image"/"version" and by env vars the test sets.
func writeFakeDocker(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-docker shell shim is POSIX-only")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "docker")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755))
	return p
}
```

```go
func TestDockerRunArgs_HardeningFlagsPresent(t *testing.T) {
	cfg := DefaultDockerConfig()
	spec := RunSpec{Command: []string{"go", "test", "./..."}, SnapshotDir: "/tmp/snap"}

	args, err := dockerRunArgs(cfg, spec)
	require.NoError(t, err)
	joined := strings.Join(args, " ")

	// Network isolation, read-only rootfs, dropped caps, no-new-privileges.
	assert.Contains(t, joined, "--network none", "must isolate network")
	assert.Contains(t, joined, "--read-only", "rootfs must be read-only")
	assert.Contains(t, joined, "--cap-drop ALL", "all capabilities must be dropped")
	assert.Contains(t, joined, "--security-opt no-new-privileges")
	// Non-root.
	assert.Contains(t, joined, "--user "+cfg.User)
	// Resource caps.
	assert.Contains(t, joined, "--memory "+cfg.Memory)
	assert.Contains(t, joined, "--cpus "+cfg.CPUs)
	assert.Contains(t, joined, "--pids-limit")
	// Snapshot mounted READ-ONLY at /work.
	assert.Contains(t, joined, "/tmp/snap:/work:ro", "snapshot must be a read-only mount")
	// Writable scratch is a tmpfs, not a host mount.
	assert.Contains(t, joined, "--tmpfs /scratch")
	// Ephemeral container.
	assert.Contains(t, joined, "run --rm")
	// The argv command is passed through verbatim after the image.
	assert.Contains(t, joined, "go test ./...")
}
```

```go
func TestRunSpec_Validate(t *testing.T) {
	cases := []struct {
		name string
		spec RunSpec
		ok   bool
	}{
		{"command only", RunSpec{Command: []string{"true"}, SnapshotDir: "/s"}, true},
		{"script only", RunSpec{Script: "true", SnapshotDir: "/s"}, true},
		{"neither", RunSpec{SnapshotDir: "/s"}, false},
		{"both", RunSpec{Command: []string{"true"}, Script: "true", SnapshotDir: "/s"}, false},
		{"no snapshot", RunSpec{Command: []string{"true"}}, false},
		{"relative snapshot", RunSpec{Command: []string{"true"}, SnapshotDir: "rel/dir"}, false},
		{"colon in snapshot (mount injection)", RunSpec{Command: []string{"true"}, SnapshotDir: "/s:ro,z"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.validate()
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
```

```go
func TestDockerBackendRun_RuntimeExitCodesAreBackendErrors(t *testing.T) {
	fakeDocker := writeFakeDocker(t, fakeDockerExitBody())
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fakeDocker
	cfg.MaxConcurrent = 1
	b := NewDockerBackend(cfg)

	spec := RunSpec{
		Command:     []string{"true"},
		SnapshotDir: t.TempDir(),
	}

	for _, code := range []int{125, 126, 127} {
		t.Run(fmt.Sprintf("exit-%d", code), func(t *testing.T) {
			t.Setenv("DOCKER_EXIT_CODE", fmt.Sprintf("%d", code))
			_, err := b.Run(context.Background(), spec)
			if err == nil {
				t.Fatalf("exit %d: expected backend error, got nil", code)
			}
			if !strings.Contains(err.Error(), "runtime error") {
				t.Fatalf("exit %d: expected error to mention runtime error, got %q", code, err.Error())
			}
		})
	}
}
```

```go
func TestDockerBackend_Preflight_RejectsMemoryExceedingHost(t *testing.T) {
	fake := writeFakeDocker(t, fakeDockerInfoBody(1<<30, 2)) // 1 GiB, 2 CPUs
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	cfg.Memory = "2g" // exceeds the 1 GiB host
	b := NewDockerBackend(cfg)
	err := b.Preflight(context.Background())
	require.Error(t, err, "configured memory above host MemTotal must fail preflight")
	assert.Contains(t, err.Error(), "memory")
}
```

## Quick Reference

Naming convention: `*_test.go`, `TestXxx_YyyReturnsZzz`.

> Source: Codebase Patterns (test_patterns) — naming_convention "*_test.go, TestXxx_YyyReturnsZzz"

| Test file | Covers |
|---|---|
| `internal/verify/exec_test.go` | Resolver shape (mirrors `ResolveExecBackend`) plus the `fakeDocker` shim: "Writes a POSIX shell script to a temp dir and injects it as `DockerConfig.DockerPath`, so resolver/preflight tests run hermetically without a docker daemon (skips on Windows)." |
| `internal/sandbox/sandbox_test.go` | `dockerRunArgs` argv assertions without a daemon (`TestDockerRunArgs_HardeningFlagsPresent`), the `writeFakeDocker` shim itself, and `RunSpec.validate()` table cases (`TestRunSpec_Validate`) |
| `internal/sandbox/docker_test.go` | Backend `Run`/`Preflight` behavior via the fakeDocker shim — runtime exit-code-to-backend-error mapping (`TestDockerBackendRun_RuntimeExitCodesAreBackendErrors`) and host resource preflight rejection (`TestDockerBackend_Preflight_RejectsMemoryExceedingHost`) |
| `internal/verify/localvalidate_test.go` | Pins the `RunConfiguredValidation` / `ValidationResult` host-path contract (`Passed()`, `TimedOut`, `StartError`, truncation) — defines what "no behavior change" means for the host path the sandboxed variant must preserve |
| `cmd/atcr/autofix_test.go` | Gate/orchestration with call-recording fakes |
| `internal/verify/autofix_exec_test.go` (planned, files_to_create) | Resolver tests mirroring `exec_test.go`'s shape: refuses-without-backend, builds-and-preflights via the fakeDocker shim, no-sandbox is a no-op |

> Source: Codebase Patterns (test_patterns) — test_location "internal/verify/, internal/sandbox/, cmd/atcr/"; example_test "internal/verify/exec_test.go (resolver shape + fakeDocker shim); internal/sandbox/sandbox_test.go (dockerRunArgs argv assertions without a daemon); cmd/atcr/autofix_test.go (gate/orchestration with call-recording fakes)"; files_to_create "internal/verify/autofix_exec_test.go (or similar), purpose: Resolver tests mirroring exec_test.go's shape (refuses-without-backend, builds-and-preflights via the fakeDocker shim, no-sandbox is a no-op)."

## Related Documentation

- [../plan.md](../plan.md) — plan goal: route the `--auto-fix` pipeline's post-apply validation step through `internal/sandbox` container isolation via a new resolver mirroring `ResolveExecBackend`, with its own test file.
- `sandbox-backend-interface.md` — related category file in this same `documentation/` directory covering the sandbox backend interface.
- `docker-backend-implementation.md` — related category file in this same `documentation/` directory covering the Docker backend implementation.
- `resolver-pattern-resolveexecbackend.md` — related category file in this same `documentation/` directory covering the `ResolveExecBackend` resolver pattern being mirrored for the new autofix resolver.
- `autofix-validation-contract.md` — related category file in this same `documentation/` directory covering the `ValidationResult` host-path contract that `internal/verify/localvalidate_test.go` pins.
