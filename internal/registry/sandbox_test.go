package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSandboxConfig_Validate(t *testing.T) {
	pid := 0
	cases := []struct {
		name string
		cfg  *SandboxConfig
		ok   bool
	}{
		{"nil is valid (unconfigured)", nil, true},
		{"docker + image + test command", &SandboxConfig{Backend: "docker", Image: "golang:1.25", TestCommand: []string{"go", "test", "./..."}}, true},
		{"default backend + image + test command", &SandboxConfig{Image: "python:3.12", TestCommand: []string{"make", "test"}}, true},
		{"unsupported backend", &SandboxConfig{Backend: "podman", Image: "img", TestCommand: []string{"go", "test"}}, false},
		{"missing image", &SandboxConfig{Backend: "docker", TestCommand: []string{"go", "test"}}, false},
		{"missing test command", &SandboxConfig{Backend: "docker", Image: "img"}, false},
		{"empty test token", &SandboxConfig{Image: "img", TestCommand: []string{"go", ""}}, false},
		{"non-positive pids", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, PidsLimit: &pid}, false},
		{"valid memory with unit", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, Memory: "512m"}, true},
		{"valid memory bare bytes", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, Memory: "512"}, true},
		{"invalid memory non-numeric", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, Memory: "abc"}, false},
		{"invalid memory zero", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, Memory: "0"}, false},
		{"valid cpus float", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, CPUs: "1.5"}, true},
		{"invalid cpus non-numeric", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, CPUs: "abc"}, false},
		{"invalid cpus zero", &SandboxConfig{Image: "img", TestCommand: []string{"go", "test"}, CPUs: "0"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.ok {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

// TestSandboxConfig_Validate_AutoFixTensionUnchanged pins the open design tension
// surfaced by Sprint 32.0 (AC 02-02): an operator who adds a `sandbox:` block
// SOLELY so `--auto-fix`'s validation step can be sandboxed — and omits
// test_command, which is irrelevant to auto-fix (it runs auto_fix.validate_command,
// not test_command) — STILL fails config load with the unconditional
// Image+TestCommand requirement. Sprint 32.0 deliberately does NOT relax this
// (loosening it would weaken `--exec`'s existing contract); it pins the current
// behavior here and leaves a split/parallel-light-validation path as future work.
// If a later change relaxes SandboxConfig.Validate() for the auto-fix case, this
// test must be revisited as a conscious decision, not slipped in silently.
func TestSandboxConfig_Validate_AutoFixTensionUnchanged(t *testing.T) {
	// A sandbox block added only for --auto-fix, missing the (auto-fix-irrelevant)
	// test_command, must still be rejected at config load.
	autoFixOnly := &SandboxConfig{Backend: "docker", Image: "golang:1.25"}
	err := autoFixOnly.Validate()
	require.Error(t, err, "auto-fix-only sandbox block must still require test_command (tension NOT relaxed)")
	assert.Contains(t, err.Error(), "test_command is required")

	// Symmetrically, omitting image is still rejected.
	noImage := &SandboxConfig{Backend: "docker", TestCommand: []string{"go", "test", "./..."}}
	noImageErr := noImage.Validate()
	require.Error(t, noImageErr)
	assert.Contains(t, noImageErr.Error(), "image is required")
}
