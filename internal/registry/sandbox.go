package registry

import (
	"errors"
	"fmt"
	"strconv"
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
	if strings.TrimSpace(s.Image) == "" {
		return errors.New("sandbox.image is required when a sandbox block is present (a base image carrying the toolchain your test_command needs)")
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
	if err := validateMemory(s.Memory); err != nil {
		return err
	}
	if err := validateCPUs(s.CPUs); err != nil {
		return err
	}
	return nil
}

// validateMemory rejects a non-empty Memory that docker's --memory would not
// accept. Memory and CPUs are operator strings injected verbatim into
// docker run --memory/--cpus, so a typo that parses at config load but faults
// the container at runtime (or silently weakens the cap) must fail here instead.
// An empty value inherits the hardened default; a valid value is a positive
// number with an optional b/k/m/g unit suffix (e.g. "512m", "1.5g", or a bare
// byte count). "0", "abc", and "512x" are rejected.
func validateMemory(mem string) error {
	m := strings.TrimSpace(mem)
	if m == "" {
		return nil
	}
	num := m
	switch m[len(m)-1] {
	case 'b', 'k', 'm', 'g', 'B', 'K', 'M', 'G':
		num = m[:len(m)-1]
	}
	if v, err := strconv.ParseFloat(num, 64); err != nil || v <= 0 {
		return fmt.Errorf("sandbox.memory %q is not a valid docker size (e.g. \"512m\", \"1.5g\")", mem)
	}
	return nil
}

// validateCPUs rejects a non-empty CPUs that docker's --cpus would not accept.
// An empty value inherits the hardened default; otherwise it must be a positive
// float (e.g. "1.5"). "0", "-1", and "abc" are rejected.
func validateCPUs(cpus string) error {
	c := strings.TrimSpace(cpus)
	if c == "" {
		return nil
	}
	if v, err := strconv.ParseFloat(c, 64); err != nil || v <= 0 {
		return fmt.Errorf("sandbox.cpus %q must be a positive number (e.g. \"1.5\")", cpus)
	}
	return nil
}
