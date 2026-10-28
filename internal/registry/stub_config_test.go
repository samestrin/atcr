package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeStubTestConfig(t *testing.T, root, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root), []byte(body), 0o644))
}

// TestLoadStubConfig pins which configs count as atcr-written stubs (Epic 84.0
// F1): only the bookkeeping and opt-out keys atcr writes on its own, so a config
// carrying anything a user or `atcr init` wrote is never treated as one.
func TestLoadStubConfig(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		stub     bool
		settings map[string]bool
	}{
		{"notice only", "telemetry_notice_shown: true\n", true, map[string]bool{"telemetry_notice_shown": true}},
		{"notice and opt-outs", "telemetry_notice_shown: true\ntelemetry: false\nquality_signal: false\n", true,
			map[string]bool{"telemetry_notice_shown": true, "telemetry": false, "quality_signal": false}},
		{"empty file", "", true, map[string]bool{}},
		{"has a roster", "agents: [bruce]\ntelemetry_notice_shown: true\n", false, nil},
		{"has another setting", "payload_mode: blocks\ntelemetry_notice_shown: true\n", false, nil},
		{"non-boolean value", "telemetry: maybe\n", false, nil},
		{"not a mapping", "- bruce\n", false, nil},
		{"unparseable", "agents: [\n", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeStubTestConfig(t, root, tc.body)
			settings, stub := LoadStubConfig(root)
			assert.Equal(t, tc.stub, stub)
			if tc.stub {
				assert.Equal(t, tc.settings, settings)
			}
		})
	}
}

func TestLoadStubConfig_AbsentOrSymlinkIsNotStub(t *testing.T) {
	root := t.TempDir()
	_, stub := LoadStubConfig(root)
	assert.False(t, stub, "an absent config is not a stub: there is nothing to replace")

	target := filepath.Join(t.TempDir(), "elsewhere.yaml")
	require.NoError(t, os.WriteFile(target, []byte("telemetry_notice_shown: true\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.Symlink(target, DefaultProjectConfigPath(root)))
	_, stub = LoadStubConfig(root)
	assert.False(t, stub, "a symlinked config is the user's arrangement, never replaced")
}
