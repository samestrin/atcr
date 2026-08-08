package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/log"
)

// osLevelBackendName identifies the backend in diagnostics and the evidence
// trail. It MUST stay equal to the `sandbox.fallback: os-level` config value
// (registry.SandboxFallbackOSLevel) — that string is how an operator connects
// what they configured to the backend named in a failure message.
const osLevelBackendName = "os-level"

// Platform sandboxing binaries. macOS ships sandbox-exec in the base system;
// Linux uses bubblewrap, which provides mount/network/PID namespace isolation
// in one tool (seccomp alone was considered and rejected during /refine-epic:
// it is a syscall filter with no filesystem or network isolation of its own).
const (
	darwinSandboxTool = "sandbox-exec"
	linuxSandboxTool  = "bwrap"
)

// osLevelToolFor maps a GOOS to its native sandboxing binary.
//
// Platform selection is a runtime switch rather than build-tagged files on
// purpose. .goreleaser.yaml ships windows/amd64 and windows/arm64 while CI runs
// on ubuntu-latest only, so a GOOS the build-tagged layout forgot would stay
// invisible until a release tag — and Phase 4 will wire NewOSLevelBackend into
// internal/verify and cli/, so that gap would then break their builds too. One
// file that compiles everywhere cannot have that hole, and taking the GOOS as a
// parameter makes every platform's decision assertable from any host.
//
// An unsupported platform fails closed: it returns an error rather than a
// best-effort passthrough. A backend that "succeeded" without a sandbox would
// run model-authored code with no containment at all.
func osLevelToolFor(goos string) (string, error) {
	switch goos {
	case "darwin":
		return darwinSandboxTool, nil
	case "linux":
		return linuxSandboxTool, nil
	default:
		return "", fmt.Errorf("sandbox: os-level sandbox is not supported on %s", goos)
	}
}

// OSLevelConfig parameterizes the OS-level backend. Zero values are not safe;
// use DefaultOSLevelConfig and override fields as needed.
type OSLevelConfig struct {
	// ToolPath is the platform sandboxing binary to invoke (sandbox-exec on
	// macOS, bwrap on Linux). Empty resolves the platform default on PATH;
	// tests inject a fake shim here, mirroring DockerConfig.DockerPath.
	ToolPath string
	// Timeout is the default per-run wall-clock budget when RunSpec.Timeout is 0.
	Timeout time.Duration
	// MaxOutputBytes truncates captured combined stdout+stderr.
	MaxOutputBytes int
	// MaxConcurrent bounds the number of sandboxed processes running at once
	// across this backend, mirroring DockerConfig.MaxConcurrent: a review
	// verifies findings concurrently and each skeptic may run many tools, so
	// without this cap a large finding set could exhaust the host.
	MaxConcurrent int
}

// DefaultOSLevelConfig returns a conservative default configuration. ToolPath is
// left empty so it resolves to the running platform's tool on PATH.
func DefaultOSLevelConfig() OSLevelConfig {
	return OSLevelConfig{
		Timeout:        60 * time.Second,
		MaxOutputBytes: 64 * 1024,
		MaxConcurrent:  4,
	}
}

// osLevelBackend runs commands and scripts under the platform's native process
// sandbox — sandbox-exec on macOS, bwrap on Linux — as a second Backend
// implementation alongside DockerBackend, for hosts without a Docker daemon.
//
// It is unexported so no sandbox-exec/bwrap-specific type crosses the Backend
// boundary; consumers hold it as a sandbox.Backend. NewOSLevelBackend returns
// the concrete type (mirroring NewDockerBackend) purely so in-package tests can
// reach cfg — see TD-002 if a cross-package assertion is ever needed.
type osLevelBackend struct {
	cfg OSLevelConfig
	// sem bounds concurrent sandbox spawns to cfg.MaxConcurrent (buffered slots).
	// NewOSLevelBackend allocates it; a struct-literal backend leaves it nil.
	sem chan struct{}
	// semOnce will lazily allocate sem on first Run so a struct-literal backend
	// (bypassing NewOSLevelBackend) still enforces the cap instead of failing
	// open, as DockerBackend does at docker.go:237-245. NOT YET WIRED: Run does
	// not acquire a slot until task 2.2, so today the cap is constructor-only.
	// Do not read this field as a live mitigation before then.
	semOnce sync.Once
}

var _ Backend = (*osLevelBackend)(nil)

