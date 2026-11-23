package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetTelemetrySetting_QuotedValueBecomesABool is the EST-8260 probe: with
// `telemetry: "true"` in the config, `config set telemetry false` reported
// success but wrote `telemetry: "false"`, which the strict loader still
// rejects as a string.
func TestSetTelemetrySetting_QuotedValueBecomesABool(t *testing.T) {
	for _, quoted := range []string{`"true"`, `'true'`} {
		dir := writeConfigDir(t, "agents: [bruce]\ntelemetry: "+quoted+"\n")
		require.NoError(t, SetTelemetrySetting(dir, false))

		data, err := os.ReadFile(DefaultProjectConfigPath(dir))
		require.NoError(t, err)
		assert.Contains(t, string(data), "telemetry: false\n", "the value must be written unquoted")

		got, err := LoadTelemetrySetting(dir)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.False(t, *got)
		_, err = LoadProjectConfig(DefaultProjectConfigPath(dir))
		require.NoError(t, err, "the strict loader must accept the rewritten config")
	}
}

// TestSetTelemetrySetting_MissingConfigCreatesNothing: a set with no config
// fails, and must not leave an empty .atcr/ behind (EST-8260).
func TestSetTelemetrySetting_MissingConfigCreatesNothing(t *testing.T) {
	root := t.TempDir()
	require.Error(t, SetTelemetrySetting(root, false))
	_, err := os.Lstat(filepath.Join(root, ".atcr"))
	assert.True(t, os.IsNotExist(err), "a failed set must not create .atcr/, got %v", err)
}
