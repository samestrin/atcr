package registry

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// SandboxBackendDocker is the only sandbox backend supported in Epic 11.0. The
// field is validated against this set so an unsupported backend fails at config
// load rather than at execution time.
const SandboxBackendDocker = "docker"

// SandboxFallbackOSLevel is the only accepted `sandbox.fallback` value: the
// OS-level backend (sandbox-exec on macOS, bwrap on Linux) that verify's
// resolvers engage when Docker's preflight fails AND an operator has explicitly
// opted in here.
//
// Its value MUST stay equal to the backend's own Name() (sandbox.osLevelBackendName,
// internal/sandbox/oslevel.go:27) — that string is how an operator connects what
// they configured to the backend named in a diagnostic.
const SandboxFallbackOSLevel = "os-level"

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
	// Fallback names a second backend to try when the primary (docker) fails its
	// preflight. Empty — the default, and the shape of every config written before
	// this field existed — means NO fallback: the resolvers keep refusing the run
	// outright, which is the fail-closed contract this field must never weaken
	// implicitly. The single accepted value is SandboxFallbackOSLevel, so opting in
	// is always an explicit, greppable act rather than something inferred from the
	// host ("docker absent", "running in CI").
	Fallback string `yaml:"fallback,omitempty"`
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
	// Trimmed in place, like Fallback below: the sibling resolvers test this
	// field for raw non-emptiness and copy it verbatim (internal/verify/exec.go:
	// `sc.Image != ""`), so operator padding must not survive config load.
	s.Image = strings.TrimSpace(s.Image)
	if s.Image == "" {
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
	// DockerPath mirrors the OSLevelConfig.ToolPath contract
	// (internal/sandbox/oslevel.go resolveToolPath/toolPath): when set it must be
	// an absolute path. Both resolvers copy sc.DockerPath verbatim after a raw
	// `!= ""` test, so a typo'd or whitespace-only value would surface only as a
	// Docker preflight failure — which, with fallback: os-level configured, the
	// resolvers treat as "Docker unavailable" and silently downgrade to a backend
	// with no memory/CPU/PID caps. Reject it at config load instead. A
	// whitespace-only value is rejected rather than normalized to "unset" so the
	// typo stays visible; surrounding padding is trimmed in place, like Fallback.
	if s.DockerPath != "" {
		s.DockerPath = strings.TrimSpace(s.DockerPath)
		if s.DockerPath == "" {
			return errors.New("sandbox.docker_path must not be whitespace-only (leave it unset to resolve docker on PATH)")
		}
		if !filepath.IsAbs(s.DockerPath) {
			return fmt.Errorf("sandbox.docker_path must be an absolute path, got %q", s.DockerPath)
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
	// The fallback allowlist is deliberately LAST and self-contained: it shares no
	// early return with the Image/TestCommand checks above, so configuring a
	// fallback can neither exempt a block from them nor change which failure an
	// operator sees for a config broken in two ways. Exact match with no case
	// folding, mirroring the Backend check earlier in this function — "OS-Level"
	// must fail at config load, because at the resolver a value that misses the
	// sentinel is indistinguishable from no fallback at all, and silently refusing
	// to fall back is the opposite of what the operator wrote down.
	//
	// The trimmed value is written BACK to the field rather than only compared:
	// the sibling resolvers test their config fields for raw non-emptiness
	// (internal/verify/exec.go: `sc.DockerPath != ""`, `sc.Image != ""`), so a
	// whitespace-only Fallback that this function classified as "unset" would read
	// as "opted in" to a resolver following that convention — implicit enablement
	// of a fail-closed bypass, from a value the operator never wrote. Normalizing
	// here leaves exactly two states reachable downstream: "" or the sentinel.
	// Safe in place: config load precedes any sharing of the struct.
	s.Fallback = strings.TrimSpace(s.Fallback)
	if s.Fallback != "" && s.Fallback != SandboxFallbackOSLevel {
		return fmt.Errorf("sandbox.fallback %q is unsupported (only %q)", s.Fallback, SandboxFallbackOSLevel)
	}
	return nil
}

// validateMemory rejects a non-empty Memory that docker's --memory would not
// accept. Memory and CPUs are operator strings injected verbatim into
// docker run --memory/--cpus, so a typo that parses at config load but faults
// the container at runtime (or silently weakens the cap) must fail here instead.
// An empty value inherits the hardened default; a valid value is a positive
// plain decimal with an optional b/k/m/g unit suffix, where the unit may itself
// be followed by an optional "b" (e.g. "512m", "512mb", "1.5g", or a bare byte
// count). Exponents, signs, and unknown units are rejected.
func validateMemory(mem string) error {
	m := strings.TrimSpace(mem)
	if m == "" {
		return nil
	}
	num := m
	switch num[len(num)-1] {
	case 'b', 'B':
		num = num[:len(num)-1]
	}
	switch num[len(num)-1] {
	case 'k', 'm', 'g', 'K', 'M', 'G':
		num = num[:len(num)-1]
	}
	if !plainDecimalRegexp.MatchString(num) {
		return fmt.Errorf("sandbox.memory %q is not a valid docker size (e.g. \"512m\", \"1.5g\")", mem)
	}
	if v, err := strconv.ParseFloat(num, 64); err != nil || v <= 0 {
		return fmt.Errorf("sandbox.memory %q is not a valid docker size (e.g. \"512m\", \"1.5g\")", mem)
	}
	return nil
}

// plainDecimalRegexp matches a plain unsigned decimal number: digits with an
// optional fractional part. It rejects exponents and signs so that values
// docker --memory would not accept fail at config load.
var plainDecimalRegexp = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

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
