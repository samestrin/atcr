package personas

import (
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
