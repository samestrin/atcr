package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
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
	// Writable asks for an ephemeral copy-on-write overlay so the snapshot stays
	// read-only while the run still gets a writable tree (sandbox.go:41-60). The
	// OS-level backend has no overlay mechanism yet, and silently ignoring the
	// flag would run the workload directly against the operator's snapshot — the
	// opposite of what the caller asked for. Refuse instead.
	if spec.Writable {
		return RunResult{}, fmt.Errorf("os-level sandbox run: RunSpec.Writable is not supported by the %s backend", osLevelBackendName)
	}

	// Bound concurrent sandbox spawns; block until a slot frees or ctx is done.
	// Lazily allocate the semaphore so a struct-literal backend (nil sem) still
	// enforces MaxConcurrent rather than silently spawning without limit,
	// mirroring docker.go:237-251.
	b.semOnce.Do(func() {
		if b.sem == nil {
			n := b.cfg.MaxConcurrent
			if n <= 0 {
				n = DefaultOSLevelConfig().MaxConcurrent
			}
			b.sem = make(chan struct{}, n)
		}
	})
	select {
	case b.sem <- struct{}{}:
		defer func() { <-b.sem }()
	case <-ctx.Done():
		return RunResult{}, ctx.Err()
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
	// Run inside the snapshot. RunSpec.validate already requires SnapshotDir to
	// be absolute, and the Docker backend mounts it as the working directory —
	// leaving cmd.Dir unset here would instead run model-authored code in atcr's
	// own working directory, i.e. the operator's live repo.
	cmd.Dir = spec.SnapshotDir
	// Hand the workload an explicit, minimal environment. Inheriting os.Environ()
	// would pass every credential in the operator's shell (LITELLM_API_KEY,
	// GITHUB_TOKEN, cloud tokens) straight to LLM-generated code. Docker gets
	// this for free from a fresh container plus an -e allowlist (docker.go:165-169);
	// neither sandbox-exec nor bwrap scrubs the environment on its own, so the
	// allowlist has to be built here. Phase 2 adds bwrap's --clearenv on top.
	cmd.Env = sandboxEnv(spec.SnapshotDir)
	// Put the sandbox in its own process group and make cancellation kill the
	// whole group, so a workload that forked is reaped rather than left running
	// past the deadline. WaitDelay is the backstop for a grandchild that escaped
	// the group and still holds the output pipes: without it cmd.Wait would block
	// forever on an already-expired deadline. Both mirror the same pattern in
	// internal/verify/localvalidate.go:100-105.
	configureSandboxProcessGroup(cmd)
	cmd.WaitDelay = osLevelWaitGrace
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

	runErr := cmd.Run()
	res := RunResult{
		Command: cmdStr,
		Output:  truncate(buf.String(), b.cfg.MaxOutputBytes),
	}

	// A cancellation-class end (deadline exceeded OR parent cancellation) is
	// folded into TimedOut so it is never misreported as a spurious non-zero
	// program exit or a backend fault, mirroring docker.go:301. runErr is
	// consulted first so a run that genuinely finished microseconds before the
	// deadline keeps its real exit code instead of being relabelled a timeout.
	if runCtxDone(runCtx) && !ranToCompletion(runErr) {
		res.TimedOut = true
		res.ExitCode = timeoutExitCode
		// A non-nil, non-ESRCH error on this path means the group kill itself
		// failed (e.g. EPERM because a group member dropped privileges). Log it
		// so a failed reap stays observable instead of being masked by TimedOut.
		if runErr != nil && !errors.Is(runErr, syscall.ESRCH) && !errors.Is(runErr, context.DeadlineExceeded) && !errors.Is(runErr, context.Canceled) {
			logger.Warn("sandbox exec kill-on-timeout returned unexpected error",
				"backend", osLevelBackendName, "command", cmdStr, "error", runErr)
		}
		logger.Warn("sandbox exec timed out", "backend", osLevelBackendName, "command", cmdStr, "timeout", timeout)
		return res, nil
	}
	if errors.Is(runErr, exec.ErrWaitDelay) {
		// The workload exited but a lingering child held its output pipes open
		// past the wait grace: completed uncleanly. Fail closed as a timeout
		// rather than reporting a success we cannot vouch for
		// (localvalidate.go:147-153).
		res.TimedOut = true
		res.ExitCode = timeoutExitCode
		logger.Warn("sandbox exec exceeded wait grace", "backend", osLevelBackendName, "command", cmdStr)
		return res, nil
	}
	if runErr != nil {
		return b.classifyRunError(ctx, res, tool, cmdStr, runErr)
	}
	logger.Info("sandbox exec done", "backend", osLevelBackendName, "command", cmdStr, "exit_code", res.ExitCode)
	return res, nil
}

