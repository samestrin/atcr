package benchmarkimport_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/benchmarkimport"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/reconcile"
)

// suiteDir is the committed standard-v1 suite, relative to this package.
const suiteDir = "../../benchmarks/standard-v1"

// The suite content is authored once and committed. These tests guard it as
// shipped data: they run offline in CI and fail if an edit makes the bundled
// suite unloadable, unscoreable, or unreviewable.

func TestBundledSuite_SatisfiesTheManifestContract(t *testing.T) {
	m, err := benchmark.Load(suiteDir)

	require.NoError(t, err, "the committed suite must load through the same path atcr benchmark verify uses")
	assert.Equal(t, "standard-v1", m.Suite)
	assert.Equal(t, "1.0.0", m.SuiteVersion)
}

func TestBundledSuite_MeetsTheEpicsCaseAndCategoryBar(t *testing.T) {
	m, err := benchmark.Load(suiteDir)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(m.Cases), 12, "the suite must carry at least 12 cases")

	distinct := map[string]struct{}{}
	for _, c := range m.Cases {
		for _, cat := range c.ExpectedCategories {
			distinct[cat] = struct{}{}
		}
	}
	assert.GreaterOrEqual(t, len(distinct), 3, "the suite must span at least 3 categories, got %v", distinct)
}

// Every category the suite plants must be a member of the closed reviewer
// vocabulary, whose authority is reconcile/category.go (epic 35.16.4).
//
// This asserts membership against the taxonomy CONSTANT directly rather than
// scraping an allowlist out of a persona prompt. Scraping is both obsolete and
// unsound here: since 35.16.4 the vocabulary is injected into all 29 prompts
// through internal/payload's ScopeRule rather than written into any prompt file,
// so there is nothing left in a persona to parse — and the regex that used to try
// silently matched nothing, while an "extracted no vocabulary" branch turned that
// miss into a pass. A category outside the taxonomy is never offered to any
// reviewer, so every case planting it scores a structural zero.
func TestBundledSuite_PlantsOnlyTaxonomyCategories(t *testing.T) {
	m, err := benchmark.Load(suiteDir)
	require.NoError(t, err)

	vocabulary := taxonomyVocabulary(t)

	planted := map[string]struct{}{}
	for _, c := range m.Cases {
		for _, cat := range c.ExpectedCategories {
			planted[cat] = struct{}{}
			assert.Contains(t, vocabulary, cat,
				"case %q plants %q, which is not a member of reconcile.Categories() — no reviewer is prompted to emit it, so that case's recall is structurally zero",
				c.ID, cat)
		}
	}

	// A suite that planted nothing would satisfy the loop above vacuously — the
	// exact shape of the escape hatch this guard replaces.
	assert.NotEmpty(t, planted, "the suite must plant at least one expected category")
}

// The ingestion map is the only producer of a suite's expected categories, so
// every value it can emit must be a taxonomy member. The Load-based guard above
// covers only what the COMMITTED suite happens to plant; this covers every value
// a future rebuild could emit, including the aacr-bench labels no committed case
// currently carries.
func TestCategoryMap_EmitsOnlyTaxonomyCategories(t *testing.T) {
	vocabulary := taxonomyVocabulary(t)

	for _, upstream := range []string{
		"code defect",
		"security vulnerability",
		"security",
		"maintainability and readability",
		"maintainability",
		"performance",
	} {
		mapped, ok := benchmarkimport.MapCategory(upstream)
		require.True(t, ok, "aacr-bench category %q must still map", upstream)
		assert.Contains(t, vocabulary, mapped,
			"categoryMap emits %q for %q, which is not a member of reconcile.Categories()", mapped, upstream)
	}
}

// taxonomyVocabulary is the closed reviewer vocabulary as a set. It fails the
// test on an empty vocabulary rather than returning one: an empty set makes every
// Contains assertion above fail loudly, but a caller that looped over it instead
// would pass vacuously, and a vacuous pass is precisely the defect these guards
// replace.
func taxonomyVocabulary(t *testing.T) map[string]struct{} {
	t.Helper()
	cats := reconcile.Categories()
	require.NotEmpty(t, cats, "reconcile must publish a non-empty category vocabulary")

	out := make(map[string]struct{}, len(cats))
	for _, c := range cats {
		out[c] = struct{}{}
	}
	return out
}

