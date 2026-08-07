package benchmarkimport_test

import (
	"os"
	"path/filepath"
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

	// Every category must be one a reviewer can actually emit (personas/_base.md),
	// or its recall is structurally zero no matter how good the reviewer is.
	emittable := map[string]bool{
		"security": true, "correctness": true, "performance": true,
		"maintainability": true, "testing": true, "style": true, "docs": true,
	}
	for cat := range distinct {
		assert.True(t, emittable[cat], "%q is not in the reviewer CATEGORY vocabulary, so it can never be matched", cat)
	}
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
