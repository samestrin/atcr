package personas

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Element 1 (AC 03-01): Alias passthrough resolves the alias-covered vendor
// tiers without a catalog scan -----------------------------------------------

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

// TestResolveModel_AliasPath_IgnoresCatalogContents covers AC 03-01 Scenario 3:
// the alias path is a pure static-table lookup that never reads the catalog, so
// it resolves identically against a nil, empty, or populated model list. The
// resolver holds no HTTPClient, so there is structurally no catalog call to make.
func TestResolveModel_AliasPath_IgnoresCatalogContents(t *testing.T) {
	arbitrary := []CatalogModel{{ID: "unrelated/model", Created: 1}}
	for _, models := range [][]CatalogModel{nil, {}, arbitrary} {
		got, err := ResolveModel(Binding{Family: "openai/gpt", Channel: "@latest"}, models)
		require.NoError(t, err)
		assert.Equal(t, "~openai/gpt-latest", got)
	}
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

// cm builds a clean (non-preview, non-expiring) catalog model for scan tests.
func cm(id string, created int64) CatalogModel {
	return CatalogModel{ID: id, CanonicalSlug: id, Created: created}
}

// --- Element 2 (AC 03-02): created-timestamp newest-in-vendor-prefix scan -----

// TestResolveModel_CreatedScan_NewestPerPrefix covers AC 03-02 Scenarios 1 & 2:
// delia/qwen resolve to the numerically-largest `created` entry under their
// vendor prefix; a higher-`created` entry under a DIFFERENT prefix is ignored.
func TestResolveModel_CreatedScan_NewestPerPrefix(t *testing.T) {
	models := []CatalogModel{
		cm("deepseek/deepseek-v3", 100),
		cm("deepseek/deepseek-v4-pro", 300),
		cm("deepseek/deepseek-v3.5", 200),
		cm("qwen/qwen3-a", 150),
		cm("qwen/qwen3.7-plus", 500),
		cm("unrelated/model", 999), // higher created, wrong prefix → ignored
	}
	got, err := ResolveModel(Binding{Family: "deepseek", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.Equal(t, "deepseek/deepseek-v4-pro", got)

	got, err = ResolveModel(Binding{Family: "qwen", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.Equal(t, "qwen/qwen3.7-plus", got)
}

// TestResolveModel_CreatedScan_Glenna_ZaiPrefixNotGlm covers AC 03-02 Scenario 3
// + Edge Case 1: family "glm" scans the "z-ai/" namespace, never "glm/". A decoy
// "glm/"-prefixed entry with a huge `created` must NOT be selected.
func TestResolveModel_CreatedScan_Glenna_ZaiPrefixNotGlm(t *testing.T) {
	models := []CatalogModel{
		cm("z-ai/glm-5.1", 100),
		cm("z-ai/glm-5.2", 200),
		cm("glm/glm-9", 999), // decoy: if the resolver wrongly used "glm/" it would pick this
	}
	got, err := ResolveModel(Binding{Family: "glm", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.Equal(t, "z-ai/glm-5.2", got, "family glm must resolve against z-ai/, never glm/")
}

// TestResolveModel_CreatedScan_ExactPrefixNoEvilCollision covers AC 03-02 Security:
// prefix matching is exact-prefix, so "z-ai-evil/..." cannot be mistaken for "z-ai/".
func TestResolveModel_CreatedScan_ExactPrefixNoEvilCollision(t *testing.T) {
	models := []CatalogModel{
		cm("z-ai-evil/glm-hack", 999),
		cm("z-ai/glm-5.2", 200),
	}
	got, err := ResolveModel(Binding{Family: "glm", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.Equal(t, "z-ai/glm-5.2", got)
}

// TestResolveModel_CreatedScan_TieBreakDescLexicographic covers AC 03-02 Edge
// Case 3: two entries tie on `created` → the lexicographically greater slug wins,
// deterministically and independent of catalog array order (asserted against a
// reversed copy).
func TestResolveModel_CreatedScan_TieBreakDescLexicographic(t *testing.T) {
	forward := []CatalogModel{
		cm("qwen/qwen3-a", 500),
		cm("qwen/qwen3-b", 500),
		cm("qwen/qwen3-c", 500),
	}
	reversed := []CatalogModel{forward[2], forward[1], forward[0]}
	gotF, err := ResolveModel(Binding{Family: "qwen", Channel: "@stable"}, forward)
	require.NoError(t, err)
	gotR, err := ResolveModel(Binding{Family: "qwen", Channel: "@stable"}, reversed)
	require.NoError(t, err)
	assert.Equal(t, "qwen/qwen3-c", gotF, "highest lexicographic slug wins a created tie")
	assert.Equal(t, gotF, gotR, "tie-break is independent of array order")
}

// TestResolveModel_CreatedScan_Singleton covers AC 03-02 Edge Case 2: exactly one
// eligible entry under the prefix resolves without ambiguity handling.
func TestResolveModel_CreatedScan_Singleton(t *testing.T) {
	got, err := ResolveModel(Binding{Family: "deepseek", Channel: "@stable"},
		[]CatalogModel{cm("deepseek/only", 42), cm("qwen/other", 99)})
	require.NoError(t, err)
	assert.Equal(t, "deepseek/only", got)
}

// TestResolveModel_CreatedScan_IneligibleCreatedExcluded covers AC 03-02 Edge
// Case 4: an entry with an absent/zero `created` is never selected; selection
// proceeds among the remaining valid entries.
func TestResolveModel_CreatedScan_IneligibleCreatedExcluded(t *testing.T) {
	models := []CatalogModel{
		cm("deepseek/no-created", 0), // ineligible even though it could look "newest"
		cm("deepseek/valid", 100),
	}
	got, err := ResolveModel(Binding{Family: "deepseek", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.Equal(t, "deepseek/valid", got)
}

// TestResolveModel_CreatedScan_NoEligible_Error covers AC 03-02 Edge Case 4 tail +
// Error Scenario 1: when no entry under the prefix has a valid `created`, the
// resolver fails closed with a descriptive error naming the prefix and family —
// never a silent empty slug.
func TestResolveModel_CreatedScan_NoEligible_Error(t *testing.T) {
	// No deepseek/ entry at all.
	got, err := ResolveModel(Binding{Family: "deepseek", Channel: "@stable"},
		[]CatalogModel{cm("qwen/x", 100)})
	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "deepseek/")
	assert.Contains(t, err.Error(), "deepseek")

	// All z-ai/ entries have ineligible created → still fails closed.
	_, err = ResolveModel(Binding{Family: "glm", Channel: "@stable"},
		[]CatalogModel{cm("z-ai/glm-5.2", 0)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "z-ai/")
}

// TestCatalogClient_FetchModels_MalformedJSON covers the parse-failure path:
// invalid catalog JSON returns a wrapped, descriptive error, not a partial list.
func TestCatalogClient_FetchModels_MalformedJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data": [ {"id": "x/y", "created": ` + "\n"))
	}))
	defer ts.Close()

	c := &CatalogClient{HTTPClient: ts.Client(), BaseURL: ts.URL}
	_, err := c.FetchModels()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse model catalog")
}

// TestCatalogModel_TolerantCreated proves a missing, zero, non-numeric, or
// numeric-string `created` on ONE entry degrades that entry's Created to 0
// (ineligible) without aborting the parse of the whole array (AC 03-02 EC4).
func TestCatalogModel_TolerantCreated(t *testing.T) {
	raw := []byte(`{"data": [
		{"id": "a/1", "created": 1777000679},
		{"id": "a/2", "created": "1780000000"},
		{"id": "a/3", "created": "not-a-number"},
		{"id": "a/4", "created": true},
		{"id": "a/5"}
	]}`)
	var resp struct {
		Data []CatalogModel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &resp), "one bad created must not abort the array")
	require.Len(t, resp.Data, 5)
	got := map[string]int64{}
	for _, m := range resp.Data {
		got[m.ID] = m.Created
	}
	assert.Equal(t, int64(1777000679), got["a/1"], "numeric created parses")
	assert.Equal(t, int64(1780000000), got["a/2"], "numeric-string created parses")
	assert.Equal(t, int64(0), got["a/3"], "non-numeric string → 0")
	assert.Equal(t, int64(0), got["a/4"], "bool → 0")
	assert.Equal(t, int64(0), got["a/5"], "absent → 0")
}
