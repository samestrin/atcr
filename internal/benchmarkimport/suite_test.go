package benchmarkimport

import (
	"context"
	"encoding/json"
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
	// failAfter delays err until this many calls have succeeded, reproducing the
	// realistic failure — a rate limit or 502 partway through a run — rather than
	// one that fires before anything has been written.
	failAfter int
}

func (f *fakeFetcher) FetchDiff(_ context.Context, owner, repo, base, head string) ([]byte, error) {
	f.calls = append(f.calls, fmt.Sprintf("%s/%s@%s..%s", owner, repo, base, head))
	if f.unavailable[base] {
		return nil, fmt.Errorf("compare %s/%s: %w", owner, repo, ErrDiffUnavailable)
	}
	if f.err != nil && len(f.calls) > f.failAfter {
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

	got, unmapped := ExpectedCategories(rec)

	assert.Equal(t, []string{"correctness", "maintainability", "security"}, got,
		"categories are deduped and sorted; the manifest contract rejects duplicates")
	assert.Equal(t, 1, unmapped,
		"a dropped category is tallied, not silent — a shrunken expected set inflates scored recall")
}

func TestExpectedCategories_RetainsAIProposedCategories(t *testing.T) {
	// Policy: is_ai_comment is provenance, not a filter. Model-proposed labels
	// are kept as ground truth exactly as upstream published them (see
	// benchmarks/standard-v1/NOTICE.md); this test pins that decision.
	rec := Record{Comments: []Comment{
		{Category: "Code Defect", IsAIComment: true, SourceModel: "GPT-5.2"},
		{Category: "Performance", IsAIComment: false},
	}}

	got, unmapped := ExpectedCategories(rec)

	assert.Equal(t, []string{"correctness", "performance"}, got,
		"an AI-proposed label counts toward ground truth exactly as a human one does")
	assert.Zero(t, unmapped)
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

func TestCaseID_RejectsLoosePullRequestURLs(t *testing.T) {
	// The dataset is third-party JSON; owner/repo flow into a compare-API path
	// and an unescaped clone URL, so only the canonical bare-PR shape parses.
	for _, bad := range []string{
		"https://github.com/o/r/pull/12/files#discussion_r1", // trailing path must not alias the bare PR
		"https://github.com/o/r/pull/12?tab=files",           // query string
		"https://github.com/../r/pull/12",                    // traversal-shaped owner
		"https://github.com/o/r/pull/12 extra",               // trailing junk
		"https://github.com/o/r/pull/12x",                    // the number is the last segment
	} {
		_, err := CaseID(Record{GithubPrURL: bad})
		assert.Error(t, err, "%q must not parse as a bare PR URL", bad)
	}
}

func TestParseDataset_RejectsAMalformedPullRequestURL(t *testing.T) {
	payload := `[{"githubPrUrl":"https://github.com/o/r/pull/12/files","source_commit":"aaaaaaa","target_commit":"bbbbbbb","comments":[{"category":"Code Defect","path":"a.go"}]}]`

	_, err := ParseDataset([]byte(payload))

	assert.Error(t, err, "the PR URL is validated at the parse boundary alongside the commit SHAs")
}

func TestUniqueCaseID_DisambiguatesOnlyAGenuineCollision(t *testing.T) {
	taken := map[string]string{}
	issue := func(url string) string {
		t.Helper()
		id, err := uniqueCaseID(Record{GithubPrURL: url}, taken)
		require.NoError(t, err)
		taken[id] = url
		return id
	}

	plain := issue("https://github.com/foo/bar-baz/pull/7")
	assert.Equal(t, "foo-bar-baz-pr-7", plain, "the plain format is kept when there is no collision — committed ids are immutable")

	colliding := issue("https://github.com/foo-bar/baz/pull/7")
	assert.NotEqual(t, plain, colliding,
		"foo-bar/baz and foo/bar-baz derive the same slug; the second must be disambiguated before any write")

	dotted := issue("https://github.com/foo/bar.baz/pull/7")
	assert.NotEqual(t, plain, dotted)
	assert.NotEqual(t, colliding, dotted)
}

func TestUniqueCaseID_RejectsADuplicateRecordBeforeWriting(t *testing.T) {
	taken := map[string]string{}
	rec := Record{GithubPrURL: "https://github.com/foo/bar/pull/7"}

	id, err := uniqueCaseID(rec, taken)
	require.NoError(t, err)
	taken[id] = rec.GithubPrURL

	_, err = uniqueCaseID(rec, taken)
	assert.Error(t, err, "the same record twice would silently overwrite its own diff")
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

func TestBuildSuite_ProducesThePinnedHashAndIdenticalBytes(t *testing.T) {
	// Comparing two runs to each other only catches Go map-iteration order,
	// which sort.Strings already removes — it cannot fail on a real regression.
	// The golden pin is what notices a changed case-id format, a changed
	// category mapping, or changed diff content. Manifest case ORDER is
	// deliberately outside the pin (ReproHashManifest re-sorts by id); the
	// byte-equality assertion below is what covers ordering, by failing if two
	// runs of the same records ever emit different suite.json bytes.
	const wantHash = "50d620ddd8629d52555780705b2835747d3064e3c0657e5f7c0919381228510b"

	recs := loadFixture(t)

	build := func() (string, map[string]string) {
		t.Helper()
		dir := t.TempDir()
		_, err := BuildSuite(context.Background(), Options{
			Records: recs, OutDir: dir, Suite: "standard-v1", SuiteVersion: "1.0.0",
			Fetcher: &fakeFetcher{},
		})
		require.NoError(t, err)
		h, err := benchmark.ReproHash(dir)
		require.NoError(t, err)
		return h, dirListing(t, dir)
	}

	hashA, bytesA := build()
	hashB, bytesB := build()

	assert.Equal(t, wantHash, hashA,
		"this fixture must keep producing the pinned hash; a change here is a reproducibility regression, not a test to update reflexively")
	assert.Equal(t, hashA, hashB, "two ingestions of the same records agree")
	assert.Equal(t, bytesA, bytesB,
		"and agree byte-for-byte across every emitted file, which is what this test's name claims")
}

func TestBuildSuite_RefusesToRebuildOverTheSameSuiteVersion(t *testing.T) {
	// The default -out is the committed benchmarks/standard-v1 at 1.0.0. Without
	// a guard, any casual re-run silently rewrites shipped cases and invalidates
	// the published hash — the one invariant the suite exists to hold.
	dir := t.TempDir()
	seedSuite(t, dir, "1.0.0")

	_, err := BuildSuite(context.Background(), Options{
		Records: loadFixture(t), OutDir: dir, Suite: "standard-v1", SuiteVersion: "1.0.0",
		Fetcher: &fakeFetcher{},
	})

	require.Error(t, err, "rebuilding over an existing suite at the same suite_version must be refused")
	assert.Contains(t, err.Error(), "1.0.0", "the operator is told which version is already published")
	assert.Contains(t, err.Error(), "force", "the error names the escape hatch")

	kept, readErr := os.ReadFile(filepath.Join(dir, "orphan.diff"))
	require.NoError(t, readErr, "a refused build must not have touched the directory")
	assert.Equal(t, "stale\n", string(kept))
}

func TestBuildSuite_PrunesDiffsTheNewManifestDoesNotReference(t *testing.T) {
	// Orphans from a prior, larger sample are not covered by ReproHash and are
	// invisible to benchmark.Load, so they ride into a commit unnoticed.
	dir := t.TempDir()
	seedSuite(t, dir, "0.9.0")

	_, err := BuildSuite(context.Background(), Options{
		Records: loadFixture(t), OutDir: dir, Suite: "standard-v1", SuiteVersion: "1.0.0",
		Fetcher: &fakeFetcher{},
	})
	require.NoError(t, err, "a different suite_version is a legitimate rebuild")

	assert.NoFileExists(t, filepath.Join(dir, "orphan.diff"),
		"a diff the new manifest does not reference is removed, not left to be committed")

	m, err := benchmark.Load(dir)
	require.NoError(t, err)
	referenced := make(map[string]bool, len(m.Cases))
	for _, c := range m.Cases {
		referenced[c.Diff] = true
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".diff" {
			continue
		}
		assert.True(t, referenced[e.Name()], "%s is on disk but absent from the manifest", e.Name())
	}
}

func TestBuildSuite_LeavesTheOutputDirectoryUntouchedWhenAFetchFails(t *testing.T) {
	// A 403 or 502 on the last record must not leave freshly-written diff bytes
	// beside the OLD suite.json — that combination silently stops matching the
	// published hash while still loading as a valid suite.
	dir := t.TempDir()
	seedSuite(t, dir, "0.9.0")
	before := dirListing(t, dir)

	_, err := BuildSuite(context.Background(), Options{
		Records: loadFixture(t), OutDir: dir, Suite: "standard-v1", SuiteVersion: "1.0.0",
		Fetcher: &fakeFetcher{err: errors.New("502 bad gateway"), failAfter: 1},
	})

	require.Error(t, err)
	assert.Equal(t, before, dirListing(t, dir),
		"an aborted build writes nothing into the output directory")
}

func TestBuildSuite_ResumesFromTheDiffCacheWithoutRefetching(t *testing.T) {
	// A sustained outage after retries are exhausted still aborts the build. The
	// cache is what keeps that from costing the whole API budget again: the
	// second run re-fetches only what the first never got.
	recs := loadFixture(t)
	require.Greater(t, len(recs), 2, "the fixture must have enough records to abort partway")
	cache := t.TempDir()

	first := &fakeFetcher{err: errors.New("502 bad gateway"), failAfter: 2}
	_, err := BuildSuite(context.Background(), Options{
		Records: recs, OutDir: t.TempDir(), Suite: "s", SuiteVersion: "1.0.0",
		Fetcher: first, CacheDir: cache,
	})
	require.Error(t, err, "the first run aborts partway")

	second := &fakeFetcher{}
	res, err := BuildSuite(context.Background(), Options{
		Records: recs, OutDir: t.TempDir(), Suite: "s", SuiteVersion: "1.0.0",
		Fetcher: second, CacheDir: cache,
	})
	require.NoError(t, err, "the second run completes")

	assert.Equal(t, len(recs), res.CasesWritten)
	assert.Len(t, second.calls, len(recs)-2,
		"the two diffs the first run already fetched are served from the cache, not re-fetched")
}

func TestBuildSuite_IgnoresAnEmptyCachedDiff(t *testing.T) {
	// A cache entry truncated by a crash mid-write must not be mistaken for a
	// completed fetch — a blank diff fails the whole build downstream.
	recs := loadFixture(t)
	cache := t.TempDir()
	id, err := CaseID(recs[0])
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cache, id+".diff"), nil, 0o644))

	f := &fakeFetcher{}
	_, err = BuildSuite(context.Background(), Options{
		Records: recs, OutDir: t.TempDir(), Suite: "s", SuiteVersion: "1.0.0",
		Fetcher: f, CacheDir: cache,
	})

	require.NoError(t, err)
	assert.Len(t, f.calls, len(recs), "a zero-byte cache entry is re-fetched, not served")
}

