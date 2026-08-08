package sandbox

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
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
// invisible until a release tag — and internal/verify and cli/ reference
// NewOSLevelBackend, so that gap would break their builds too. One file that
// compiles everywhere cannot have that hole, and taking the GOOS as a parameter
// makes every platform's decision assertable from any host.
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
// It is unexported: callers reach it through the Backend interface returned by
// NewOSLevelBackend, so no sandbox-exec/bwrap-specific type ever crosses the
// interface boundary.
type osLevelBackend struct {
	cfg OSLevelConfig
	// sem bounds concurrent sandbox spawns to cfg.MaxConcurrent (buffered slots).
	sem chan struct{}
	// semOnce lazily allocates sem on first Run so a backend built as a struct
	// literal (bypassing NewOSLevelBackend) still enforces the cap instead of
	// failing open. Safe under concurrent Run.
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

// Run implements Backend. Implemented in task 1.5.
func (b *osLevelBackend) Run(ctx context.Context, spec RunSpec) (RunResult, error) {
	return RunResult{}, errors.New("sandbox: os-level Run not implemented")
}

// toolPath resolves the binary this backend invokes: an explicit cfg.ToolPath
// override (the test-shim seam) wins, otherwise the running platform's tool.
func (b *osLevelBackend) toolPath() (string, error) {
	if b.cfg.ToolPath != "" {
		return b.cfg.ToolPath, nil
	}
	return osLevelToolFor(runtime.GOOS)
}
