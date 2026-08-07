package benchmarkimport_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/payload"
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

	// Every category must be one a reviewer is actually prompted to emit, or its
	// recall is structurally zero no matter how good the reviewer is. The
	// vocabulary is read from the live prompt rather than copied here, so
	// reverting the personas/_base.md edit this suite depends on fails the guard.
	emittable := baseCategoryVocabulary(t)
	for cat := range distinct {
		assert.Contains(t, emittable, cat,
			"%q is not in personas/_base.md's CATEGORY vocabulary, so no reviewer is prompted to emit it", cat)
	}
}

// baseCategoryVocabulary parses the CATEGORY word list out of the shared base
// persona prompt.
func baseCategoryVocabulary(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../personas/_base.md")
	require.NoError(t, err)

	m := regexp.MustCompile(`CATEGORY is a single lowercase word \(([^)]*)\)`).FindSubmatch(raw)
	require.NotNil(t, m, "personas/_base.md must still declare the CATEGORY vocabulary in the expected form")

	var out []string
	for _, w := range strings.Split(string(m[1]), ",") {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	require.NotEmpty(t, out)
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
