package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeProject writes content as a config.yaml inside a temp dir and returns
// its path.
func writeProject(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// testRegistry returns a loaded registry with agents bruce, greta, kai.
func testRegistry(t *testing.T) *Registry {
	t.Helper()
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m1}
  greta: {provider: p, model: m2}
  kai: {provider: p, model: m3}
`))
	require.NoError(t, err)
	return reg
}

func TestProjectConfig_ValidFull(t *testing.T) {
	cfg, err := LoadProjectConfig(writeProject(t, `
agents: [bruce, greta]
serial_agents: [kai]
payload_mode: diff
timeout_secs: 900
fail_on: MEDIUM
`))
	require.NoError(t, err)
	assert.Equal(t, []string{"bruce", "greta"}, cfg.Agents)
	assert.Equal(t, []string{"kai"}, cfg.SerialAgents)
	assert.Equal(t, "diff", cfg.PayloadMode)
	require.NotNil(t, cfg.TimeoutSecs)
	assert.Equal(t, 900, *cfg.TimeoutSecs)
	assert.Equal(t, "MEDIUM", cfg.FailOn)
}

func TestDefaultProjectConfigYAML_DocumentsAutoFix(t *testing.T) {
	out := DefaultProjectConfigYAML([]string{"bruce"})
	// The opt-in auto_fix keys must ship as a commented template, mirroring how
	// max_parallel / cache_max_bytes are documented, so an operator enabling
	// --auto-fix has an in-repo stanza to copy (TD-017).
	assert.Contains(t, out, "auto_fix:")
	assert.Contains(t, out, "apply_target:")
	assert.Contains(t, out, "validate_command:")
	assert.Contains(t, out, "validate_timeout:")

	// The rendered config (commented stanza included) must still load cleanly.
	_, err := LoadProjectConfig(writeProject(t, out))
	require.NoError(t, err)
}

func TestDefaultProjectConfigYAML_DocumentsTelemetry(t *testing.T) {
	out := DefaultProjectConfigYAML([]string{"bruce"})
	// Every other project-config knob ships self-documented in the `atcr init`
	// template; telemetry must too, so an operator knows the knob exists and how
	// to opt out (anonymous usage ping; set false or ATCR_TELEMETRY=0 to disable).
	assert.Contains(t, out, "telemetry:")

	// The rendered config (commented telemetry line included) must still load cleanly.
	_, err := LoadProjectConfig(writeProject(t, out))
	require.NoError(t, err)
}

func TestProjectConfig_MinimalRoster(t *testing.T) {
	cfg, err := LoadProjectConfig(writeProject(t, `
agents: [bruce]
`))
	require.NoError(t, err)
	assert.Equal(t, []string{"bruce"}, cfg.Agents)
	assert.Empty(t, cfg.SerialAgents)
	// Embedded defaults are applied by ResolveSettings, not at load time —
	// see TestProjectConfig_AbsentFieldsStayUnset.
}

func TestProjectConfig_FileNotFound(t *testing.T) {
	_, err := LoadProjectConfig(filepath.Join(t.TempDir(), "config.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no roster found")
	assert.Contains(t, err.Error(), ".atcr/config.yaml")
}

func TestProjectConfig_EmptyAgents(t *testing.T) {
	for name, content := range map[string]string{
		"explicit empty list": "agents: []\n",
		"agents key absent":   "payload_mode: blocks\n",
		"comments only":       "# nothing here\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadProjectConfig(writeProject(t, content))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no agents selected")
		})
	}
}

// The serial lane exists for rate-limited providers, so a project where every
// provider is rate-limited legitimately has an empty parallel lane. The roster
// is the union of both lanes (matching fanout's ErrEmptyRoster contract):
// only a config empty in BOTH lanes is rejected.
func TestProjectConfig_SerialOnlyRosterLoads(t *testing.T) {
	cfg, err := LoadProjectConfig(writeProject(t, "agents: []\nserial_agents: [kai]\n"))
	require.NoError(t, err)
	assert.Empty(t, cfg.Agents)
	assert.Equal(t, []string{"kai"}, cfg.SerialAgents)
}

func TestProjectConfig_UnknownField(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, `
agents: [bruce]
serial_agnets: [kai]
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "serial_agnets")
}