func TestBundledSuite_CaseIdsAreTheGoldenSeededSelection(t *testing.T) {
	// The committed suite is the -seed 20260807 / -limit 18 selection. Pinning
	// the exact ids catches a silent re-sample: an upstream record added or
	// removed, or a change in Go's math/rand stream, would shift the whole
	// selection while every other assertion here still passed.
	want := []string{
		"bluewave-labs-checkmate-pr-2883",
		"cherryhq-cherry-studio-pr-5637",
		"cline-cline-pr-4786",
		"cline-cline-pr-5955",
		"dotnet-aspnetcore-pr-62734",
		"elastic-elasticsearch-pr-118183",
		"elastic-elasticsearch-pr-124403",
		"electron-electron-pr-46982",
		"freecad-freecad-pr-18688",
		"freecad-freecad-pr-20825",
		"juspay-hyperswitch-pr-7353",
		"lvgl-lvgl-pr-7602",
		"n8n-io-n8n-pr-20188",
		"openai-codex-pr-3212",
		"timescale-timescaledb-pr-7632",
		"vllm-project-vllm-pr-12608",
		"vllm-project-vllm-pr-17425",
	}

	m, err := benchmark.Load(suiteDir)
	require.NoError(t, err)

	got := make([]string, 0, len(m.Cases))
	for _, c := range m.Cases {
		got = append(got, c.ID)
	}
	assert.Equal(t, want, got, "the committed suite is no longer the golden seeded selection")
}

func TestBundledSuite_ReproHashIsPinned(t *testing.T) {
	// The published reproducibility hash. .gitattributes keeps raw diff bytes
	// stable across checkouts, but a `git add --renormalize`, a contributor on
	// core.autocrlf=true, or a single-byte edit to any committed .diff changes
	// the hash silently — this pin is the only thing that notices.
	const want = "07b047c221422e198060d8ed1e1ca39b1e1803e8b7ce5de24abf531ac394dd1d"

	got, err := benchmark.ReproHash(suiteDir)
	require.NoError(t, err)
	assert.Equal(t, want, got,
		"the committed suite's raw bytes changed; the published hash no longer reproduces")
}

func TestBundledSuite_EveryDiffYieldsReviewableContent(t *testing.T) {
	m, err := benchmark.Load(suiteDir)
	require.NoError(t, err)

	for _, c := range m.Cases {
		t.Run(c.ID, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(suiteDir, c.Diff))
			require.NoError(t, err)

			entries, err := payload.BuildEntriesFromDiff(string(raw))

			// A diff that parses to zero entries aborts the whole benchmark run,
			// not just its own case, so this is a suite-wide invariant.
			require.NoError(t, err, "diff must parse through the production ingestion path")
			assert.NotEmpty(t, entries, "diff must yield at least one reviewable file entry")
		})
	}
}

func TestBundledSuite_DiffsStayWithinTheRunnersSizeCeiling(t *testing.T) {
	m, err := benchmark.Load(suiteDir)
	require.NoError(t, err)

	for _, c := range m.Cases {
		info, err := os.Stat(filepath.Join(suiteDir, c.Diff))
		require.NoError(t, err)
		assert.LessOrEqual(t, info.Size(), benchmark.MaxDiffBytes,
			"case %q exceeds the runner's per-diff ceiling and would fail the run", c.ID)
	}
}

func TestBundledSuite_CreditsItsUpstreamSource(t *testing.T) {
	notice, err := os.ReadFile(filepath.Join(suiteDir, "NOTICE.md"))

	require.NoError(t, err, "redistributed dataset content must ship its attribution")
	for _, want := range []string{"alibaba/aacr-bench", "Apache-2.0", "2601.19494"} {
		assert.Contains(t, string(notice), want, "NOTICE.md must credit %s", want)
	}
}
