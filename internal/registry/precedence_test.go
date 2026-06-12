package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
func int64Ptr(i int64) *int64 { return &i }

// resolve is a test helper asserting resolution succeeds.
func resolve(t *testing.T, cli CLIOverrides, proj *ProjectConfig, reg *Registry) Settings {
	t.Helper()
	s, err := ResolveSettings(cli, proj, reg)
	require.NoError(t, err)
	return s
}

// loadRegistryWith loads a minimal registry carrying the given user-level
// default lines.
func loadRegistryWith(t *testing.T, globals string) *Registry {
	t.Helper()
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m}
`+globals))
	require.NoError(t, err)
	return reg
}

func TestPrecedence_CLIOverridesProject(t *testing.T) {
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ntimeout_secs: 600\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{TimeoutSecs: intPtr(300)}, proj, nil)
	assert.Equal(t, 300, s.TimeoutSecs, "CLI flag wins over project config")
}

func TestPrecedence_ProjectOverridesRegistry(t *testing.T) {
	reg := loadRegistryWith(t, "timeout_secs: 1200\npayload_mode: files\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ntimeout_secs: 900\npayload_mode: diff\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, 900, s.TimeoutSecs, "project config wins over registry")
	assert.Equal(t, "diff", s.PayloadMode, "project config wins over registry")
}

func TestPrecedence_RegistryOverridesEmbedded(t *testing.T) {
	reg := loadRegistryWith(t, "timeout_secs: 1200\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, 1200, s.TimeoutSecs, "registry wins over embedded default")
}

func TestPrecedence_FullChain(t *testing.T) {
	reg := loadRegistryWith(t, "payload_mode: files\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\npayload_mode: blocks\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{PayloadMode: strPtr("diff")}, proj, reg)
	assert.Equal(t, "diff", s.PayloadMode, "CLI flag wins over the full chain")
}

func TestPrecedence_NoOverride(t *testing.T) {
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, nil)
	assert.Equal(t, DefaultPayloadMode, s.PayloadMode, "embedded default used when nothing overrides")
	assert.Equal(t, DefaultTimeoutSecs, s.TimeoutSecs)
}

func TestPrecedence_ByteBudgetDefault(t *testing.T) {
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, nil)
	assert.Equal(t, int64(DefaultPayloadByteBudget), s.PayloadByteBudget, "embedded default used when nothing overrides")
	assert.Equal(t, int64(524288), s.PayloadByteBudget, "v1 ships a 512 KiB default")
}

func TestPrecedence_ByteBudgetChain(t *testing.T) {
	reg := loadRegistryWith(t, "payload_byte_budget: 1000\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\npayload_byte_budget: 2000\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, int64(2000), s.PayloadByteBudget, "project config wins over registry")

	s = resolve(t, CLIOverrides{PayloadByteBudget: int64Ptr(3000)}, proj, reg)
	assert.Equal(t, int64(3000), s.PayloadByteBudget, "CLI flag wins over the full chain")
}

func TestPrecedence_ByteBudgetExplicitZeroIsUnlimited(t *testing.T) {
	// 0 is the documented unlimited escape hatch (AC 06-03) and must survive
	// default application: an explicit zero is a real override, not "unset".
	reg := loadRegistryWith(t, "payload_byte_budget: 1000\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\npayload_byte_budget: 0\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, int64(0), s.PayloadByteBudget, "explicit 0 overrides both registry and embedded default")
}

func TestPrecedence_ByteBudgetCLINegativeRejected(t *testing.T) {
	_, err := ResolveSettings(CLIOverrides{PayloadByteBudget: int64Ptr(-1)}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "byte budget")
}

func TestRegistry_ByteBudgetNegativeRejectedAtLoad(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m}
payload_byte_budget: -5
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payload_byte_budget")
}

func TestPrecedence_EachFieldIndependent(t *testing.T) {
	reg := loadRegistryWith(t, "fail_on: LOW\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ntimeout_secs: 900\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{PayloadMode: strPtr("diff")}, proj, reg)
	assert.Equal(t, "diff", s.PayloadMode, "from CLI")
	assert.Equal(t, 900, s.TimeoutSecs, "from project")
}

func TestPrecedence_NilTiersFallThrough(t *testing.T) {
	s := resolve(t, CLIOverrides{}, nil, nil)
	assert.Equal(t, DefaultPayloadMode, s.PayloadMode)
	assert.Equal(t, DefaultTimeoutSecs, s.TimeoutSecs)
}

func TestPrecedence_CLITimeoutValidated(t *testing.T) {
	for name, v := range map[string]int{
		"zero":      0,
		"negative":  -10,
		"too large": MaxTimeoutSecs + 1,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ResolveSettings(CLIOverrides{TimeoutSecs: intPtr(v)}, nil, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "timeout")
		})
	}
}

func TestPrecedence_EmptyCLIStringTreatedAsUnset(t *testing.T) {
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\npayload_mode: diff\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{PayloadMode: strPtr("")}, proj, nil)
	assert.Equal(t, "diff", s.PayloadMode, "empty CLI string must not clobber lower tiers")
}

func TestPrecedence_WhitespaceTierValuesIgnored(t *testing.T) {
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\npayload_mode: \" \"\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, nil)
	assert.Equal(t, DefaultPayloadMode, s.PayloadMode, "whitespace-only YAML value counts as unset")
}

func TestRegistryGlobals_Validation(t *testing.T) {
	for name, v := range map[string]string{
		"negative registry timeout":  "timeout_secs: -1",
		"zero registry timeout":      "timeout_secs: 0",
		"oversized registry timeout": "timeout_secs: 99999999999999",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadRegistry(writeRegistry(t, "providers:\n  p:\n    api_key_env: KEY\nagents: {}\n"+v+"\n"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "timeout_secs")
		})
	}
}

func TestRegistryGlobals_AbsentStayUnset(t *testing.T) {
	reg := loadRegistryWith(t, "")
	assert.Empty(t, reg.PayloadMode)
	assert.Nil(t, reg.TimeoutSecs)
	assert.Empty(t, reg.FailOn)
	assert.Nil(t, reg.MaxParallel)
}

func TestProjectConfig_AbsentFieldsStayUnset(t *testing.T) {
	cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)
	assert.Empty(t, cfg.PayloadMode)
	assert.Nil(t, cfg.TimeoutSecs)
	assert.Empty(t, cfg.FailOn)
	assert.Nil(t, cfg.MaxParallel)
}

func TestEffectivePayloadMode(t *testing.T) {
	s := Settings{PayloadMode: "blocks"}
	withOwn := AgentConfig{Payload: "diff"}
	without := AgentConfig{}
	whitespace := AgentConfig{Payload: "  "}
	assert.Equal(t, "diff", withOwn.EffectivePayloadMode(s), "agent's own payload override wins")
	assert.Equal(t, "blocks", without.EffectivePayloadMode(s), "unset payload inherits resolved settings")
	assert.Equal(t, "blocks", whitespace.EffectivePayloadMode(s), "whitespace payload counts as unset")
}

func TestEffectiveTimeoutSecs(t *testing.T) {
	s := Settings{TimeoutSecs: 900}
	withOwn := AgentConfig{TimeoutSecs: intPtr(120)}
	without := AgentConfig{}
	assert.Equal(t, 120, withOwn.EffectiveTimeoutSecs(s), "agent's own timeout wins")
	assert.Equal(t, 900, without.EffectiveTimeoutSecs(s), "unset agent timeout inherits resolved settings")
}

func TestPrecedence_MaxParallelDefault(t *testing.T) {
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, nil)
	assert.Equal(t, DefaultMaxParallel, s.MaxParallel, "embedded default used when nothing overrides")
	assert.Equal(t, 10, s.MaxParallel, "v1 ships a max_parallel default of 10")
}

func TestPrecedence_MaxParallelChain(t *testing.T) {
	reg := loadRegistryWith(t, "max_parallel: 4\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_parallel: 6\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, 6, s.MaxParallel, "project config wins over registry")

	s = resolve(t, CLIOverrides{MaxParallel: intPtr(8)}, proj, reg)
	assert.Equal(t, 8, s.MaxParallel, "CLI flag wins over the full chain")
}

func TestPrecedence_MaxParallelRegistryOverridesEmbedded(t *testing.T) {
	reg := loadRegistryWith(t, "max_parallel: 4\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, 4, s.MaxParallel, "registry wins over embedded default")
}

func TestPrecedence_MaxParallelExplicitZeroIsUnbounded(t *testing.T) {
	// 0 is the documented unbounded escape hatch and must survive default
	// application: an explicit zero is a real override, not "unset".
	reg := loadRegistryWith(t, "max_parallel: 4\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_parallel: 0\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, 0, s.MaxParallel, "explicit 0 overrides both registry and embedded default")
}

func TestPrecedence_MaxParallelCLIZeroAllowed(t *testing.T) {
	s, err := ResolveSettings(CLIOverrides{MaxParallel: intPtr(0)}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, s.MaxParallel, "CLI 0 is the unbounded escape hatch, not an error")
}

func TestPrecedence_MaxParallelCLINegativeRejected(t *testing.T) {
	_, err := ResolveSettings(CLIOverrides{MaxParallel: intPtr(-1)}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_parallel")
}

func TestRegistry_MaxParallelNegativeRejectedAtLoad(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m}
max_parallel: -5
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_parallel")
}

func TestProject_MaxParallelNegativeRejectedAtLoad(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_parallel: -1\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_parallel")
}

// TestPrecedence_FileTierNegativeMaxParallelRejectedAtResolve verifies that a
// ProjectConfig or Registry constructed directly (bypassing the file loader)
// cannot sneak a negative MaxParallel into the engine via ResolveSettings.
// The engine treats n<=0 as unbounded; a negative value is the inverse of
// the user's intent and must be caught at the resolution boundary.
func TestPrecedence_FileTierNegativeMaxParallelRejectedAtResolve(t *testing.T) {
	neg := -1
	proj := &ProjectConfig{MaxParallel: &neg}
	_, err := ResolveSettings(CLIOverrides{}, proj, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_parallel")

	// Registry tier must also be guarded.
	reg := &Registry{MaxParallel: &neg}
	_, err = ResolveSettings(CLIOverrides{}, nil, reg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_parallel")
}
