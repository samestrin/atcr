package personas

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strptr(s string) *string { return &s }

func TestCheckDrift_MissingSlug(t *testing.T) {
	f := CheckDrift(
		[]InstalledLock{{Name: "p", Model: "vendor/gone-1.0"}},
		[]CatalogModel{{ID: "vendor/present-1.0", Created: 100}},
	)
	require.Len(t, f, 1)
	assert.Equal(t, ConditionMissing, f[0].Condition)
	assert.Equal(t, "vendor/gone-1.0", f[0].CurrentSlug)
	assert.Empty(t, f[0].SuggestedSlug)
	assert.Empty(t, f[0].ExpirationDate)
}

func TestCheckDrift_NonNullExpiration_Deprecation(t *testing.T) {
	f := CheckDrift(
		[]InstalledLock{{Name: "p", Model: "vendor/m-1.0"}},
		[]CatalogModel{{ID: "vendor/m-1.0", Created: 100, ExpirationDate: strptr("2026-09-01")}},
	)
	require.Len(t, f, 1)
	assert.Equal(t, ConditionDeprecation, f[0].Condition)
	assert.Equal(t, "2026-09-01", f[0].ExpirationDate)
}

func TestCheckDrift_NullExpiration_NoDeprecation(t *testing.T) {
	// Only a non-null expiration_date triggers deprecation (AC 05-04 EC2).
	f := CheckDrift(
		[]InstalledLock{{Name: "p", Model: "vendor/m-1.0"}},
		[]CatalogModel{{ID: "vendor/m-1.0", Created: 100}}, // ExpirationDate nil
	)
	assert.Empty(t, f)
}

func TestCheckDrift_EmptyStringExpiration_NotDeprecated(t *testing.T) {
	f := CheckDrift(
		[]InstalledLock{{Name: "p", Model: "vendor/m-1.0"}},
		[]CatalogModel{{ID: "vendor/m-1.0", Created: 100, ExpirationDate: strptr("  ")}},
	)
	assert.Empty(t, f) // empty/whitespace == JSON null (not deprecated)
}

func TestCheckDrift_EmptyLock_Skipped(t *testing.T) {
	f := CheckDrift(
		[]InstalledLock{{Name: "p", Model: "  "}},
		[]CatalogModel{{ID: "vendor/m-1.0", Created: 100}},
	)
	assert.Empty(t, f)
}

func TestCheckDrift_BindinglessNewerMember_SameTierOnly(t *testing.T) {
	models := []CatalogModel{
		{ID: "vendor/fam-1.0", Created: 100},
		{ID: "vendor/fam-2.0", Created: 200},
		{ID: "vendor/fam-3.0-mini", Created: 900}, // sibling tier, must not bleed
	}
	f := CheckDrift([]InstalledLock{{Name: "p", Model: "vendor/fam-1.0"}}, models)
	require.Len(t, f, 1)
	assert.Equal(t, ConditionNewerMember, f[0].Condition)
	assert.Equal(t, "vendor/fam-2.0", f[0].SuggestedSlug) // not fam-3.0-mini
	assert.Equal(t, "vendor/fam", f[0].Family)
	assert.Equal(t, "stable", f[0].Channel)
}

func TestCheckDrift_BoundPersona_NewerMemberViaResolver(t *testing.T) {
	models, err := SnapshotModels()
	require.NoError(t, err)
	// z-ai/glm-4.5 is deprecated in the snapshot; the glm@stable binding resolves
	// the newest stable z-ai/ member (glm-5.2).
	f := CheckDrift([]InstalledLock{{Name: "glenna", Model: "z-ai/glm-4.5", Binding: "glm@stable"}}, models)
	byCond := map[string]DriftFinding{}
	for _, x := range f {
		byCond[x.Condition] = x
	}
	nm, ok := byCond[ConditionNewerMember]
	require.True(t, ok, "expected a newer-member finding")
	assert.Equal(t, "z-ai/glm-5.2", nm.SuggestedSlug)
	assert.Equal(t, "glm", nm.Family)
	assert.Equal(t, "stable", nm.Channel)
	_, dep := byCond[ConditionDeprecation]
	assert.True(t, dep, "expected a deprecation finding")
}

func TestCheckDrift_DeterministicOrder(t *testing.T) {
	models := []CatalogModel{
		{ID: "vendor/fam-1.0", Created: 100, ExpirationDate: strptr("2026-01-01")},
		{ID: "vendor/fam-2.0", Created: 200},
	}
	locks := []InstalledLock{{Name: "p", Model: "vendor/fam-1.0"}}
	first := CheckDrift(locks, models)
	// newer-member before deprecation, stable across repeated calls.
	require.Len(t, first, 2)
	assert.Equal(t, ConditionNewerMember, first[0].Condition)
	assert.Equal(t, ConditionDeprecation, first[1].Condition)
	for i := 0; i < 5; i++ {
		assert.Equal(t, first, CheckDrift(locks, models))
	}
}

