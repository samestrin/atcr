package personas

import (
	"path/filepath"
	"strings"
	"testing"

	builtins "github.com/samestrin/atcr/personas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deliaCommunityYAML mirrors the shipped personas/community/delia.yaml (DeepSeek,
// a flat-rate open model). It is the mock-registry payload for the install step;
// its provider/model are kept in lockstep with the served index entry below so
// InstallUnit's strict community-YAML validation passes.
const deliaCommunityYAML = `name: delia
version: 1.0.0
description: DeepSeek algorithmic-complexity and efficiency reviewer (reasoning-tier lens)
provider: openrouter
model: deepseek/deepseek-v4-pro
persona: delia
role: reviewer
`

// TestDiscoverByModel_EndToEnd_MockRegistry is the Phase 7 AC6 proof (task 7.2):
// it drives the full "I have model X → find and install its persona" flow against
// a mock registry (httptest.NewServer + srv.URL, the CI pattern — zero live
// network): search --model deepseek → install → list → test (fixture). Each step
// is asserted, and discovery is proven to come strictly from the structured Model
// field, never from the model name in free text (the served index Description
// carries no "deepseek" token). The live samestrin/atcr fetch stays deferred until
// the repo is public.
func TestDiscoverByModel_EndToEnd_MockRegistry(t *testing.T) {
	authoredMD, err := builtins.CommunityGet("delia")
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(authoredMD))

	// The index entry carries provider/model as STRUCTURED data; its Description
	// deliberately omits the model token so a match can only come from Model.
	index := `[{"name":"delia","version":"1.0.0",` +
		`"description":"algorithmic-complexity and efficiency reviewer (reasoning-tier lens)",` +
		`"path":"delia.yaml","provider":"openrouter","model":"deepseek/deepseek-v4-pro"}]`
	srv := testServer(t, map[string]string{
		"/index.json": index,
		"/delia.yaml": deliaCommunityYAML,
		"/delia.md":   authoredMD,
	})

	// 1. search --model deepseek → finds delia strictly from structured Model data.
	found, err := SearchWithOptions(srv.Client(), srv.URL, SearchOptions{Model: "deepseek"})
	require.NoError(t, err)
	require.Len(t, found, 1, "search --model deepseek must find exactly the DeepSeek persona")
	require.Equal(t, "delia", found[0].Name)
	require.Equal(t, "deepseek/deepseek-v4-pro", found[0].Model)
	// Structured-only proof: the match did NOT come from free text.
	require.NotContains(t, strings.ToLower(found[0].Description), "deepseek",
		"description must not carry the model token — the match is from structured Model data")

	// 2. install the discovered persona as a self-contained unit (yaml + co-located md).
	destDir := t.TempDir()
	require.NoError(t, InstallUnit(srv.Client(), srv.URL, found[0].Name, destDir))
	assert.FileExists(t, filepath.Join(destDir, "delia.yaml"))
	assert.FileExists(t, filepath.Join(destDir, "delia.md"))

	// 3. list → the installed persona is reported as a community-source row.
	metas, err := List(destDir)
	require.NoError(t, err)
	var listed *PersonaMeta
	for i := range metas {
		if metas[i].Name == "delia" {
			listed = &metas[i]
			break
		}
	}
	require.NotNil(t, listed, "installed persona must appear in `list`")
	assert.Equal(t, "community", listed.Source)

	// 4. test (fixture) → exercise the installed on-disk unit, not the embedded copy.
	runner := TemplateFixtureRunner{
		PersonasDir: func() (string, error) { return destDir, nil },
	}
	out, err := runner.RunFixture("delia")
	require.NoError(t, err)
	require.True(t, out.HasFixture, "delia must resolve a fixture")
	require.Equal(t, out.Total, out.Passed, "delia fixture must pass")
	require.Equal(t, 1, out.Passed, "expected exactly one passing fixture case")
}
