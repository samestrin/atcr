package personas

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

// cmExp builds a catalog model carrying an expiration_date string (deprecation
// signal). Pass "" to model a JSON empty-string expiration_date.
func cmExp(id string, created int64, exp string) CatalogModel {
	return CatalogModel{ID: id, CanonicalSlug: id, Created: created, ExpirationDate: &exp}
}

// --- Element 5 (AC 03-05): @latest includes preview, still excludes deprecated --

// TestResolveModel_Latest_IncludesPreviewNewest covers AC 03-05 Scenario 1: under
// @latest the preview-tagged newest entry that @stable skips IS selected —
// directly contrasting with the @stable case over the identical fixture.
func TestResolveModel_Latest_IncludesPreviewNewest(t *testing.T) {
	models := []CatalogModel{
		cm("deepseek/deepseek-v5-preview", 300),
		cm("deepseek/deepseek-v4-pro", 200),
	}
	gotLatest, err := ResolveModel(Binding{Family: "deepseek", Channel: "@latest"}, models)
	require.NoError(t, err)
	assert.Equal(t, "deepseek/deepseek-v5-preview", gotLatest, "@latest includes the preview newest")

	gotStable, err := ResolveModel(Binding{Family: "deepseek", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.Equal(t, "deepseek/deepseek-v4-pro", gotStable, "@stable excludes it (contrast)")
}

// TestResolveModel_Latest_NewestAmongAll covers AC 03-05 Scenario 2: @latest still
// selects exactly the single newest-by-created among all (preview or not) — it
// widens eligibility, it does not "return every preview build".
func TestResolveModel_Latest_NewestAmongAll(t *testing.T) {
	models := []CatalogModel{
		cm("qwen/qwen-preview-a", 100),
		cm("qwen/qwen-preview-b", 300), // newest overall (preview)
		cm("qwen/qwen-stable-c", 200),
	}
	got, err := ResolveModel(Binding{Family: "qwen", Channel: "@latest"}, models)
	require.NoError(t, err)
	assert.Equal(t, "qwen/qwen-preview-b", got)
}

// TestResolveModel_Latest_CleanPrefixSameAsStable covers AC 03-05 Scenario 3:
// with no preview/expiring entries, @latest and @stable return the identical slug
// (@latest is a strict superset of @stable's eligible set).
func TestResolveModel_Latest_CleanPrefixSameAsStable(t *testing.T) {
	models := []CatalogModel{cm("z-ai/glm-5.1", 100), cm("z-ai/glm-5.2", 200)}
	gotLatest, err := ResolveModel(Binding{Family: "glm", Channel: "@latest"}, models)
	require.NoError(t, err)
	gotStable, err := ResolveModel(Binding{Family: "glm", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.Equal(t, gotStable, gotLatest)
	assert.Equal(t, "z-ai/glm-5.2", gotLatest)
}

// TestResolveModel_Latest_StillExcludesDeprecated covers AC 03-05 Edge Case 1:
// @latest bypasses ONLY the preview-token exclusion; a non-null expiration_date
// (deprecation) is STILL excluded, failing over to the next-newest non-expiring
// entry (or failing closed if none).
func TestResolveModel_Latest_StillExcludesDeprecated(t *testing.T) {
	// Newest is deprecated (non-preview) → skipped even under @latest.
	models := []CatalogModel{
		cmExp("deepseek/deepseek-v5", 300, "2027-01-01"), // newest, deprecated → excluded
		cm("deepseek/deepseek-v4-pro", 200),              // non-expiring
	}
	got, err := ResolveModel(Binding{Family: "deepseek", Channel: "@latest"}, models)
	require.NoError(t, err)
	assert.Equal(t, "deepseek/deepseek-v4-pro", got, "@latest still excludes deprecated")

	// A preview newest that is ALSO deprecated is excluded under @latest too;
	// only the non-deprecated (preview) member survives.
	models2 := []CatalogModel{
		cmExp("deepseek/deepseek-v6-preview", 400, "2027-01-01"), // preview + deprecated → excluded
		cm("deepseek/deepseek-v5-preview", 300),                  // preview, non-expiring → selected
	}
	got, err = ResolveModel(Binding{Family: "deepseek", Channel: "@latest"}, models2)
	require.NoError(t, err)
	assert.Equal(t, "deepseek/deepseek-v5-preview", got)

	// All deprecated → fail closed even under @latest.
	_, err = ResolveModel(Binding{Family: "deepseek", Channel: "@latest"},
		[]CatalogModel{cmExp("deepseek/only", 100, "2027-01-01")})
	require.Error(t, err)
}

// TestResolveModel_Latest_AliasUnaffected covers AC 03-05 Edge Case 2: an
// alias-covered persona ignores channel entirely — @latest is a no-op there.
func TestResolveModel_Latest_AliasUnaffected(t *testing.T) {
	got, err := ResolveModel(Binding{Family: "google/gemini-pro", Channel: "@latest"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "~google/gemini-pro-latest", got)
}

// TestResolveModel_AgainstFixture_ScanFamilies is the fixture↔resolver contract
// guard (Phase 3 gate): it feeds the checked-in catalog_snapshot.json through
// ResolveModel for the created-timestamp families and asserts the end-to-end
// result, so a future fixture edit or filter regression that would break Phase 4's
// zero-migration cannot ship green. It also documents, against the REAL fixture,
// why quinn is an explicit-pin (not created-timestamp) persona: the newest qwen/
// member is the general qwen3.7-plus, NOT the coder pin.
func TestResolveModel_AgainstFixture_ScanFamilies(t *testing.T) {
	fixture, err := os.ReadFile("testdata/catalog_snapshot.json")
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(fixture)
	}))
	defer ts.Close()
	models, err := (&CatalogClient{HTTPClient: ts.Client(), BaseURL: ts.URL}).FetchModels()
	require.NoError(t, err)

	// delia + glenna: newest eligible in-prefix EQUALS the 19.6 pin under @stable
	// (deepseek: newest v5-pro deprecation-excluded, v3.2-exp preview-excluded,
	// legacy created:0 ineligible → v4-pro; z-ai glm-4.5 + glm-5v-turbo deprecation-
	// excluded → glm-5.2) — proving the seed-lock zero-migration Phase 4 depends on.
	for _, tc := range []struct{ family, channel, want string }{
		{"deepseek", "@stable", "deepseek/deepseek-v4-pro"},
		{"deepseek", "@latest", "deepseek/deepseek-v4-pro"}, // exp member older than v4-pro
		{"glm", "@stable", "z-ai/glm-5.2"},
		{"glm", "@latest", "z-ai/glm-5.2"}, // turbo/4.5 deprecated → excluded under both
	} {
		got, err := ResolveModel(Binding{Family: tc.family, Channel: tc.channel}, models)
		require.NoError(t, err, "%s %s", tc.family, tc.channel)
		assert.Equal(t, tc.want, got, "%s %s resolves to the 19.6 pin", tc.family, tc.channel)
	}

	// quinn: scanning qwen/ yields the general newest (qwen3.7-plus), NOT the coder
	// pin — this is precisely why the spike reclassified quinn to explicit-pin.
	got, err := ResolveModel(Binding{Family: "qwen", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.Equal(t, "qwen/qwen3.7-plus", got)
	assert.NotEqual(t, "qwen/qwen3-coder-plus", got,
		"newest-in-prefix drops the coder specialization → quinn must pin explicitly")
}

// TestResolveModel_UnrecognizedChannel_Error covers AC 03-05 Error Scenario 1: a
// channel that is neither @stable nor @latest fails closed with a descriptive
// error rather than silently defaulting.
func TestResolveModel_UnrecognizedChannel_Error(t *testing.T) {
	for _, ch := range []string{"@edge", "stable", "@Stable", "latest"} {
		got, err := ResolveModel(Binding{Family: "deepseek", Channel: ch},
			[]CatalogModel{cm("deepseek/deepseek-v4-pro", 200)})
		require.Error(t, err, "channel %q must be rejected", ch)
		assert.Empty(t, got)
		assert.Contains(t, err.Error(), "channel")
	}
}

// TestResolveModel_EmptyChannel_DefaultsStable confirms an omitted or
// whitespace-only channel decodes as the @stable default on the scan path.
func TestResolveModel_EmptyChannel_DefaultsStable(t *testing.T) {
	models := []CatalogModel{
		cm("deepseek/deepseek-v5-preview", 300),
		cm("deepseek/deepseek-v4-pro", 200),
	}
	for _, ch := range []string{"", "   ", "\t"} {
		got, err := ResolveModel(Binding{Family: "deepseek", Channel: ch}, models)
		require.NoError(t, err)
		assert.Equal(t, "deepseek/deepseek-v4-pro", got, "channel %q defaults to @stable", ch)
	}
}

// TestResolveModel_Channel_WhitespaceTrimmed confirms a valid channel with
// surrounding whitespace is trimmed and honored (e.g. "  @latest  " includes preview).
func TestResolveModel_Channel_WhitespaceTrimmed(t *testing.T) {
	models := []CatalogModel{
		cm("deepseek/deepseek-v5-preview", 300),
		cm("deepseek/deepseek-v4-pro", 200),
	}
	got, err := ResolveModel(Binding{Family: "deepseek", Channel: "  @latest  "}, models)
	require.NoError(t, err)
	assert.Equal(t, "deepseek/deepseek-v5-preview", got, "padded @latest must be trimmed and honored")
}

// TestResolveModel_InvalidChannel_IgnoredOnAliasAndPin confirms channel is
// consulted ONLY on the created-timestamp scan path: an unrecognized channel on
// an alias or pin binding is ignored (resolves without error), because both
// strategies short-circuit before channel validation.
func TestResolveModel_InvalidChannel_IgnoredOnAliasAndPin(t *testing.T) {
	got, err := ResolveModel(Binding{Family: "google/gemini-pro", Channel: "@edge"}, nil)
	require.NoError(t, err, "invalid channel must not error on the alias path")
	assert.Equal(t, "~google/gemini-pro-latest", got)

	got, err = ResolveModel(Binding{Pin: "custom/model", Channel: "@edge"}, nil)
	require.NoError(t, err, "invalid channel must not error on the pin path")
	assert.Equal(t, "custom/model", got)
}

// --- Element 4 (AC 03-04): @stable excludes preview/beta/exp and expiring -----

// TestResolveModel_Stable_SkipsPreviewNewest covers AC 03-04 Scenario 1 + Edge
// Case 1: under @stable the newest-by-created entry is skipped when its slug
// carries a preview token (including a `-preview-01` suffixed form), and the
// next-newest eligible entry is selected.
func TestResolveModel_Stable_SkipsPreviewNewest(t *testing.T) {
	for _, previewID := range []string{
		"deepseek/deepseek-v5-preview",
		"deepseek/deepseek-v5-preview-01",
		"deepseek/deepseek-v5-beta",
		"deepseek/deepseek-v5-exp",
	} {
		models := []CatalogModel{
			cm(previewID, 300),                  // newest, but preview → excluded under @stable
			cm("deepseek/deepseek-v4-pro", 200), // next-newest, clean
		}
		got, err := ResolveModel(Binding{Family: "deepseek", Channel: "@stable"}, models)
		require.NoError(t, err)
		assert.Equal(t, "deepseek/deepseek-v4-pro", got, "@stable must skip preview newest %q", previewID)
	}
}

// TestResolveModel_Stable_TD001_StripsVariantSuffix covers TD-001: the preview
// segment match normalizes away a `:variant` suffix before tokenizing, so a
// `…-preview:free` slug is still excluded under @stable.
func TestResolveModel_Stable_TD001_StripsVariantSuffix(t *testing.T) {
	models := []CatalogModel{
		cm("deepseek/deepseek-v5-preview:free", 300), // preview + variant suffix → excluded
		cm("deepseek/deepseek-v4-pro", 200),
	}
	got, err := ResolveModel(Binding{Family: "deepseek", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.Equal(t, "deepseek/deepseek-v4-pro", got)
}

// TestResolveModel_Stable_SkipsExpiringNewest covers AC 03-04 Scenario 2: under
// @stable the newest entry with a non-null expiration_date (deprecation) is
// skipped in favor of the older non-expiring entry.
func TestResolveModel_Stable_SkipsExpiringNewest(t *testing.T) {
	models := []CatalogModel{
		cmExp("qwen/qwen3.8-plus", 300, "2027-01-01"), // newest, expiring → excluded
		cm("qwen/qwen3.7-plus", 200),                  // older, non-expiring
	}
	got, err := ResolveModel(Binding{Family: "qwen", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.Equal(t, "qwen/qwen3.7-plus", got)
}

// TestResolveModel_Stable_TD002_FarFutureExpirationExcluded covers TD-002: the
// deprecation rule is any-non-null expiration_date (fails closed, no horizon), so
// a far-future sentinel date is treated as deprecated and excluded. Documents the
// decision to keep the simple any-non-null rule rather than a horizon window.
func TestResolveModel_Stable_TD002_FarFutureExpirationExcluded(t *testing.T) {
	models := []CatalogModel{
		cmExp("z-ai/glm-5v-turbo", 300, "2098-12-31"), // far-future sentinel → still excluded
		cm("z-ai/glm-5.2", 200),
	}
	got, err := ResolveModel(Binding{Family: "glm", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.Equal(t, "z-ai/glm-5.2", got)
}

// TestResolveModel_Stable_AcceptsCleanNewest covers AC 03-04 Scenario 3: an entry
// with no preview token and null expiration_date is accepted directly — @stable is
// a pure exclusion filter, not an extra inclusion requirement.
func TestResolveModel_Stable_AcceptsCleanNewest(t *testing.T) {
	got, err := ResolveModel(Binding{Family: "glm", Channel: "@stable"},
		[]CatalogModel{cm("z-ai/glm-5.1", 100), cm("z-ai/glm-5.2", 200)})
	require.NoError(t, err)
	assert.Equal(t, "z-ai/glm-5.2", got)
}

// TestResolveModel_Stable_EmptyExpirationIsNotDeprecated covers AC 03-04 Edge Case
// 3: an expiration_date of "" (empty string, not JSON null) is treated as NOT
// deprecated, equivalently to null.
func TestResolveModel_Stable_EmptyExpirationIsNotDeprecated(t *testing.T) {
	got, err := ResolveModel(Binding{Family: "glm", Channel: "@stable"},
		[]CatalogModel{cmExp("z-ai/glm-5.2", 200, "   ")})
	require.NoError(t, err)
	assert.Equal(t, "z-ai/glm-5.2", got, `expiration_date "" / whitespace must count as not-deprecated`)
}

// TestResolveModel_Stable_AllExcluded_Error covers AC 03-04 Edge Case 2 + Error
// Scenario 1: when every entry under a prefix is preview-tagged or expiring, @stable
// fails closed with a descriptive error rather than returning an excluded entry.
func TestResolveModel_Stable_AllExcluded_Error(t *testing.T) {
	models := []CatalogModel{
		cm("deepseek/deepseek-v5-preview", 300),
		cmExp("deepseek/deepseek-v4-pro", 200, "2027-01-01"),
	}
	got, err := ResolveModel(Binding{Family: "deepseek", Channel: "@stable"}, models)
	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "deepseek/")
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

// --- Element 3 (AC 03-03): explicit-slug pin resolves unchanged, never floats --

// TestResolveModel_Pin_ResolvesVerbatim covers AC 03-03 Scenario 1: a pinned
// binding returns the pinned slug exactly, ignoring catalog contents.
func TestResolveModel_Pin_ResolvesVerbatim(t *testing.T) {
	models := []CatalogModel{cm("deepseek/deepseek-v9", 9999)}
	got, err := ResolveModel(Binding{Pin: "deepseek/deepseek-v4-pro"}, models)
	require.NoError(t, err)
	assert.Equal(t, "deepseek/deepseek-v4-pro", got)
}

// TestResolveModel_Pin_InvariantAcrossSnapshots covers AC 03-03 Scenario 2: the
// pin is identical across two catalogs whose newest vendor member differs —
// proving it never floats onto the newer entry an unpinned persona would take.
func TestResolveModel_Pin_InvariantAcrossSnapshots(t *testing.T) {
	snapA := []CatalogModel{cm("deepseek/deepseek-v4-pro", 100)}
	snapB := []CatalogModel{
		cm("deepseek/deepseek-v4-pro", 100),
		cm("deepseek/deepseek-v5", 999), // newer member exists in B
	}
	b := Binding{Family: "deepseek", Pin: "deepseek/deepseek-v4-pro", Channel: "@stable"}
	gotA, err := ResolveModel(b, snapA)
	require.NoError(t, err)
	gotB, err := ResolveModel(b, snapB)
	require.NoError(t, err)
	assert.Equal(t, "deepseek/deepseek-v4-pro", gotA)
	assert.Equal(t, gotA, gotB, "pin must not float onto the newer snapshot-B member")
}

// TestResolveModel_Pin_OverridesChannel covers AC 03-03 Scenario 3: a pin plus a
// @latest channel still returns the pin unchanged — channel is irrelevant to a pin.
func TestResolveModel_Pin_OverridesChannel(t *testing.T) {
	got, err := ResolveModel(Binding{Pin: "z-ai/glm-5.2", Channel: "@latest"},
		[]CatalogModel{cm("z-ai/glm-9", 9999)})
	require.NoError(t, err)
	assert.Equal(t, "z-ai/glm-5.2", got)
}

// TestResolveModel_Pin_PrecedenceOverAlias covers AC 03-03 Edge Case 1: a pin on a
// family that would otherwise route through the alias table wins — the alias table
// is never consulted (strategy order pin → alias → created-timestamp).
func TestResolveModel_Pin_PrecedenceOverAlias(t *testing.T) {
	got, err := ResolveModel(Binding{Family: "anthropic/claude-opus", Pin: "custom/override"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "custom/override", got)
	assert.NotEqual(t, "~anthropic/claude-opus-latest", got)
}

// TestResolveModel_Pin_PrecedenceOverCreatedScan covers AC 03-03 Edge Case 2: a pin
// on a created-timestamp family wins over a newer vendor-prefix member.
func TestResolveModel_Pin_PrecedenceOverCreatedScan(t *testing.T) {
	models := []CatalogModel{
		cm("z-ai/glm-5.2", 100),
		cm("z-ai/glm-9", 9999), // newer; an unpinned glm binding would take this
	}
	got, err := ResolveModel(Binding{Family: "glm", Pin: "z-ai/glm-5.2", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.Equal(t, "z-ai/glm-5.2", got, "pin must win over the newer created-timestamp member")
}

// TestResolveModel_Pin_EmptyFallsThrough covers AC 03-03 Edge Case 3: an empty or
// whitespace-only pin is treated as "no pin" and control falls through to the
// alias/created-timestamp strategy, never returned as a valid empty slug.
func TestResolveModel_Pin_EmptyFallsThrough(t *testing.T) {
	for _, pin := range []string{"", "   ", "\t"} {
		got, err := ResolveModel(Binding{Family: "anthropic/claude-opus", Pin: pin}, nil)
		require.NoError(t, err)
		assert.Equal(t, "~anthropic/claude-opus-latest", got,
			"empty/whitespace pin %q must fall through to the alias strategy", pin)
	}
}

// TestResolveModel_Pin_Invalid_Error covers AC 03-03 Error Scenario 1 and pins the
// security invariant to the pin short-circuit itself: an implausible pin (no "/",
// a control character, or a bare vendor/model segment) is rejected with an error
// and an empty slug — an untrusted community pin never reaches a lock unvalidated.
func TestResolveModel_Pin_Invalid_Error(t *testing.T) {
	for _, pin := range []string{"not-a-slug", "deepseek/x\ny", "z-ai/", "/glm-5.2"} {
		got, err := ResolveModel(Binding{Pin: pin}, nil)
		require.Error(t, err, "invalid pin %q must be rejected on the pin path", pin)
		assert.Empty(t, got)
		assert.Contains(t, err.Error(), "pin")
	}
}

// TestResolveModel_CreatedScan_ControlCharSlug_Rejected proves the scan output is
// sanitized: a selected entry whose slug carries a control character fails closed
// with an error rather than resolving to a poisoned lock value.
func TestResolveModel_CreatedScan_ControlCharSlug_Rejected(t *testing.T) {
	got, err := ResolveModel(Binding{Family: "deepseek", Channel: "@stable"},
		[]CatalogModel{cm("deepseek/x\ny", 100)})
	require.Error(t, err)
	assert.Empty(t, got)
}

// TestValidateResolvedSlug covers the sanitization guard directly: empty,
// control-char, bare-vendor, bare-model, and valid inputs (mirrors 19.6 TD-008).
func TestValidateResolvedSlug(t *testing.T) {
	cases := []struct {
		slug    string
		wantErr bool
	}{
		{"deepseek/deepseek-v4-pro", false},
		{"z-ai/glm-5.2", false},
		{"", true},
		{"   ", true},
		{"no-slash-here", true},
		{"z-ai/", true},         // bare vendor, empty model
		{"/glm-5.2", true},      // empty vendor
		{"deepseek/x\ny", true}, // control char
	}
	for _, tc := range cases {
		err := validateResolvedSlug(tc.slug)
		if tc.wantErr {
			assert.Error(t, err, "slug %q must be rejected", tc.slug)
		} else {
			assert.NoError(t, err, "slug %q must be accepted", tc.slug)
		}
	}
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

// --- Story 08 (AC 08-01): the checked-in snapshot fixture exercises EVERY
// resolver branch, proven by loading the real testdata file through the same
// zero-live-network httptest path the resolver uses ---------------------------

// loadFixtureModels serves the checked-in testdata/catalog_snapshot.json through
// an httptest server and returns the parsed model list, so AC 08-01's coverage
// assertions run against the real fixture with zero live network.
func loadFixtureModels(t *testing.T) []CatalogModel {
	t.Helper()
	fixture, err := os.ReadFile("testdata/catalog_snapshot.json")
	require.NoError(t, err, "catalog snapshot fixture must exist")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(ts.Close)
	models, err := (&CatalogClient{HTTPClient: ts.Client(), BaseURL: ts.URL}).FetchModels()
	require.NoError(t, err)
	return models
}

// TestCatalogSnapshot_CoversAliasAndPrefixBranches (AC 08-01 Scenarios 1-3) asserts
// the fixture carries an entry for every alias-covered vendor tier, ≥2 members
// under each created-scan vendor prefix (deepseek/, qwen/, z-ai/), and all 10 of
// Epic 19.6's pinned slugs (zero-migration coverage).
func TestCatalogSnapshot_CoversAliasAndPrefixBranches(t *testing.T) {
	models := loadFixtureModels(t)
	byID := make(map[string]CatalogModel, len(models))
	prefixCount := map[string]int{}
	scanPrefixes := []string{"deepseek/", "qwen/", "z-ai/"}
	for _, m := range models {
		byID[m.ID] = m
		for _, p := range scanPrefixes {
			if strings.HasPrefix(m.ID, p) {
				prefixCount[p]++
			}
		}
	}

	// Every alias slug the resolver can emit must be a real fixture entry.
	for family, alias := range aliasTable {
		assert.Contains(t, byID, alias, "fixture must contain alias entry %q (family %q)", alias, family)
	}

	// ≥2 candidates under each created-scan prefix (so newest-selection is exercised,
	// not just a singleton), including z-ai/ for glenna — never glm/.
	for _, p := range scanPrefixes {
		assert.GreaterOrEqual(t, prefixCount[p], 2, "fixture needs ≥2 %s members", p)
	}
	assert.Zero(t, prefixCount["glm/"], "fixture must never use a glm/ namespace")

	// All 10 of Epic 19.6's pinned slugs (zero-migration coverage): no `models check`
	// missing condition for any seed lock.
	for _, slug := range []string{
		"anthropic/claude-opus-4.8", "anthropic/claude-sonnet-5",
		"openai/gpt-5.5", "openai/gpt-5.4-mini",
		"google/gemini-2.5-pro", "google/gemini-2.5-flash",
		"deepseek/deepseek-v4-pro", "qwen/qwen3-coder-plus",
		"moonshotai/kimi-k2.7-code", "z-ai/glm-5.2",
	} {
		assert.Contains(t, byID, slug, "fixture must contain pinned slug %q", slug)
	}
}

// TestCatalogSnapshot_CoversPreviewUnderScanPrefix (AC 08-01 Edge Case 1) requires
// the fixture to contain a qwen/ preview-tokened model whose `created` is NEWER
// than the newest GA qwen model, so @stable must skip it and @latest must select
// it — exercising the preview-exclusion branch of the created-timestamp scan
// against real fixture data.
func TestCatalogSnapshot_CoversPreviewUnderScanPrefix(t *testing.T) {
	models := loadFixtureModels(t)

	var newestGA, newestPreview *CatalogModel
	for i := range models {
		m := &models[i]
		if !strings.HasPrefix(m.ID, "qwen/") || m.Created <= 0 {
			continue
		}
		if hasPreviewToken(*m) {
			if newestPreview == nil || m.Created > newestPreview.Created {
				newestPreview = m
			}
		} else if newestGA == nil || m.Created > newestGA.Created {
			newestGA = m
		}
	}
	require.NotNil(t, newestGA, "fixture needs a GA (non-preview) qwen model")
	require.NotNil(t, newestPreview, "fixture needs a preview-tokened qwen model (EC1 coverage)")
	assert.Greater(t, newestPreview.Created, newestGA.Created,
		"the preview qwen must be newer than the newest GA to exercise the skip branch")

	// @stable skips the newer preview and selects the newest GA; @latest selects the
	// newest preview — proving the fixture drives both sides of the branch.
	stable, err := ResolveModel(Binding{Family: "qwen", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.Equal(t, newestGA.ID, stable, "@stable skips the preview, selects newest GA")
	latest, err := ResolveModel(Binding{Family: "qwen", Channel: "@latest"}, models)
	require.NoError(t, err)
	assert.Equal(t, newestPreview.ID, latest, "@latest selects the newest preview")
}

// TestCatalogSnapshot_CoversExpiringNewestUnderScanPrefix (AC 08-01 Edge Case 2)
// requires the fixture to contain a deepseek/ model that is the newest by `created`
// under the prefix AND carries a non-null expiration_date, so @stable excludes it
// and falls through to the next-newest non-expiring member — exercising the
// deprecation-exclusion branch of the created-timestamp scan against real data.
func TestCatalogSnapshot_CoversExpiringNewestUnderScanPrefix(t *testing.T) {
	models := loadFixtureModels(t)
	byID := make(map[string]CatalogModel, len(models))

	var newest *CatalogModel
	for i := range models {
		m := &models[i]
		byID[m.ID] = *m
		if !strings.HasPrefix(m.ID, "deepseek/") || m.Created <= 0 {
			continue
		}
		if newest == nil || m.Created > newest.Created {
			newest = m
		}
	}
	require.NotNil(t, newest, "fixture needs deepseek/ members")
	require.True(t, isDeprecated(*newest),
		"the newest-by-created deepseek member must carry an expiration_date (EC2 coverage)")

	// @stable must exclude the expiring newest and select an older, non-expiring member.
	stable, err := ResolveModel(Binding{Family: "deepseek", Channel: "@stable"}, models)
	require.NoError(t, err)
	assert.NotEqual(t, newest.ID, stable, "@stable must not select the expiring newest")
	assert.False(t, isDeprecated(byID[stable]), "@stable selection must itself be non-expiring")
}

// TestCatalogSnapshot_CoversIneligibleCreatedUnderScanPrefix (AC 08-01 / AC 03-02
// EC4) requires the fixture to contain a scan-prefix member with an absent/zero
// `created`, so the resolver's ineligibility branch (m.Created <= 0 → skipped) is
// exercised by the CHECKED-IN fixture, not only by synthetic unit tests. The
// ineligible member must be present yet never selected under either channel.
func TestCatalogSnapshot_CoversIneligibleCreatedUnderScanPrefix(t *testing.T) {
	models := loadFixtureModels(t)

	var ineligible *CatalogModel
	for i := range models {
		m := &models[i]
		if strings.HasPrefix(m.ID, "deepseek/") && m.Created <= 0 {
			ineligible = m
			break
		}
	}
	require.NotNil(t, ineligible,
		"fixture needs a deepseek/ member with created<=0 to exercise the ineligibility branch")

	for _, channel := range []string{"@stable", "@latest"} {
		got, err := ResolveModel(Binding{Family: "deepseek", Channel: channel}, models)
		require.NoError(t, err, "deepseek %s", channel)
		assert.NotEqual(t, ineligible.ID, got,
			"a created<=0 member must never be selected under %s", channel)
	}
}
