package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// payload_byte_budget did double duty: the global payload cap in
// ApplyByteBudgetPreferEscalated AND the per-chunk line cap via
// ClampLinesToByteBudget. The two jobs want opposite values — lower it to protect a
// small-window model and it silently sheds files from the payload; raise it to
// preserve the payload and every agent's chunks grow. On the 2026-08-15 run 65536
// flattened all 13 agents to 1365 lines/chunk AND shed 40 files.
//
// chunk_byte_budget splits them. INHERITANCE is what keeps this from being a
// behavior change: unset at every tier, it resolves to whatever payload_byte_budget
// resolved to, so an existing config is sized exactly as before and only an operator
// who sets it separately gets the decoupled behavior.
func TestResolveSettings_ChunkByteBudget_InheritsPayloadByteBudgetWhenUnset(t *testing.T) {
	s, err := ResolveSettings(CLIOverrides{}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, DefaultPayloadByteBudget, s.ChunkByteBudget,
		"no tier set it, so it inherits the resolved payload budget — not a window-derived size")

	custom := int64(65536)
	s, err = ResolveSettings(CLIOverrides{}, &ProjectConfig{PayloadByteBudget: &custom}, nil)
	require.NoError(t, err)
	assert.Equal(t, custom, s.ChunkByteBudget,
		"inheritance tracks the RESOLVED payload budget, not the embedded default")

	// The CLI tier resolves last; inheritance must observe its value too, or
	// `--byte-budget` would silently stop governing chunk sizing.
	cliBudget := int64(4096)
	s, err = ResolveSettings(CLIOverrides{PayloadByteBudget: &cliBudget}, &ProjectConfig{PayloadByteBudget: &custom}, nil)
	require.NoError(t, err)
	assert.Equal(t, cliBudget, s.ChunkByteBudget)
}

// Set explicitly, it decouples: the payload keeps its own (large) cap while chunks
// are sized to something a small-window model can hold — the configuration that was
// unreachable while one key served both.
func TestResolveSettings_ChunkByteBudget_ExplicitValueDecouplesFromThePayloadCap(t *testing.T) {
	payloadBudget := int64(524288)
	chunkBudget := int64(65536)

	s, err := ResolveSettings(CLIOverrides{}, &ProjectConfig{
		PayloadByteBudget: &payloadBudget,
		ChunkByteBudget:   &chunkBudget,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, payloadBudget, s.PayloadByteBudget)
	assert.Equal(t, chunkBudget, s.ChunkByteBudget, "an explicit value must not be overwritten by inheritance")

	// A pointer, so an explicit 0 — the documented unlimited escape hatch every
	// sibling budget honors — survives rather than reading as unset and inheriting.
	zero := int64(0)
	s, err = ResolveSettings(CLIOverrides{}, &ProjectConfig{
		PayloadByteBudget: &payloadBudget,
		ChunkByteBudget:   &zero,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(0), s.ChunkByteBudget,
		"explicit 0 is unlimited chunk sizing, NOT unset")
}

// The project tier overrides the registry tier, like every sibling budget.
func TestResolveSettings_ChunkByteBudget_ProjectTierWinsOverRegistry(t *testing.T) {
	regBudget := int64(200000)
	projBudget := int64(100000)
	s, err := ResolveSettings(CLIOverrides{},
		&ProjectConfig{ChunkByteBudget: &projBudget},
		&Registry{ChunkByteBudget: &regBudget})
	require.NoError(t, err)
	assert.Equal(t, projBudget, s.ChunkByteBudget)

	s, err = ResolveSettings(CLIOverrides{}, nil, &Registry{ChunkByteBudget: &regBudget})
	require.NoError(t, err)
	assert.Equal(t, regBudget, s.ChunkByteBudget, "the registry tier applies when the project is silent")
}

// Negative is invalid at every tier, exactly as payload_byte_budget is.
func TestChunkByteBudget_RejectsNegative(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nchunk_byte_budget: -1\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chunk_byte_budget")

	neg := int64(-1)
	_, err = ResolveSettings(CLIOverrides{}, &ProjectConfig{ChunkByteBudget: &neg}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chunk_byte_budget")
}

// The generated config must document the knob AND the fact that it inherits, since
// an operator who sees only payload_byte_budget has no way to learn that lowering it
// also shrinks every chunk.
func TestDefaultProjectConfigYAML_DocumentsChunkByteBudget(t *testing.T) {
	out := DefaultProjectConfigYAML([]string{"bruce"})
	assert.Contains(t, out, "chunk_byte_budget", "the knob must appear in the generated config")
	assert.Contains(t, out, "# chunk_byte_budget:", "it must carry a help comment like its siblings")

	// The baseline caveat. `atcr review --all/--dir` partitions per agent
	// unconditionally — a mechanism distinct from review_strategy: chunked, which
	// buildSlots gates off for baseline scans (`== chunked && !baseline`). Both
	// mechanisms are called "chunking", and only the review_strategy one reads this
	// key, so an operator who sets it expecting --all to chunk smaller is silently
	// wrong. Pinned here because that confusion was filed as a defect once already.
	assert.Contains(t, out, "baseline scan",
		"the comment must say the key is not consulted on a baseline scan")
	assert.Contains(t, out, "--all/--dir",
		"...and name the invocations it does not govern")

	// Commented out by default: an emitted value would freeze the inheritance the
	// answer chose, so a later change to payload_byte_budget would stop reaching
	// chunk sizing for every config generated before that change.
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	path := DefaultProjectConfigPath(root)
	require.NoError(t, os.WriteFile(path, []byte(out), 0o644))
	cfg, err := LoadProjectConfig(path)
	require.NoError(t, err)
	assert.Nil(t, cfg.ChunkByteBudget, "the template must leave it unset so the default stays inheritance")

	s, err := ResolveSettings(CLIOverrides{}, cfg, nil)
	require.NoError(t, err)
	assert.Equal(t, s.PayloadByteBudget, s.ChunkByteBudget,
		"a freshly generated config must behave exactly as it did before this key existed")
}