// osLevelWaitGrace bounds how long cmd.Wait blocks after the sandboxed process
// exits while a lingering grandchild still holds its output pipes open.
const osLevelWaitGrace = 5 * time.Second

// runCtxDone reports whether the run's context ended in a cancellation class
// (deadline or explicit cancel).
func runCtxDone(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled)
}

// ranToCompletion reports whether the process delivered a real exit status —
// the case where a run finished just before its deadline elapsed and must keep
// its exit code rather than being relabelled a timeout.
func ranToCompletion(runErr error) bool {
	if runErr == nil {
		return true
	}
	var ee *exec.ExitError
	return errors.As(runErr, &ee) && ee.ProcessState != nil && ee.Exited()
}

// sandboxEnv builds the minimal environment handed to a sandboxed workload. It
// is an allowlist, not a filter: anything not named here never reaches
// model-authored code. HOME/TMPDIR and the Go/XDG cache vars point at the
// snapshot so toolchains that must write have somewhere to do it, mirroring the
// intent of docker.go:165-169.
func sandboxEnv(snapshotDir string) []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin:/usr/sbin:/sbin"
	}
	return []string{
		"PATH=" + path,
		"HOME=" + snapshotDir,
		"TMPDIR=" + snapshotDir,
		"XDG_CACHE_HOME=" + filepath.Join(snapshotDir, ".cache"),
		"GOCACHE=" + filepath.Join(snapshotDir, ".gocache"),
		"GOTMPDIR=" + snapshotDir,
		"LANG=C",
	}
}

// bwrap reserved exit codes. Bubblewrap exits 125 when bubblewrap itself fails,
// 126 when the command cannot be executed, and 127 when it does not exist —
// none of which is the workload's own status.
//
// These are encoded as named constants rather than inlined so the Phase 3
// integration leg, which is the first thing in this sprint to run a real bwrap,
// has one obvious place to correct them if the real binary disagrees.
const (
	bwrapFaultExitCode      = 125
	bwrapCannotExecExitCode = 126
	bwrapNotFoundExitCode   = 127
)

// sandboxExecDiagnosticPrefix is how macOS sandbox-exec identifies its own
// failures on stderr.
const sandboxExecDiagnosticPrefix = "sandbox-exec:"

