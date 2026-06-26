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