// NewOSLevelBackend constructs an OS-level backend from cfg, flooring
// non-positive values to their defaults. It mirrors NewDockerBackend
// (docker.go:91-105): a caller must not have to pre-populate every field, and a
// zero or negative timeout would be an unbounded run — the same
// resource-exhaustion risk the Docker backend floors.
func NewOSLevelBackend(cfg OSLevelConfig) *osLevelBackend {
	def := DefaultOSLevelConfig()
	if cfg.Timeout <= 0 {
		cfg.Timeout = def.Timeout
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = def.MaxOutputBytes
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = def.MaxConcurrent
	}
	return &osLevelBackend{cfg: cfg, sem: make(chan struct{}, cfg.MaxConcurrent)}
}

// Name implements Backend. It returns a constant, so a struct-literal backend
// reports the same identifier as a constructed one.
func (b *osLevelBackend) Name() string { return osLevelBackendName }

// Timeout reports the backend's default per-run wall-clock budget — the value
// applied when RunSpec.Timeout is zero. Exposed for the same reason
// DockerBackend.Timeout is (docker.go:115): the value is a context deadline and
// never appears in the spawned argv, so a resolver's propagation of an
// operator's configured timeout cannot otherwise be asserted.
func (b *osLevelBackend) Timeout() time.Duration { return b.cfg.Timeout }

// Preflight implements Backend. Implemented in task 1.11.
func (b *osLevelBackend) Preflight(ctx context.Context) error {
	return errors.New("sandbox: os-level Preflight not implemented")
}

// osLevelRunArgs builds the argv for the platform sandboxing tool: the tool's
// own containment arguments, then the workload.
//
// Phase 1 scope: the containment arguments are empty. The deny-by-default
// sandbox-exec profile and the bwrap bind-mount/namespace argument list are
// generated in Phase 2 (oslevel_profile.go) and slot in here. Until they do,
// this backend provides NO containment — which is why nothing wires it into a
// resolver until Phase 4, and why Preflight refuses.
//
// Like dockerRunArgs, this is pure (no I/O) so the argv can be asserted in a
// unit test without either binary installed. The script body is NOT included
// here: it is streamed over stdin to `sh -s` by Run, never interpolated into
// argv, preserving RunSpec's injection-safety guarantee (sandbox.go:51-56).
func osLevelRunArgs(cfg OSLevelConfig, spec RunSpec) ([]string, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	var args []string
	if spec.Script != "" {
		return append(args, "/bin/sh", "-s"), nil
	}
	return append(args, spec.Command...), nil
}

// Run implements Backend. It reproduces DockerBackend.Run's three-way outcome
// taxonomy: a normal exit (zero or not) is a RunResult with a nil error, a
// timeout or parent cancellation is TimedOut/124 with a nil error, and a
// genuine backend fault is a wrapped error.
func (b *osLevelBackend) Run(ctx context.Context, spec RunSpec) (RunResult, error) {
	tool, err := b.toolPath()
	if err != nil {
		return RunResult{}, err
	}
	args, err := osLevelRunArgs(b.cfg, spec)
	if err != nil {
		return RunResult{}, err
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = b.cfg.Timeout
	}
	if timeout <= 0 {
		timeout = DefaultOSLevelConfig().Timeout
	}
	logger := log.FromContext(ctx)
	cmdStr := renderCommand(spec)
	logger.Info("sandbox exec start", "backend", osLevelBackendName, "command", cmdStr)

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, tool, args...)
	// Run the sandbox in its own process group so a timeout can kill the whole
	// tree. exec.CommandContext only signals the direct child; a workload that
	// forked (a build spawning a compiler, a test spawning a server) would
	// otherwise keep consuming host resources after the deadline — the same
	// leak DockerBackend closes by explicitly killing its container
	// (docker.go:305-311). On Linux bwrap's own PID namespace will reap the
	// tree as well; the process group makes the guarantee platform-independent
	// and covers the pre-namespace window.
	setProcessGroup(cmd)
	if spec.Script != "" {
		// The script is stdin DATA to `sh -s`, never argv and never inside a
		// `sh -c "..."` string, so the script body IS the program source rather
		// than an argument — there is no shell-injection vector.
		cmd.Stdin = strings.NewReader(spec.Script)
	}
	var buf bytes.Buffer
	// Cap the captured buffer so a chatty workload cannot exhaust host memory
	// before truncation, with headroom for rune-boundary backup.
	lw := &limitedWriter{w: &buf, n: int64(b.cfg.MaxOutputBytes) + 4096}
	cmd.Stdout = lw
	cmd.Stderr = lw

	if err := cmd.Start(); err != nil {
		logger.Error("sandbox exec backend fault", "backend", osLevelBackendName, "command", cmdStr, "error", err)
		return RunResult{}, fmt.Errorf("os-level sandbox run: %s: %w", tool, err)
	}
	runErr := b.waitOrKill(runCtx, cmd)

	res := RunResult{
		Command: cmdStr,
		Output:  truncate(buf.String(), b.cfg.MaxOutputBytes),
	}

	// A cancellation-class end (deadline exceeded OR parent cancellation) is
	// folded into TimedOut so it is never misreported as a spurious non-zero
	// program exit or a backend fault, mirroring docker.go:301.
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.Canceled) {
		res.TimedOut = true
		res.ExitCode = timeoutExitCode
		logger.Warn("sandbox exec timed out", "backend", osLevelBackendName, "command", cmdStr, "timeout", timeout)
		return res, nil
	}
	if runErr != nil {
		return b.classifyRunError(ctx, res, tool, cmdStr, runErr)
	}
	logger.Info("sandbox exec done", "backend", osLevelBackendName, "command", cmdStr, "exit_code", res.ExitCode)
	return res, nil
}

