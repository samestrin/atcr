package registry

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectConfig_PayloadModeValid(t *testing.T) {
	for _, mode := range []string{"diff", "blocks", "files"} {
		cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\npayload_mode: "+mode+"\n"))
		require.NoErrorf(t, err, "mode %q", mode)
		assert.Equal(t, mode, cfg.PayloadMode)
	}
}

func TestProjectConfig_PayloadModeInvalid(t *testing.T) {
	for _, bad := range []string{"invalid", "DIFF", "Blocks"} {
		t.Run(bad, func(t *testing.T) {
			_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\npayload_mode: \""+bad+"\"\n"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid payload_mode")
			assert.Contains(t, err.Error(), "must be one of diff, blocks, files")
		})
	}
}

func TestProjectConfig_PayloadModeEmptyIsUnset(t *testing.T) {
	// Empty / whitespace falls through to a later precedence tier — not an error.
	for _, val := range []string{`""`, `" "`} {
		cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\npayload_mode: "+val+"\n"))
		require.NoErrorf(t, err, "value %q", val)
		assert.Equal(t, DefaultPayloadMode, ResolveSettingsPayload(t, cfg))
	}
}

// ResolveSettingsPayload is a tiny helper resolving the effective payload mode
// for a project config with no registry/CLI tiers.
func ResolveSettingsPayload(t *testing.T, cfg *ProjectConfig) string {
	t.Helper()
	s, err := ResolveSettings(CLIOverrides{}, cfg, nil)
	require.NoError(t, err)
	return s.PayloadMode
}

func TestRegistry_CLIPayloadModeInvalid(t *testing.T) {
	// The CLI tier bypasses file-load checks, so ResolveSettings must reject an
	// invalid --payload value before any review work begins.
	bad := "DIFF"
	_, err := ResolveSettings(CLIOverrides{PayloadMode: &bad}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid payload_mode")
	assert.Contains(t, err.Error(), "must be one of diff, blocks, files")
}

func TestRegistry_PayloadModeWhitespaceAroundValid(t *testing.T) {
	// "  diff  " trims to a valid value: accepted at load and resolves to diff.
	cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\npayload_mode: \"  diff  \"\n"))
	require.NoError(t, err)
	assert.Equal(t, "diff", ResolveSettingsPayload(t, cfg))
}

func TestRegistry_AgentPayloadValid(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p: {api_key_env: KEY}
agents:
  bruce: {provider: p, model: m, payload: diff}
  greta: {provider: p, model: m, payload: files}
`))
	require.NoError(t, err)
	assert.Equal(t, "diff", reg.Agents["bruce"].Payload)
	assert.Equal(t, "files", reg.Agents["greta"].Payload)
}

func TestRegistry_AgentPayloadInvalid(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p: {api_key_env: KEY}
agents:
  bruce: {provider: p, model: m, payload: wrong}
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent 'bruce'")
	assert.Contains(t, err.Error(), "invalid payload 'wrong'")
	assert.Contains(t, err.Error(), "must be one of diff, blocks, files")
}

func TestPayloadModeEnumParity(t *testing.T) {
	// validPayloadModes deliberately hand-duplicates the frozen enum in
	// internal/payload (the package boundary forbids importing it in
	// production code). This test imports both sides and fails when they
	// drift. payload.validModes is unexported, so the reverse direction is
	// checked through the exported mode constants plus the frozen set size.
	for mode := range validPayloadModes {
		assert.Truef(t, payload.ValidMode(mode), "registry accepts payload_mode %q but internal/payload rejects it", mode)
	}
	for _, mode := range []payload.PayloadMode{payload.ModeDiff, payload.ModeBlocks, payload.ModeFiles} {
		assert.Truef(t, validPayloadModes[string(mode)], "internal/payload defines mode %q but registry rejects it", mode)
	}
	assert.Len(t, validPayloadModes, 3, "frozen v1 enum has exactly three modes; adding one is a coordinated change in both packages")
}

func TestRegistry_DefaultPayloadModeInvalid(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p: {api_key_env: KEY}
agents:
  bruce: {provider: p, model: m}
payload_mode: BOGUS
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid payload_mode")
}