// classifyToolExit reports whether a non-zero exit came from the sandboxing tool
// itself rather than from the workload, and why.
//
// The two platforms need different rules, and the difference is the reason this
// function exists rather than a shared numeric table:
//
//   - Linux/bwrap has a Docker-like reserved-code convention (125/126/127), so
//     classification is a pure exit-code test.
//   - macOS/sandbox-exec has NO such convention: it execs the child and returns
//     the child's own status, so exit 1 is genuinely ambiguous between "the
//     workload failed" and "sandbox-exec refused to apply the profile". The only
//     signal it gives is the diagnostic it prints, so that is what is matched.
//     This is deliberately a weaker test than Linux's, and it is the reason AC
//     01-03 asks for the classification boundary to be written down rather than
//     inferred: a workload that itself prints a line starting with
//     "sandbox-exec:" would be misclassified as a fault. That direction is the
//     safe one — a fault is refused, a missed fault would be reported as a
//     successful contained run.
//
// Any platform without a rule fails closed: an unclassifiable non-zero exit is
// treated as a fault, never as a successful run, per the package's
// containment-first contract (sandbox.go:5-14).
func classifyToolExit(goos string, exitCode int, output string) (bool, string) {
	if exitCode == 0 {
		return false, ""
	}
	switch goos {
	case "linux":
		switch exitCode {
		case bwrapFaultExitCode:
			return true, fmt.Sprintf("%s failed to set up the sandbox (exit %d)", linuxSandboxTool, exitCode)
		case bwrapCannotExecExitCode:
			return true, fmt.Sprintf("%s could not execute the command (exit %d)", linuxSandboxTool, exitCode)
		case bwrapNotFoundExitCode:
			return true, fmt.Sprintf("%s could not find the command (exit %d)", linuxSandboxTool, exitCode)
		}
		return false, ""
	case "darwin":
		if strings.Contains(output, sandboxExecDiagnosticPrefix) {
			return true, fmt.Sprintf("%s reported a sandbox error (exit %d)", darwinSandboxTool, exitCode)
		}
		return false, ""
	default:
		return true, fmt.Sprintf("no exit-code classification rule for %s; treating exit %d as a sandbox fault", goos, exitCode)
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
		// A signal death (OOM killer, an external SIGKILL, and once Phase 2 lands
		// a sandbox-profile kill) originates from the kernel, not the workload,
		// and ExitCode() reports -1 for it. Reporting -1 as a program result
		// would present an impossible exit status as a real one, so route it to
		// a fault exactly as the Docker backend routes 128+N (docker.go:323-326).
		code := ee.ExitCode()
		if code < 0 {
			logger.Error("sandbox exec killed by signal", "backend", osLevelBackendName, "command", cmdStr, "error", runErr)
			return res, fmt.Errorf("os-level sandbox run: %s: process killed by signal (%s): %w", tool, ee.String(), runErr)
		}
		// The sandboxing tool's own failures must not be folded into ExitCode,
		// where a caller would read them as the workload's result.
		if isFault, reason := classifyToolExit(runtime.GOOS, code, res.Output); isFault {
			logger.Error("sandbox exec runtime error", "backend", osLevelBackendName, "command", cmdStr, "exit_code", code, "error", runErr)
			return res, fmt.Errorf("os-level sandbox run: %s: runtime error: %s: %w", tool, reason, runErr)
		}
		res.ExitCode = code
		logger.Info("sandbox exec done", "backend", osLevelBackendName, "command", cmdStr, "exit_code", res.ExitCode)
		return res, nil
	}
	// Not an exit status: spawn failure, binary vanished, I/O error — a fault.
	logger.Error("sandbox exec backend fault", "backend", osLevelBackendName, "command", cmdStr, "error", runErr)
	return res, fmt.Errorf("os-level sandbox run: %s: %w", tool, runErr)
}

// toolPath resolves the binary this backend invokes.
//
// The platform gate runs FIRST and unconditionally: an unsupported GOOS is
// refused even when cfg.ToolPath is set. Applying the override before the gate
// would let an operator's tool_path re-enable the backend on, say, Windows,
// where setProcessGroup is a no-op and killProcessGroup always fails — a
// spawned workload with neither containment nor a reliable reap. The override
// selects WHICH binary runs on a supported platform; it is not a way to declare
// a platform supported.
//
// The override is both the test-shim seam and the field an operator config
// would populate, so it is the seam through which containment could be replaced
// by a no-op. It must therefore be an absolute path: a relative or bare name
// would be resolved through $PATH at spawn time, letting any user-writable
// $PATH entry shadow the real sandbox binary. Existence and executability are
// Preflight's checks (task 1.11) — this function stays pure so it can be
// asserted without touching the filesystem.
//
// The platform default is still returned as a bare name here and resolved by
// exec; pinning it to an absolute path via exec.LookPath is TD-003, scheduled
// for Preflight where an I/O check is already in scope.
func (b *osLevelBackend) toolPath() (string, error) {
	platformTool, err := osLevelToolFor(runtime.GOOS)
	if err != nil {
		return "", err
	}
	if b.cfg.ToolPath == "" {
		return platformTool, nil
	}
	if !filepath.IsAbs(b.cfg.ToolPath) {
		return "", fmt.Errorf("sandbox: os-level tool_path must be absolute, got %q", b.cfg.ToolPath)
	}
	return b.cfg.ToolPath, nil
}