func TestSnapshotModels_RoundTrip(t *testing.T) {
	models, err := SnapshotModels()
	require.NoError(t, err)
	require.NotEmpty(t, models)
	byID := make(map[string]CatalogModel, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}
	// A concrete pinned slug, an alias, and a deprecated entry all survive the loader.
	opus, ok := byID["anthropic/claude-opus-4.8"]
	require.True(t, ok)
	assert.Nil(t, opus.ExpirationDate) // JSON null → nil (not deprecated)
	assert.Greater(t, opus.Created, int64(0))

	_, ok = byID["~anthropic/claude-opus-latest"]
	assert.True(t, ok, "alias entry present")

	glm45, ok := byID["z-ai/glm-4.5"]
	require.True(t, ok)
	require.NotNil(t, glm45.ExpirationDate)
	assert.Equal(t, "2026-12-31", *glm45.ExpirationDate)
}

func TestSnapshotModels_EnvOverride_SizeCap(t *testing.T) {
	// An env-override snapshot path must be size-bounded like the network path,
	// so a FIFO or multi-GB file cannot exhaust memory.
	dir := t.TempDir()
	big := filepath.Join(dir, "oversized.json")
	data := make([]byte, fetchBodyLimit+1)
	require.NoError(t, os.WriteFile(big, data, 0o644))

	t.Setenv(envCatalogSnapshot, big)
	_, err := SnapshotModels()
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to load catalog snapshot")
}

// TestDeriveFamilyPrefix_NonTrailingVersionSegment: a version segment that is NOT
// the last hyphen segment (a tiered slug like openai/gpt-5.4-mini) must still be
// stripped so the slug derives to its family tier (openai/gpt-mini) and groups with
// a newer sibling — not to itself, which only ever matches itself and reports no
// drift forever. Trailing-version and no-version slugs keep their existing output.
func TestDeriveFamilyPrefix_NonTrailingVersionSegment(t *testing.T) {
	cases := []struct{ slug, want string }{
		{"openai/gpt-5.4-mini", "openai/gpt-mini"},                       // non-trailing version stripped
		{"anthropic/claude-opus-4.8", "anthropic/claude-opus"},           // trailing version (unchanged)
		{"z-ai/glm-5.2", "z-ai/glm"},                                     // trailing version (unchanged)
		{"anthropic/claude-opus-latest", "anthropic/claude-opus-latest"}, // no version segment (unchanged)
	}
	for _, c := range cases {
		assert.Equal(t, c.want, deriveFamilyPrefix(c.slug), "deriveFamilyPrefix(%q)", c.slug)
	}
}

// TestCheckDrift_AliasSlugAbsentFromCatalog_NoFalseMissing: an alias-bound persona
// locks to a synthetic ~vendor/…-latest slug that the provider resolves server-side.
// A refreshed catalog snapshot need not list that id, so an alias lock absent from
// the catalog must NOT be reported `missing` (it stays resolvable) — unlike a
// genuine concrete slug that vanished (TD-005).
func TestCheckDrift_AliasSlugAbsentFromCatalog_NoFalseMissing(t *testing.T) {
	f := CheckDrift(
		[]InstalledLock{{Name: "p", Model: "~anthropic/claude-opus-latest"}},
		[]CatalogModel{{ID: "anthropic/claude-opus-4.8", Created: 100}},
	)
	assert.Empty(t, f, "an alias slug absent from the catalog is not a false missing")
}

// TestCheckDrift_LocalProviderSlug_NoFalseMissing covers the local-persona
// exemption (Epic 27.0): a community persona bound to a local endpoint carries a
// local/<model> slug that the OpenRouter catalog snapshot never lists. Such a slug
// is resolved by the user's own local server (ollama/llama.cpp/vllm), not the
// catalog, so it must never be reported `missing` — the same reasoning as the
// alias-slug exemption above (TD-005).
func TestCheckDrift_LocalProviderSlug_NoFalseMissing(t *testing.T) {
	f := CheckDrift(
		[]InstalledLock{{Name: "gerald", Model: "local/gemma3-27b"}},
		[]CatalogModel{{ID: "anthropic/claude-opus-4.8", Created: 100}},
	)
	assert.Empty(t, f, "a local/<model> slug absent from the catalog is not a false missing")
}
