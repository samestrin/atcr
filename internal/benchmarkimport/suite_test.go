package benchmarkimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/benchmark"
)

// fakeFetcher stands in for the GitHub compare API so the test suite never
// touches the network. Ingestion is an authoring-time action; only its
// committed output is exercised by CI.
type fakeFetcher struct {
	calls       []string
	diff        string
	err         error
	unavailable map[string]bool
}

func (f *fakeFetcher) FetchDiff(_ context.Context, owner, repo, base, head string) ([]byte, error) {
	f.calls = append(f.calls, fmt.Sprintf("%s/%s@%s..%s", owner, repo, base, head))
	if f.unavailable[base] {
		return nil, fmt.Errorf("compare %s/%s: %w", owner, repo, ErrDiffUnavailable)
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.diff != "" {
		return []byte(f.diff), nil
	}
	return []byte("--- a/x.go\n+++ b/x.go\n@@ -1,1 +1,2 @@\n func x() {\n+\treturn nil // planted\n"), nil
}

func TestMapCategory_CoversTheDatasetVocabulary(t *testing.T) {
	cases := map[string]string{
		"Code Defect":                     "correctness",
		"Security Vulnerability":          "security",
		"Maintainability and Readability": "maintainability",
		"Performance":                     "performance",
		// The epic body used shorter spellings than the dataset actually ships;
		// both are accepted so a wording change upstream is not a silent drop.
		"Security":        "security",
		"Maintainability": "maintainability",
	}

	for in, want := range cases {
		got, ok := MapCategory(in)
		assert.True(t, ok, "%q must map to an ATCR category", in)
		assert.Equal(t, want, got, "%q maps to %q", in, want)
	}
}

func TestMapCategory_IsCaseAndSpaceInsensitive(t *testing.T) {
	got, ok := MapCategory("  cODE dEFECT  ")

	assert.True(t, ok, "matching tolerates upstream casing and padding")
	assert.Equal(t, "correctness", got)
}

func TestMapCategory_RejectsUnknownVocabulary(t *testing.T) {
	_, ok := MapCategory("Documentation Update")

	assert.False(t, ok, "an unmapped category is reported, never guessed at")
}

func TestExpectedCategories_IsDedupedAndSorted(t *testing.T) {
	rec := Record{Comments: []Comment{
		{Category: "Maintainability and Readability"},
		{Category: "Code Defect"},
		{Category: "Code Defect"},
		{Category: "Security Vulnerability"},
		{Category: "Documentation Update"}, // unmappable, dropped
	}}

	got := ExpectedCategories(rec)

	assert.Equal(t, []string{"correctness", "maintainability", "security"}, got,
		"categories are deduped and sorted; the manifest contract rejects duplicates")
}

func TestExpectedCategories_RetainsAIProposedCategories(t *testing.T) {
	// Policy: is_ai_comment is provenance, not a filter. Model-proposed labels
	// are kept as ground truth exactly as upstream published them (see
	// benchmarks/standard-v1/NOTICE.md); this test pins that decision.
	rec := Record{Comments: []Comment{
		{Category: "Code Defect", IsAIComment: true, SourceModel: "GPT-5.2"},
		{Category: "Performance", IsAIComment: false},
	}}

	got := ExpectedCategories(rec)

	assert.Equal(t, []string{"correctness", "performance"}, got,
		"an AI-proposed label counts toward ground truth exactly as a human one does")
}

func TestCaseID_IsDerivedFromThePullRequest(t *testing.T) {
	got, err := CaseID(Record{GithubPrURL: "https://github.com/alibaba/spring-ai-alibaba/pull/869"})

	require.NoError(t, err)
	assert.Equal(t, "alibaba-spring-ai-alibaba-pr-869", got,
		"case ids are slug-safe and carry no path separator")
}

func TestCaseID_RejectsAnUnparsablePullRequestURL(t *testing.T) {
	_, err := CaseID(Record{GithubPrURL: "https://example.com/not-a-pr"})

	assert.Error(t, err, "a URL that is not a GitHub PR cannot yield a stable id")
}

func TestBuildSuite_WritesAManifestTheContractAccepts(t *testing.T) {
	dir := t.TempDir()
	recs := loadFixture(t)
	f := &fakeFetcher{}

	res, err := BuildSuite(context.Background(), Options{
		Records:      recs,
		OutDir:       dir,
		Suite:        "standard-v1",
		SuiteVersion: "1.0.0",
		Fetcher:      f,
	})
	require.NoError(t, err)

	assert.Equal(t, len(recs), res.CasesWritten, "every mappable record becomes a case")
	assert.Len(t, f.calls, len(recs), "one compare call per record")

	// The real contract check: internal/benchmark must load what we wrote.
	m, err := benchmark.Load(dir)
	require.NoError(t, err, "emitted suite must satisfy the manifest contract")
	assert.Equal(t, "standard-v1", m.Suite)
	assert.Equal(t, "1.0.0", m.SuiteVersion)
	assert.Len(t, m.Cases, len(recs))

	for _, c := range m.Cases {
		assert.NotEmpty(t, c.ExpectedCategories, "every case carries at least one category")
		assert.FileExists(t, filepath.Join(dir, c.Diff), "every case's diff file is written")
	}
}

func TestBuildSuite_IsByteReproducible(t *testing.T) {
	recs := loadFixture(t)

	hashes := make([]string, 2)
	for i := range hashes {
		dir := t.TempDir()
		_, err := BuildSuite(context.Background(), Options{
			Records: recs, OutDir: dir, Suite: "standard-v1", SuiteVersion: "1.0.0",
			Fetcher: &fakeFetcher{},
		})
		require.NoError(t, err)
		h, err := benchmark.ReproHash(dir)
		require.NoError(t, err)
		hashes[i] = h
	}

	assert.Equal(t, hashes[0], hashes[1],
		"two ingestions of the same records produce an identical reproducibility hash")
}

func TestBuildSuite_SkipsRecordsWithNoMappableCategory(t *testing.T) {
	dir := t.TempDir()
	recs := []Record{
		{GithubPrURL: "https://github.com/o/r/pull/1", SourceCommit: "aaaaaaa", TargetCommit: "bbbbbbb",
			Comments: []Comment{{Category: "Documentation Update"}}},
		{GithubPrURL: "https://github.com/o/r/pull/2", SourceCommit: "aaaaaaa", TargetCommit: "bbbbbbb",
			Comments: []Comment{{Category: "Code Defect"}}},
	}

	res, err := BuildSuite(context.Background(), Options{
		Records: recs, OutDir: dir, Suite: "s", SuiteVersion: "1.0.0", Fetcher: &fakeFetcher{},
	})
	require.NoError(t, err)

	assert.Equal(t, 1, res.CasesWritten, "a record whose categories are all unmappable is skipped")
	assert.Equal(t, 1, res.Skipped, "the skip is counted, not silent")
}

func TestBuildSuite_FailsWhenEveryRecordIsSkipped(t *testing.T) {
	dir := t.TempDir()
	recs := []Record{
		{GithubPrURL: "https://github.com/o/r/pull/1", SourceCommit: "aaaaaaa", TargetCommit: "bbbbbbb",
			Comments: []Comment{{Category: "Documentation Update"}}},
	}

	_, err := BuildSuite(context.Background(), Options{
		Records: recs, OutDir: dir, Suite: "s", SuiteVersion: "1.0.0", Fetcher: &fakeFetcher{},
	})

	assert.Error(t, err, "an empty suite is refused rather than written as a valid-but-useless manifest")
}

func TestBuildSuite_SkipsRecordsWhoseDiffIsGoneUpstream(t *testing.T) {
	dir := t.TempDir()
	recs := loadFixture(t)
	// A PR whose commits were force-pushed or garbage-collected 404s. That is a
	// property of that one record, not a broken ingestion, so it must not take
	// the whole suite down with it.
	f := &fakeFetcher{unavailable: map[string]bool{recs[0].SourceCommit: true}}

	res, err := BuildSuite(context.Background(), Options{
		Records: recs, OutDir: dir, Suite: "s", SuiteVersion: "1.0.0", Fetcher: f,
	})
	require.NoError(t, err, "one dead upstream PR does not fail the build")

	assert.Equal(t, len(recs)-1, res.CasesWritten, "the remaining records still become cases")
	assert.Equal(t, 1, res.Unavailable, "the dropped record is counted, not silently lost")
}

func TestBuildSuite_PropagatesFetchFailure(t *testing.T) {
	dir := t.TempDir()

	_, err := BuildSuite(context.Background(), Options{
		Records: loadFixture(t), OutDir: dir, Suite: "s", SuiteVersion: "1.0.0",
		Fetcher: &fakeFetcher{err: errors.New("upstream 404")},
	})

	assert.Error(t, err, "a failed diff fetch aborts ingestion instead of emitting a partial suite")
}

func TestBuildSuite_RejectsAnEmptyDiff(t *testing.T) {
	dir := t.TempDir()

	_, err := BuildSuite(context.Background(), Options{
		Records: loadFixture(t)[:1], OutDir: dir, Suite: "s", SuiteVersion: "1.0.0",
		Fetcher: &fakeFetcher{diff: "   \n"},
	})

	assert.Error(t, err,
		"a blank diff yields no reviewable content and would fail the entire benchmark run, not just its case")
}

func TestBuildSuite_RequiresAFetcherAndAnOutputDirectory(t *testing.T) {
	_, err := BuildSuite(context.Background(), Options{Records: loadFixture(t), OutDir: t.TempDir()})
	assert.Error(t, err, "a nil fetcher is a caller mistake, caught before any file is written")

	_, err = BuildSuite(context.Background(), Options{Records: loadFixture(t), Fetcher: &fakeFetcher{}})
	assert.Error(t, err, "an empty output directory would write into the working directory")
}

func TestBuildSuite_RejectsARecordWithAnUnparsablePullRequestURL(t *testing.T) {
	_, err := BuildSuite(context.Background(), Options{
		Records: []Record{{GithubPrURL: "https://example.com/x", SourceCommit: "aaaaaaa", TargetCommit: "bbbbbbb",
			Comments: []Comment{{Category: "Code Defect"}}}},
		OutDir: t.TempDir(), Suite: "s", SuiteVersion: "1.0.0", Fetcher: &fakeFetcher{},
	})

	assert.Error(t, err, "a record whose URL yields no case id aborts the build rather than being skipped silently")
}

func TestBuildSuite_RejectsOptionShapedCommitValuesBeforeFetching(t *testing.T) {
	// Options.Records can be constructed programmatically, bypassing
	// ParseDataset's validation; the trust boundary must hold by construction.
	f := &fakeFetcher{}
	_, err := BuildSuite(context.Background(), Options{
		Records: []Record{{GithubPrURL: "https://github.com/o/r/pull/1",
			SourceCommit: "--upload-pack=touch /tmp/pwned", TargetCommit: "bbbbbbb",
			Comments: []Comment{{Category: "Code Defect"}}}},
		OutDir: t.TempDir(), Suite: "s", SuiteVersion: "1.0.0", Fetcher: f,
	})

	require.Error(t, err, "an option-shaped commit value must be rejected at the boundary")
	assert.Empty(t, f.calls, "rejection happens before any fetcher call, so git never sees the value")
}

func TestBuildSuite_WritesDiffFilesInsideTheSuiteDirectory(t *testing.T) {
	dir := t.TempDir()

	_, err := BuildSuite(context.Background(), Options{
		Records: loadFixture(t), OutDir: dir, Suite: "s", SuiteVersion: "1.0.0",
		Fetcher: &fakeFetcher{},
	})
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, e.IsDir(), "the suite is flat: %s", e.Name())
	}
}
