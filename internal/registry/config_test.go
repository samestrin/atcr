package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeRegistry writes content as a registry.yaml inside a temp dir and
// returns its path.
func writeRegistry(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

const validRegistry = `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
  local-llm:
    api_key_env: LOCAL_API_KEY
    base_url: http://localhost:11434/v1
agents:
  bruce:
    provider: openai
    model: gpt-4
  greta:
    provider: local-llm
    model: qwen2.5-coder
    persona: greta
    temperature: 0.3
    timeout_secs: 120
    rate_limited: true
    fallback: bruce
    payload: diff
`

func TestRegistryLoad_ValidConfig(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.NoError(t, err)

	require.Contains(t, reg.Providers, "openai")
	assert.Equal(t, "OPENAI_API_KEY", reg.Providers["openai"].APIKeyEnv)
	assert.Empty(t, reg.Providers["openai"].BaseURL)
	assert.Equal(t, "http://localhost:11434/v1", reg.Providers["local-llm"].BaseURL)

	require.Contains(t, reg.Agents, "greta")
	greta := reg.Agents["greta"]
	assert.Equal(t, "local-llm", greta.Provider)
	assert.Equal(t, "qwen2.5-coder", greta.Model)
	assert.Equal(t, "greta", greta.Persona)
	require.NotNil(t, greta.Temperature)
	assert.InDelta(t, 0.3, *greta.Temperature, 1e-9)
	require.NotNil(t, greta.TimeoutSecs)
	assert.Equal(t, 120, *greta.TimeoutSecs)
	assert.True(t, greta.RateLimited)
	assert.Equal(t, "bruce", greta.Fallback)
	assert.Equal(t, "diff", greta.Payload)
}

func TestRegistryLoad_OptionalFieldDefaults(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.NoError(t, err)

	bruce := reg.Agents["bruce"]
	assert.Equal(t, "bruce", bruce.Persona, "persona defaults to the agent name")
	require.NotNil(t, bruce.Temperature)
	assert.InDelta(t, 0.7, *bruce.Temperature, 1e-9, "temperature defaults to 0.7")
	assert.Nil(t, bruce.TimeoutSecs, "timeout stays unset at load (inherits resolved settings)")
	assert.Equal(t, 600, bruce.EffectiveTimeoutSecs(Settings{TimeoutSecs: DefaultTimeoutSecs}),
		"effective timeout defaults to 600 via the settings chain")
	assert.False(t, bruce.RateLimited, "rate_limited defaults to false")
	assert.Empty(t, bruce.Payload, "payload stays empty when unset (inherits project default)")
	assert.Empty(t, bruce.Fallback)
}

func TestRegistryLoad_MissingProvider(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    model: gpt-4
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "provider")
}

func TestRegistryLoad_MissingModel(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "model")
}

func TestRegistryLoad_MissingAPIKeyEnv(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai: {}
agents:
  bruce:
    provider: openai
    model: gpt-4
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "openai")
	assert.Contains(t, err.Error(), "api_key_env")
}

func TestRegistryLoad_UnknownField(t *testing.T) {
	t.Run("agent typo", func(t *testing.T) {
		_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    temprature: 0.5
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "temprature")
	})
	t.Run("provider typo", func(t *testing.T) {
		_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
    typo_field: zzz
agents: {}
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "typo_field")
	})
}