// seedSuite writes a prior build into dir: a suite.json at the given version
// plus a diff file no later manifest will reference.
func seedSuite(t *testing.T, dir, version string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "orphan.diff"), []byte("stale\n"), 0o644))
	prior := benchmark.Manifest{
		Suite:        "standard-v1",
		SuiteVersion: version,
		Cases:        []benchmark.Case{{ID: "orphan", Diff: "orphan.diff", ExpectedCategories: []string{"correctness"}}},
	}
	data, err := json.MarshalIndent(prior, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"), append(data, '\n'), 0o644))
}

// dirListing returns name+content for every regular file in dir, so a test can
// assert the directory is byte-for-byte unchanged.
func dirListing(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		out[e.Name()] = string(b)
	}
	return out
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

func TestBuildSuite_ValidatesSuiteIdentityBeforeFetching(t *testing.T) {
	// A typo'd -suite or -suite-version must fail before any network fetch or
	// file write, not after the whole API budget is spent (manifest.Validate
	// would only catch it at the end).
	for _, opts := range []Options{
		{Records: loadFixture(t), OutDir: t.TempDir(), Suite: "", SuiteVersion: "1.0.0"},
		{Records: loadFixture(t), OutDir: t.TempDir(), Suite: "   ", SuiteVersion: "1.0.0"},
		{Records: loadFixture(t), OutDir: t.TempDir(), Suite: "s", SuiteVersion: ""},
	} {
		f := &fakeFetcher{}
		opts.Fetcher = f

		_, err := BuildSuite(context.Background(), opts)

		require.Error(t, err, "suite identity %q/%q is required up front", opts.Suite, opts.SuiteVersion)
		assert.Empty(t, f.calls, "validation happens before any fetcher call")
	}
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

func TestBuildSuite_CountsPartiallyUnmappedRecords(t *testing.T) {
	res, err := BuildSuite(context.Background(), Options{
		Records: []Record{{GithubPrURL: "https://github.com/o/r/pull/1",
			SourceCommit: "aaaaaaa", TargetCommit: "bbbbbbb",
			Comments: []Comment{
				{Category: "Code Defect"},
				{Category: "Brand New Upstream Vocabulary"},
			}}},
		OutDir: t.TempDir(), Suite: "s", SuiteVersion: "1.0.0", Fetcher: &fakeFetcher{},
	})

	require.NoError(t, err)
	assert.Equal(t, 1, res.CasesWritten, "the record is kept on the strength of its mappable comment")
	assert.Equal(t, 1, res.UnmappedCategories,
		"the partial drop is reported — a silently shrunken expected set would flatter every scored recall")
}

func TestBuildSuite_WritesDiffFilesInsideTheSuiteDirectory(t *testing.T) {
	dir := t.TempDir()

	_, err := BuildSuite(context.Background(), Options{
		Records: loadFixture(t), OutDir: dir, Suite: "s", SuiteVersion: "1.0.0",
		Fetcher: &fakeFetcher{},
	})
	require.NoError(t, err)

	// Asserting only that the entries already inside dir are files pins "the
	// suite is flat", not "diffs stay inside the suite directory": an
	// implementation writing every diff to ../ would leave dir holding just
	// suite.json and still pass. Containment has to be asserted from the
	// manifest's own Diff values.
	m, err := benchmark.Load(dir)
	require.NoError(t, err)
	require.NotEmpty(t, m.Cases)

	onDisk := map[string]bool{}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, e.IsDir(), "the suite is flat: %s", e.Name())
		if filepath.Ext(e.Name()) == ".diff" {
			onDisk[e.Name()] = true
		}
	}

	referenced := map[string]bool{}
	for _, c := range m.Cases {
		assert.Equal(t, dir, filepath.Dir(filepath.Join(dir, c.Diff)),
			"case %q resolves outside the suite directory: %q", c.ID, c.Diff)
		referenced[c.Diff] = true
	}
	assert.Equal(t, referenced, onDisk,
		"the *.diff files on disk and the diffs the manifest names must be the same set")
}
