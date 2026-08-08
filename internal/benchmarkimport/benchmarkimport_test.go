package benchmarkimport

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fixturePath = "testdata/positive_samples_fixture.json"

func loadFixture(t *testing.T) []Record {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	require.NoError(t, err, "fixture must be readable")
	recs, err := ParseDataset(raw)
	require.NoError(t, err, "fixture must parse as a positive_samples.json payload")
	return recs
}

func TestParseDataset_ReadsRealFixtureShape(t *testing.T) {
	recs := loadFixture(t)

	require.Len(t, recs, 6, "fixture holds six PR records")

	first := recs[0]
	assert.Equal(t, "https://github.com/CherryHQ/cherry-studio/pull/5540", first.GithubPrURL,
		"record order is preserved from the source file")
	assert.NotEmpty(t, first.SourceCommit, "source_commit is the compare base")
	assert.NotEmpty(t, first.TargetCommit, "target_commit is the compare head")
	assert.NotEmpty(t, first.ProjectMainLanguage, "language travels with the record")
	assert.NotEmpty(t, first.Comments, "each record carries its ground-truth comments")
	assert.NotEmpty(t, first.Comments[0].Category, "each comment carries a source category")
	assert.NotEmpty(t, first.Comments[0].Path, "each comment cites a file path")
}

func TestParseDataset_RejectsEmptyAndMalformed(t *testing.T) {
	_, err := ParseDataset([]byte(`[]`))
	assert.Error(t, err, "an empty dataset is refused rather than silently producing an empty suite")

	_, err = ParseDataset([]byte(`{"not":"an array"}`))
	assert.Error(t, err, "a non-array payload is refused")

	_, err = ParseDataset([]byte(`not json at all`))
	assert.Error(t, err, "invalid JSON is refused")
}

func TestParseDataset_RejectsRecordMissingCommitPair(t *testing.T) {
	_, err := ParseDataset([]byte(`[{"githubPrUrl":"https://github.com/o/r/pull/1","source_commit":"","target_commit":"bbbbbbb","comments":[{"category":"Code Defect","path":"a.go"}]}]`))
	assert.Error(t, err, "a record without both commits cannot produce a diff, so it is refused at parse time")
}

func TestParseDataset_RejectsCommitValuesThatAreNotHexSHAs(t *testing.T) {
	// These reach git as bare arguments and the compare URL as path segments, so
	// an option-shaped or traversal-shaped value must never get that far.
	for _, bad := range []string{"--upload-pack=touch /tmp/pwned", "-n", "../../etc/passwd", "not a sha", ""} {
		payload := `[{"githubPrUrl":"https://github.com/o/r/pull/1","source_commit":"` + bad +
			`","target_commit":"aaaaaaa","comments":[{"category":"Code Defect","path":"a.go"}]}]`

		_, err := ParseDataset([]byte(payload))

		assert.Error(t, err, "commit value %q must be rejected before it reaches git", bad)
	}
}

func TestParseDataset_AcceptsShortAndFullHexSHAs(t *testing.T) {
	payload := `[{"githubPrUrl":"https://github.com/o/r/pull/1","source_commit":"1fae8db",` +
		`"target_commit":"ed57db2b34c2a7a955b2dc76583dce7ace02e628","comments":[{"category":"Code Defect","path":"a.go"}]}]`

	_, err := ParseDataset([]byte(payload))

	assert.NoError(t, err, "both abbreviated and full object names are legitimate upstream values")
}

func TestParseDataset_RejectsDuplicatePullRequestURLs(t *testing.T) {
	rec := `{"githubPrUrl":"https://github.com/o/r/pull/1","source_commit":"aaaaaaa","target_commit":"bbbbbbb","comments":[{"category":"Code Defect","path":"a.go"}]}`

	_, err := ParseDataset([]byte("[" + rec + "," + rec + "]"))

	assert.Error(t, err, "a duplicate PR URL would make sampling order-dependent and emit two cases sharing an id")
}

func TestSample_IsDeterministicForAFixedSeed(t *testing.T) {
	recs := loadFixture(t)

	a, err := Sample(recs, 4, 1337)
	require.NoError(t, err)
	b, err := Sample(recs, 4, 1337)
	require.NoError(t, err)

	require.Len(t, a, 4, "sample honors the requested size")
	assert.Equal(t, urls(a), urls(b), "the same seed selects the same records")
}

func TestSample_DiffersAcrossSeeds(t *testing.T) {
	recs := loadFixture(t)

	a, err := Sample(recs, 3, 1)
	require.NoError(t, err)
	b, err := Sample(recs, 3, 99)
	require.NoError(t, err)

	assert.NotEqual(t, urls(a), urls(b), "a different seed selects a different subset")
}

func TestSample_IsIndependentOfInputOrdering(t *testing.T) {
	recs := loadFixture(t)
	reversed := make([]Record, len(recs))
	for i, r := range recs {
		reversed[len(recs)-1-i] = r
	}

	a, err := Sample(recs, 4, 7)
	require.NoError(t, err)
	b, err := Sample(reversed, 4, 7)
	require.NoError(t, err)

	assert.Equal(t, urls(a), urls(b),
		"sampling canonicalizes order first, so a reshuffled source file yields the same sample")
}

func TestSample_OutputIsStablySorted(t *testing.T) {
	recs := loadFixture(t)

	got, err := Sample(recs, 5, 42)
	require.NoError(t, err)

	for i := 1; i < len(got); i++ {
		assert.Less(t, got[i-1].GithubPrURL, got[i].GithubPrURL,
			"sample output is sorted so the emitted suite ordering is reproducible")
	}
}

func TestSample_ClampsToAvailableRecords(t *testing.T) {
	recs := loadFixture(t)

	got, err := Sample(recs, 500, 42)
	require.NoError(t, err)
	assert.Len(t, got, len(recs), "asking for more records than exist yields every record")
}

func TestSample_RejectsNonPositiveSize(t *testing.T) {
	recs := loadFixture(t)

	_, err := Sample(recs, 0, 42)
	assert.Error(t, err, "a zero-size sample is a caller mistake, not an empty suite")

	_, err = Sample(recs, -3, 42)
	assert.Error(t, err, "a negative sample size is refused")
}

func urls(recs []Record) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.GithubPrURL)
	}
	return out
}