func TestProjectConfig_TimeoutValidation(t *testing.T) {
	for name, content := range map[string]string{
		"negative timeout": "agents: [bruce]\ntimeout_secs: -1\n",
		"zero timeout":     "agents: [bruce]\ntimeout_secs: 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadProjectConfig(writeProject(t, content))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "timeout_secs must be positive")
		})
	}
}

func TestProjectConfig_ByteBudgetNegativeRejectedAtLoad(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\npayload_byte_budget: -1\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payload_byte_budget")
}

func TestProjectConfig_ReviewStrategyRejectedAtLoad(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nreview_strategy: invalid\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid review_strategy")
	assert.Contains(t, err.Error(), "config.yaml")
}

func TestProjectConfig_TrailingDocumentSeparatorTolerated(t *testing.T) {
	cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n---\n"))
	require.NoError(t, err, "a trailing --- is a single logical document, not a second one")
	assert.Equal(t, []string{"bruce"}, cfg.Agents)
}

func TestProjectConfig_SecondDocumentWithContentRejected(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n---\nagents: [greta]\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "second YAML document")
}

func TestProjectConfig_EmptyRosterEntries(t *testing.T) {
	for name, content := range map[string]string{
		"empty string agent":      "agents: [\"\"]\n",
		"whitespace agent":        "agents: [\"  \"]\n",
		"whitespace serial agent": "agents: [bruce]\nserial_agents: [\" \"]\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadProjectConfig(writeProject(t, content))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "roster entries must not be empty")
		})
	}
}

func TestProjectConfig_ValidateAgainstNilRegistry(t *testing.T) {
	cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)
	assert.Error(t, cfg.ValidateAgainst(nil), "nil registry must error, not panic")
}

func TestProjectConfig_ValidateAgainstRegistry(t *testing.T) {
	reg := testRegistry(t)

	t.Run("subset selection works", func(t *testing.T) {
		cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce, greta]\n"))
		require.NoError(t, err)
		require.NoError(t, cfg.ValidateAgainst(reg))
		assert.Equal(t, []string{"bruce", "greta"}, cfg.Agents)
	})

	t.Run("serial agents validated too", func(t *testing.T) {
		cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nserial_agents: [kai]\n"))
		require.NoError(t, err)
		assert.NoError(t, cfg.ValidateAgainst(reg))
	})

	t.Run("unknown agent rejected", func(t *testing.T) {
		cfg, err := LoadProjectConfig(writeProject(t, "agents: [unknown-agent]\n"))
		require.NoError(t, err)
		err = cfg.ValidateAgainst(reg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown-agent")
		assert.Contains(t, err.Error(), "not found in registry")
	})

	t.Run("unknown serial agent rejected", func(t *testing.T) {
		cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nserial_agents: [ghost]\n"))
		require.NoError(t, err)
		err = cfg.ValidateAgainst(reg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ghost")
	})

	t.Run("agent in both lanes rejected", func(t *testing.T) {
		cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nserial_agents: [bruce]\n"))
		require.NoError(t, err)
		err = cfg.ValidateAgainst(reg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bruce")
		assert.Contains(t, err.Error(), "both")
	})

	t.Run("duplicate within roster rejected", func(t *testing.T) {
		cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce, bruce]\n"))
		require.NoError(t, err)
		err = cfg.ValidateAgainst(reg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bruce")
	})

	t.Run("duplicate within serial lane rejected", func(t *testing.T) {
		cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nserial_agents: [kai, kai]\n"))
		require.NoError(t, err)
		err = cfg.ValidateAgainst(reg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "kai")
		assert.Contains(t, err.Error(), "serial_agents")
	})
}
