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
		{"docker + test command", &SandboxConfig{Backend: "docker", TestCommand: []string{"go", "test", "./..."}}, true},
		{"default backend + test command", &SandboxConfig{TestCommand: []string{"make", "test"}}, true},
		{"unsupported backend", &SandboxConfig{Backend: "podman", TestCommand: []string{"go", "test"}}, false},
		{"missing test command", &SandboxConfig{Backend: "docker"}, false},
		{"empty test token", &SandboxConfig{TestCommand: []string{"go", ""}}, false},
		{"non-positive pids", &SandboxConfig{TestCommand: []string{"go", "test"}, PidsLimit: &pid}, false},
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