func TestRegistryLoad_DanglingProviderRef(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: nonexistent
    model: gpt-4
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestRegistryLoad_NoRegistryFile(t *testing.T) {
	_, err := LoadRegistry(filepath.Join(t.TempDir(), "registry.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "registry not found")
	assert.Contains(t, err.Error(), "atcr init")
}

func TestRegistryLoad_EmptyFile(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, ""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestRegistryLoad_InvalidYAMLSyntax(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, "providers: [unclosed"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "registry.yaml")
}

func TestRegistryLoad_BaseURLScheme(t *testing.T) {
	t.Run("https accepted", func(t *testing.T) {
		_, err := LoadRegistry(writeRegistry(t, `
providers:
  remote:
    api_key_env: KEY
    base_url: https://api.example.com/v1
agents: {}
`))
		assert.NoError(t, err)
	})
	t.Run("non-http scheme rejected", func(t *testing.T) {
		_, err := LoadRegistry(writeRegistry(t, `
providers:
  remote:
    api_key_env: KEY
    base_url: file:///etc/passwd
agents: {}
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "base_url")
	})
	t.Run("garbage url rejected", func(t *testing.T) {
		_, err := LoadRegistry(writeRegistry(t, `
providers:
  remote:
    api_key_env: KEY
    base_url: "::not a url::"
agents: {}
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "base_url")
	})
}

func TestRegistryLoad_APIKeyNotReadAtLoad(t *testing.T) {
	// The env var named by api_key_env is deliberately NOT set; loading must
	// still succeed because resolution happens at invoke time.
	t.Setenv("ATCR_TEST_UNSET_KEY", "")
	require.NoError(t, os.Unsetenv("ATCR_TEST_UNSET_KEY"))

	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: ATCR_TEST_UNSET_KEY
agents:
  a:
    provider: p
    model: m
`))
	require.NoError(t, err)
	assert.Equal(t, "ATCR_TEST_UNSET_KEY", reg.Providers["p"].APIKeyEnv)
}

func TestRegistryLoad_ValidationRejections(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			"comments-only file",
			"# just a comment\n# another\n",
			"empty",
		},
		{
			"trailing second document",
			validRegistry + "\n---\nproviders: {}\n",
			"second YAML document",
		},
		{
			"zero timeout",
			"providers:\n  p:\n    api_key_env: KEY\nagents:\n  a:\n    provider: p\n    model: m\n    timeout_secs: 0\n",
			"timeout_secs must be within",
		},
		{
			"negative timeout",
			"providers:\n  p:\n    api_key_env: KEY\nagents:\n  a:\n    provider: p\n    model: m\n    timeout_secs: -5\n",
			"timeout_secs must be within",
		},
		{
			"temperature out of range",
			"providers:\n  p:\n    api_key_env: KEY\nagents:\n  a:\n    provider: p\n    model: m\n    temperature: 9.5\n",
			"temperature",
		},
		{
			"whitespace agent name",
			"providers:\n  p:\n    api_key_env: KEY\nagents:\n  \" \":\n    provider: p\n    model: m\n",
			"agent name must not be empty",
		},
		{
			"whitespace provider name",
			"providers:\n  \" \":\n    api_key_env: KEY\nagents: {}\n",
			"provider name must not be empty",
		},
		{
			"invalid api_key_env format",
			"providers:\n  p:\n    api_key_env: \"MY KEY\"\nagents: {}\n",
			"not a valid environment variable name",
		},
		{
			"base_url with embedded credentials",
			"providers:\n  p:\n    api_key_env: KEY\n    base_url: https://user:secret@host/v1\nagents: {}\n",
			"must not embed credentials",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadRegistry(writeRegistry(t, tt.yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestRegistryLoad_ExplicitZeroTemperatureSurvives(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  a:
    provider: p
    model: m
    temperature: 0
`))
	require.NoError(t, err)
	require.NotNil(t, reg.Agents["a"].Temperature)
	assert.Zero(t, *reg.Agents["a"].Temperature, "explicit temperature 0 must not be rewritten to the default")
}

func TestRegistryLoad_YAML11BooleanQuirk(t *testing.T) {
	// YAML 1.1 styles like `yes` decode into bool fields; document the
	// behavior so hand-edited registries behave predictably.
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  a:
    provider: p
    model: m
    rate_limited: yes
`))
	require.NoError(t, err)
	assert.True(t, reg.Agents["a"].RateLimited)
}

// --- Epic 1.1: reserved agentic-stage fields (parsed + validated, inert in 1.x) ---

// TestRegistryLoad_ReservedFieldsParsed verifies the four reserved fields
// (tools, max_turns, tool_budget_bytes, role) load cleanly under the strict
// v1 parser instead of being rejected as unknown keys, and that their values
// are preserved for the stage that will eventually act on them.
func TestRegistryLoad_ReservedFieldsParsed(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  a:
    provider: p
    model: m
    tools: true
    max_turns: 5
    tool_budget_bytes: 4096
    role: skeptic
`))
	require.NoError(t, err)
	a := reg.Agents["a"]
	assert.True(t, a.Tools, "tools parsed")
	require.NotNil(t, a.MaxTurns)
	assert.Equal(t, 5, *a.MaxTurns, "max_turns parsed")
	require.NotNil(t, a.ToolBudgetBytes)
	assert.Equal(t, int64(4096), *a.ToolBudgetBytes, "tool_budget_bytes parsed")
	assert.Equal(t, "skeptic", a.Role, "role parsed")
}

