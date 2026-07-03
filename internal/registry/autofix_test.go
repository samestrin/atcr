package registry

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoFixConfig_Validate(t *testing.T) {
	t.Run("nil block is valid", func(t *testing.T) {
		var a *AutoFixConfig
		require.NoError(t, a.Validate())
	})
	t.Run("empty block is valid (inherits defaults)", func(t *testing.T) {
		require.NoError(t, (&AutoFixConfig{}).Validate())
	})
	t.Run("empty token in validate_command is rejected", func(t *testing.T) {
		err := (&AutoFixConfig{ValidateCommand: []string{"go", "  "}}).Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "validate_command")
	})
	t.Run("good duration is accepted", func(t *testing.T) {
		require.NoError(t, (&AutoFixConfig{ValidateTimeout: "90s"}).Validate())
	})
	t.Run("malformed duration is rejected", func(t *testing.T) {
		err := (&AutoFixConfig{ValidateTimeout: "twenty"}).Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "validate_timeout")
	})
	t.Run("non-positive duration is rejected", func(t *testing.T) {
		err := (&AutoFixConfig{ValidateTimeout: "0s"}).Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "positive")
	})
}

// TestLoadProjectConfig_AutoFixBlock: an auto_fix block is strict-decoded and
// its Validate runs at load time (a bad timeout fails the load).
func TestLoadProjectConfig_AutoFixBlock(t *testing.T) {
	dir := t.TempDir()
	good := dir + "/good.yaml"
	require.NoError(t, os.WriteFile(good, []byte("agents:\n  - a\nauto_fix:\n  apply_target: \".\"\n  validate_command: [go, build, \"./...\"]\n  validate_timeout: 2m\n"), 0o644))
	cfg, err := LoadProjectConfig(good)
	require.NoError(t, err)
	require.NotNil(t, cfg.AutoFix)
	require.Equal(t, []string{"go", "build", "./..."}, cfg.AutoFix.ValidateCommand)

	bad := dir + "/bad.yaml"
	require.NoError(t, os.WriteFile(bad, []byte("agents:\n  - a\nauto_fix:\n  validate_timeout: nope\n"), 0o644))
	_, err = LoadProjectConfig(bad)
	require.Error(t, err)
	require.Contains(t, err.Error(), "validate_timeout")
}
