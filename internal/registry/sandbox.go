package registry

import (
	"errors"
	"fmt"
	"strings"
)

// SandboxBackendDocker is the only sandbox backend supported in Epic 11.0. The
// field is validated against this set so an unsupported backend fails at config
// load rather than at execution time.
const SandboxBackendDocker = "docker"

// SandboxConfig is the optional `sandbox:` block in .atcr/config.yaml that
// enables `--exec` reproduction (Epic 11.0). Its mere presence does NOT enable
// execution — `--exec` must also be passed and the backend must pass a preflight
// check (verify.ResolveExecBackend). A nil block means execution is unconfigured
// and `--exec` is refused.
type SandboxConfig struct {
	// Backend selects the executor; only "docker" is supported today.
	Backend string `yaml:"backend,omitempty"`
	// Image is the base container image (must be present locally; runs are
	// network-isolated so it cannot be pulled on demand).
	Image string `yaml:"image,omitempty"`
	// TestCommand is the project's test command run by the run_tests tool, as an
	// argv (e.g. [go, test, ./...]). Required when the block is present.
	TestCommand []string `yaml:"test_command,omitempty"`
	// DockerPath overrides the docker binary location (e.g. a Homebrew install
	// not on the default PATH). Empty resolves "docker" on PATH.
	DockerPath string `yaml:"docker_path,omitempty"`
	// Resource caps. Empty fields inherit the hardened defaults.
	Memory    string `yaml:"memory,omitempty"`
	CPUs      string `yaml:"cpus,omitempty"`
	PidsLimit *int   `yaml:"pids_limit,omitempty"`
	// TimeoutSecs is the default per-run wall-clock budget.
	TimeoutSecs *int `yaml:"timeout_secs,omitempty"`
}

// Validate checks the sandbox block. A nil block is valid (execution simply
// unconfigured). When present, the backend must be supported and a non-empty
// test command is required (run_tests has nothing to run otherwise).
func (s *SandboxConfig) Validate() error {
	if s == nil {
		return nil
	}
	if b := strings.TrimSpace(s.Backend); b != "" && b != SandboxBackendDocker {
		return fmt.Errorf("sandbox.backend %q is unsupported (only %q)", s.Backend, SandboxBackendDocker)
	}
	if len(s.TestCommand) == 0 {
		return errors.New("sandbox.test_command is required when a sandbox block is present")
	}
	for _, tok := range s.TestCommand {
		if strings.TrimSpace(tok) == "" {
			return errors.New("sandbox.test_command must not contain empty tokens")
		}
	}
	if s.PidsLimit != nil && *s.PidsLimit <= 0 {
		return errors.New("sandbox.pids_limit must be positive")
	}
	if s.TimeoutSecs != nil && (*s.TimeoutSecs <= 0 || *s.TimeoutSecs > MaxTimeoutSecs) {
		return fmt.Errorf("sandbox.timeout_secs must be within 1..%d", MaxTimeoutSecs)
	}
	return nil
}