// TestRegistryLoad_ReservedFieldsInert verifies the reserved fields stay at
// their inert zero/unset state when omitted — no load-time default is applied
// (Epic 1.1 keeps the 1.x behavior footprint exactly zero).
func TestRegistryLoad_ReservedFieldsInert(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.NoError(t, err)
	bruce := reg.Agents["bruce"]
	assert.False(t, bruce.Tools, "tools defaults to false (zero value, no default applied)")
	assert.Nil(t, bruce.MaxTurns, "max_turns stays unset (no default applied in 1.x)")
	assert.Nil(t, bruce.ToolBudgetBytes, "tool_budget_bytes stays unset")
	assert.Empty(t, bruce.Role, "role stays empty (no default applied in 1.x)")
}

// TestRegistryLoad_ReservedFieldValidation covers load-time validation of the
// reserved fields: enum check for role, positivity for max_turns,
// non-negativity for tool_budget_bytes.
func TestRegistryLoad_ReservedFieldValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			"invalid role",
			"providers:\n  p:\n    api_key_env: KEY\nagents:\n  a:\n    provider: p\n    model: m\n    role: overlord\n",
			"role must be one of",
		},
		{
			"zero max_turns",
			"providers:\n  p:\n    api_key_env: KEY\nagents:\n  a:\n    provider: p\n    model: m\n    max_turns: 0\n",
			"max_turns must be",
		},
		{
			"negative max_turns",
			"providers:\n  p:\n    api_key_env: KEY\nagents:\n  a:\n    provider: p\n    model: m\n    max_turns: -3\n",
			"max_turns must be",
		},
		{
			"absurd max_turns",
			"providers:\n  p:\n    api_key_env: KEY\nagents:\n  a:\n    provider: p\n    model: m\n    max_turns: 2000000000\n",
			"max_turns must be",
		},
		{
			"negative tool_budget_bytes",
			"providers:\n  p:\n    api_key_env: KEY\nagents:\n  a:\n    provider: p\n    model: m\n    tool_budget_bytes: -1\n",
			"tool_budget_bytes must be",
		},
		{
			"wrong type for tools",
			"providers:\n  p:\n    api_key_env: KEY\nagents:\n  a:\n    provider: p\n    model: m\n    tools: maybe\n",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadRegistry(writeRegistry(t, tt.yaml))
			require.Error(t, err)
			if tt.wantErr != "" {
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

// TestRegistryLoad_ReservedRolesAccepted verifies every valid role enum value
// (and an explicit reviewer) loads cleanly.
func TestRegistryLoad_ReservedRolesAccepted(t *testing.T) {
	for _, role := range []string{"reviewer", "skeptic", "judge"} {
		t.Run(role, func(t *testing.T) {
			reg, err := LoadRegistry(writeRegistry(t, "providers:\n  p:\n    api_key_env: KEY\nagents:\n  a:\n    provider: p\n    model: m\n    role: "+role+"\n"))
			require.NoError(t, err)
			assert.Equal(t, role, reg.Agents["a"].Role)
		})
	}
}

// TestAgentConfig_ToolBudgetBytesIsInt64 is a compile-time assertion that
// ToolBudgetBytes uses *int64 to match the *int64 ToolBytes in status.json and
// the *int64 PayloadByteBudget — all byte-quantity fields must share the same
// integer width so future agentic stages don't hit a silent truncation.
func TestAgentConfig_ToolBudgetBytesIsInt64(t *testing.T) {
	// This line fails to compile when ToolBudgetBytes is *int.
	a := AgentConfig{ToolBudgetBytes: new(int64)}
	if a.ToolBudgetBytes == nil {
		t.Fatal("ToolBudgetBytes must not be nil")
	}
}
