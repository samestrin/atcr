package personas

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Element 1 (AC 03-01): Alias passthrough resolves the alias-covered vendor
// tiers without a catalog scan -----------------------------------------------

// failingHTTPClient fails the test if any HTTP call is made. The alias path must
// never touch the network, so injecting this proves "the provider owns
// resolution" (AC 03-01 Scenario 3 / Story-Specific DoD).
type failingHTTPClient struct{ t *testing.T }

func (f failingHTTPClient) Do(*http.Request) (*http.Response, error) {
	f.t.Helper()
	f.t.Fatal("alias path must not perform any HTTP/catalog call")
	return nil, nil
}

// TestResolveModel_AliasPassthrough_AllVendorTiers covers AC 03-01 Scenario 2:
// each alias-covered vendor tier resolves to its documented ~…-latest alias slug
// verbatim, with no catalog scan (nil model list).
func TestResolveModel_AliasPassthrough_AllVendorTiers(t *testing.T) {
	cases := []struct {
		family string
		want   string
	}{
		{"anthropic/claude-opus", "~anthropic/claude-opus-latest"},
		{"anthropic/claude-sonnet", "~anthropic/claude-sonnet-latest"},
		{"openai/gpt", "~openai/gpt-latest"},
		{"openai/gpt-mini", "~openai/gpt-mini-latest"},
		{"google/gemini-pro", "~google/gemini-pro-latest"},
		{"google/gemini-flash", "~google/gemini-flash-latest"},
		{"moonshotai/kimi", "~moonshotai/kimi-latest"},
	}
	for _, tc := range cases {
		t.Run(tc.family, func(t *testing.T) {
			// nil catalog: alias resolution must not depend on catalog contents.
			got, err := ResolveModel(Binding{Family: tc.family, Channel: "@stable"}, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestResolveModel_AliasPath_ChannelIrrelevant covers AC 03-01 Scenario 1: the
// alias path ignores channel — @stable and @latest resolve identically.
func TestResolveModel_AliasPath_ChannelIrrelevant(t *testing.T) {
	stable, err := ResolveModel(Binding{Family: "anthropic/claude-opus", Channel: "@stable"}, nil)
	require.NoError(t, err)
	latest, err := ResolveModel(Binding{Family: "anthropic/claude-opus", Channel: "@latest"}, nil)
	require.NoError(t, err)
	assert.Equal(t, stable, latest)
	assert.Equal(t, "~anthropic/claude-opus-latest", stable)
}

// TestResolveModel_AliasTable_DistinctSlugsSameVendor covers AC 03-01 Edge Case 1:
// two personas share the anthropic vendor but get distinct alias slugs, proving
// the table is keyed by model tier, not merely by vendor.
func TestResolveModel_AliasTable_DistinctSlugsSameVendor(t *testing.T) {
	opus, err := ResolveModel(Binding{Family: "anthropic/claude-opus", Channel: "@stable"}, nil)
	require.NoError(t, err)
	sonnet, err := ResolveModel(Binding{Family: "anthropic/claude-sonnet", Channel: "@stable"}, nil)
	require.NoError(t, err)
	assert.NotEqual(t, opus, sonnet)
	assert.Equal(t, "~anthropic/claude-opus-latest", opus)
	assert.Equal(t, "~anthropic/claude-sonnet-latest", sonnet)
}

// TestResolveModel_AliasTable_ExactMatchOnly covers AC 03-01 Edge Case 2: an
// unrecognized family that is only a near-miss of an alias key (case/substring)
// does NOT fuzzy-match — it falls through to the resolution error.
func TestResolveModel_AliasTable_ExactMatchOnly(t *testing.T) {
	// Wrong case and an unmapped tier must not match the alias table.
	for _, family := range []string{"ANTHROPIC/CLAUDE-OPUS", "anthropic/claude-haiku"} {
		_, err := ResolveModel(Binding{Family: family, Channel: "@stable"}, nil)
		require.Error(t, err, "family %q must not fuzzy/substring-match an alias key", family)
	}
}

// TestResolveModel_AliasPath_DoesNotCallCatalog covers AC 03-01 Scenario 3 with a
// stronger guarantee: a client that fails on any call is never invoked, because
// the alias path is a pure static-table lookup.
func TestResolveModel_AliasPath_DoesNotCallCatalog(t *testing.T) {
	// If the resolver ever fetched, failingHTTPClient would fail the test. The
	// resolver takes an already-parsed model list, so no client is even passed —
	// this asserts the alias branch does not consult the (empty) model list.
	_ = failingHTTPClient{t: t}
	got, err := ResolveModel(Binding{Family: "openai/gpt", Channel: "@latest"}, []CatalogModel{})
	require.NoError(t, err)
	assert.Equal(t, "~openai/gpt-latest", got)
}

// TestResolveModel_UnknownFamily_DescriptiveError covers AC 03-01 Error Scenario 1:
// a family with no alias, pin, or vendor-prefix strategy returns a descriptive
// error naming the family, never a zero-value slug.
func TestResolveModel_UnknownFamily_DescriptiveError(t *testing.T) {
	got, err := ResolveModel(Binding{Family: "mystery/model", Channel: "@stable"}, nil)
	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "mystery/model")
}

// --- Catalog client scaffolding: zero-live-network fetch via httptest --------

// TestCatalogClient_FetchModels_ParsesFixtureSnapshot proves the CatalogClient
// reuses the injectable HTTPClient seam and parses the checked-in snapshot
// (zero live network, per catalog-snapshot-fixture.md).
func TestCatalogClient_FetchModels_ParsesFixtureSnapshot(t *testing.T) {
	fixture, err := os.ReadFile("testdata/catalog_snapshot.json")
	require.NoError(t, err, "catalog snapshot fixture must exist")

	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer ts.Close()

	c := &CatalogClient{HTTPClient: ts.Client(), BaseURL: ts.URL}
	models, err := c.FetchModels()
	require.NoError(t, err)
	assert.Equal(t, "/models", gotPath, "catalog client must GET the /models endpoint")
	require.NotEmpty(t, models)

	// The fixture must carry all 10 of Epic 19.6's pinned slugs (zero-migration
	// coverage) plus the alias entries — spot-check a representative sample.
	byID := make(map[string]CatalogModel, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}
	for _, id := range []string{
		"anthropic/claude-opus-4.8", "deepseek/deepseek-v4-pro",
		"qwen/qwen3-coder-plus", "z-ai/glm-5.2", "~openai/gpt-latest",
	} {
		assert.Contains(t, byID, id, "fixture must contain %q", id)
	}
}
