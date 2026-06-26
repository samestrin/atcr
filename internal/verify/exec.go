package verify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
)

// ErrExecNoBackend is returned when `--exec` is requested but no sandbox backend
// is configured. It is the refuse-without-backend gate (Epic 11.0 SC-1): the
// command must hard-error without executing anything.
var ErrExecNoBackend = errors.New("--exec requires a [sandbox] block in .atcr/config.yaml (backend, image, test_command); none is configured")

// ResolveExecBackend implements the execution gate. When execEnabled is false it
// returns nil (execution off — the normal path). When true it REQUIRES a
// configured sandbox backend that passes a preflight check; any failure is an
// error so the caller refuses the run. It never enables execution implicitly.
//
// Returns the ready backend, the resolved test command, and the per-run timeout.
func ResolveExecBackend(ctx context.Context, execEnabled bool, sc *registry.SandboxConfig) (sandbox.Backend, []string, time.Duration, error) {
	if !execEnabled {
		return nil, nil, 0, nil
	}
	if sc == nil {
		return nil, nil, 0, ErrExecNoBackend
	}
	cfg := sandbox.DefaultDockerConfig()
	if sc.DockerPath != "" {
		cfg.DockerPath = sc.DockerPath
	}
	if sc.Image != "" {
		cfg.Image = sc.Image
	}
	if sc.Memory != "" {
		cfg.Memory = sc.Memory
	}
	if sc.CPUs != "" {
		cfg.CPUs = sc.CPUs
	}
	if sc.PidsLimit != nil {
		cfg.PidsLimit = *sc.PidsLimit
	}
	timeout := cfg.Timeout
	if sc.TimeoutSecs != nil {
		timeout = time.Duration(*sc.TimeoutSecs) * time.Second
		cfg.Timeout = timeout
	}
	backend := sandbox.NewDockerBackend(cfg)
	if err := backend.Preflight(ctx); err != nil {
		return nil, nil, 0, fmt.Errorf("--exec preflight failed: %w", err)
	}
	return backend, sc.TestCommand, timeout, nil
}
