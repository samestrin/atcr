package registry

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 2.2: scope / min_severity / max_findings are optional per-agent review
// guardrails. They parse off AgentConfig and are validated at load.
func TestRegistryLoad_ReviewConstraints(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
  bruce-backup:
    provider: openai
    model: gpt-4-mini
    scope: ["performance", "efficiency"]
    min_severity: MEDIUM
    max_findings: 20
`))
	require.NoError(t, err)

	bb := reg.Agents["bruce-backup"]
	assert.Equal(t, []string{"performance", "efficiency"}, bb.Scope)
	assert.Equal(t, "MEDIUM", bb.MinSeverity)
	require.NotNil(t, bb.MaxFindings)
	assert.Equal(t, 20, *bb.MaxFindings)

	// Unset on a plain agent — backward compatible, all zero values.
	bruce := reg.Agents["bruce"]
	assert.Nil(t, bruce.Scope, "scope stays nil when unset")
	assert.Empty(t, bruce.MinSeverity, "min_severity stays empty when unset")
	assert.Nil(t, bruce.MaxFindings, "max_findings stays nil when unset")
}

func TestRegistryLoad_InvalidMinSeverity(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    min_severity: SOMETIMES
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "min_severity")
}

func TestRegistryLoad_InvalidMaxFindings(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    max_findings: 0
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "max_findings")
}

func TestRegistryLoad_MaxFindingsExceedsCap(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    max_findings: 99999
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "max_findings")
}

func TestRegistryLoad_EmptyScopeEntry(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    scope: ["performance", ""]
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "scope")
}

// Whitespace-only scope entries exercise the strings.TrimSpace branch in
// validation; without this case, the TrimSpace call could be removed and only
// the "" case would catch the regression.
func TestRegistryLoad_WhitespaceOnlyScopeEntry(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    scope: ["performance", "   "]
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "scope")
}

// min_severity accepts every rubric level, case-insensitively normalized to the
// canonical upper-case form so enforcement comparisons are stable.
func TestRegistryLoad_MinSeverityCaseInsensitive(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    min_severity: high
`))
	require.NoError(t, err)
	assert.Equal(t, "HIGH", reg.Agents["bruce"].MinSeverity, "min_severity normalized to canonical upper-case")
}

// Whitespace-only min_severity should be treated as unset rather than producing
// a confusing "must be one of CRITICAL, HIGH, MEDIUM, LOW" validation error.
func TestRegistryLoad_MinSeverityWhitespaceOnly(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    min_severity: "   "
`))
	require.NoError(t, err, "whitespace-only min_severity should be treated as unset, not a validation error")
	assert.Empty(t, reg.Agents["bruce"].MinSeverity, "whitespace-only min_severity normalized to empty")
}

// Scope entries should be trimmed at load (mirroring MinSeverity canonicalization)
// so that " performance " survives into ScopeFocus as "performance".
func TestRegistryLoad_ScopeEntriesTrimmed(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    scope: [" performance ", "  efficiency"]
`))
	require.NoError(t, err)
	assert.Equal(t, []string{"performance", "efficiency"}, reg.Agents["bruce"].Scope,
		"scope entries trimmed at load so downstream comparisons are stable")
}

// TestRegistryLoad_ScopeAliasing documents that AgentsByRole returns values
// whose Scope slice aliases the registry's backing memory. Mutating the
// returned slice corrupts the shared registry. This is the documented contract
// (see AgentsByRole godoc) — the test anchors it as a regression guard.
func TestRegistryLoad_ScopeAliasing(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    scope: ["performance"]
`))
	require.NoError(t, err)

	byRole := reg.AgentsByRole(RoleReviewer)
	bruceCfg, ok := byRole["bruce"]
	require.True(t, ok)
	require.Len(t, bruceCfg.Scope, 1)

	// Mutate the returned Scope — this SHOULD corrupt the registry because
	// the slice aliases the backing memory (documented contract).
	bruceCfg.Scope[0] = "MUTATED"
	assert.Equal(t, "MUTATED", reg.Agents["bruce"].Scope[0],
		"Scope slice aliases registry backing memory; mutation corrupts shared state")
}

// Scope entries containing control characters (\n, \r, etc.) must be rejected
// at load validation. A newline embedded in a scope entry can break out of the
// injected ScopeFocus block and add arbitrary text to the agent persona prompt.
func TestRegistryLoad_ScopeEntryWithNewline(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    scope: ["performance\nmalicious"]
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "scope")
}

func TestRegistryLoad_ScopeEntryWithCarriageReturn(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    scope: ["performance\rmalicious"]
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "scope")
}

// AgentConfig.Persona selects a reviewer prompt template (resolved via
// ResolvePersona, whose validateName already rejects path traversal). It is the
// one prompt/filesystem-adjacent agent string with no control-char or length
// guard in validateAgent, unlike the executor persona and the Scope/Language
// entries. A control character in it must be rejected at load validation,
// mirroring the executor persona guard, so a malformed community persona fails
// fast at config validation rather than at runtime resolution.
func TestRegistryLoad_AgentPersonaWithNewline(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    persona: "owasp\nmalicious"
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "persona")
}

// An over-long persona is rejected at load, mirroring the executor persona cap
// (shared MaxExecutorPersonaLen) so prompt-stuffing via the agent persona field
// is bounded the same way as the executor's.
func TestRegistryLoad_AgentPersonaTooLong(t *testing.T) {
	long := strings.Repeat("a", MaxExecutorPersonaLen+1)
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    persona: "`+long+`"
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "persona")
}