// waitOrKill waits for cmd, killing its whole process group if ctx ends first.
// exec.CommandContext's own Cancel would signal only the direct child, leaving
// a forked workload alive past the deadline.
func (b *osLevelBackend) waitOrKill(ctx context.Context, cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		b.killGroup(ctx, cmd)
		// Reap the child so it does not linger as a zombie; the Wait error is
		// discarded because the caller already classifies this as a timeout.
		<-done
		return ctx.Err()
	}
}

// killGroup SIGKILLs the command's process group, falling back to the single
// process if the group kill fails. A failure is logged rather than swallowed so
// an operator can detect an orphaned sandbox process (docker.go:308-310).
func (b *osLevelBackend) killGroup(ctx context.Context, cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if err := killProcessGroup(pid); err != nil {
		log.FromContext(ctx).Warn("sandbox exec kill-on-timeout failed",
			"backend", osLevelBackendName, "pid", pid, "error", err)
		if err := cmd.Process.Kill(); err != nil {
			log.FromContext(ctx).Warn("sandbox exec fallback kill failed",
				"backend", osLevelBackendName, "pid", pid, "error", err)
		}
	}
}

// classifyRunError maps a failed *exec.Cmd run onto the Backend.Run contract: a
// program exit becomes RunResult.ExitCode with a nil error, anything else is a
// wrapped backend fault. Platform-specific reserved exit codes are classified in
// task 1.8; this is the exit-vs-fault split only.
func (b *osLevelBackend) classifyRunError(ctx context.Context, res RunResult, tool, cmdStr string, runErr error) (RunResult, error) {
	logger := log.FromContext(ctx)
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		res.ExitCode = ee.ExitCode()
		logger.Info("sandbox exec done", "backend", osLevelBackendName, "command", cmdStr, "exit_code", res.ExitCode)
		return res, nil
	}
	// Not an exit status: spawn failure, binary vanished, I/O error — a fault.
	logger.Error("sandbox exec backend fault", "backend", osLevelBackendName, "command", cmdStr, "error", runErr)
	return res, fmt.Errorf("os-level sandbox run: %s: %w", tool, runErr)
}

// toolPath resolves the binary this backend invokes: an explicit cfg.ToolPath
// override wins, otherwise the running platform's tool.
//
// The override is both the test-shim seam and the field an operator config
// would populate, so it is the seam through which containment can be replaced
// by a no-op. An override must therefore be an absolute path: a relative or
// bare name would be resolved through $PATH at spawn time, letting any
// user-writable $PATH entry shadow the real sandbox binary. Existence and
// executability are Preflight's checks (task 1.11) — this function is pure so
// it can be asserted without touching the filesystem.
//
// The platform default is still returned as a bare name here and resolved by
// exec; pinning it to an absolute path via exec.LookPath is TD-003, scheduled
// for Preflight where an I/O check is already in scope.
func (b *osLevelBackend) toolPath() (string, error) {
	if b.cfg.ToolPath == "" {
		return osLevelToolFor(runtime.GOOS)
	}
	if !filepath.IsAbs(b.cfg.ToolPath) {
		return "", fmt.Errorf("sandbox: os-level tool_path must be absolute, got %q", b.cfg.ToolPath)
	}
	return b.cfg.ToolPath, nil
}
