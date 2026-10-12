package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coverageFixtureDir is a committed review tree holding only chunks.json
// records (Epic 35.16.13 T5): brad planned 64 two-file bins over src/f000-f127
// and reviewed 4 (the rest timed out, the measured 4-of-64 shape); kai planned
// 64 one-file bins over src/f000-f063 and reviewed all 64. Bins: 68/128. Files:
// kai's 64 plus brad's first 8 (already kai's) = 64/128, 50%.
func coverageFixtureDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "coverage"))
	require.NoError(t, err)
	return abs
}

const fixtureCoverageLine = "Coverage: 68/128 persona-bins reviewed, 64/128 files covered by at least one persona (50.0%)\n"

func TestReadRunCoverage_FixtureTotals(t *testing.T) {
	cov := readRunCoverage(t.Context(), coverageFixtureDir(t))
	require.NoError(t, cov.err)
	require.True(t, cov.measured)
	assert.Equal(t, fanout.Coverage{BinsReviewed: 68, BinsPlanned: 128, FilesCovered: 64, FilesInScope: 128}, cov.Coverage)

	var text bytes.Buffer
	writeCoverageLine(&text, cov)
	assert.Equal(t, fixtureCoverageLine, text.String())

	var axi bytes.Buffer
	require.NoError(t, writeReviewSummaryAXI(&axi, false, "id", "dir", summarySnapshot{}, cov))
	head, row, _ := strings.Cut(axi.String(), "\n")
	assert.Contains(t, head, "bins_reviewed")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(row), "68,128,64,128"), "row %q", row)

	// The legacy pipe format is frozen: it carries no coverage columns.
	var legacy bytes.Buffer
	require.NoError(t, writeReviewSummaryAXI(&legacy, true, "id", "dir", summarySnapshot{}, cov))
	assert.NotContains(t, legacy.String(), "bins_reviewed")
	assert.NotContains(t, legacy.String(), "|68|128|64|128")
}

func TestWriteCoverageLine_UnmeasuredPrintsNothing(t *testing.T) {
	var b bytes.Buffer
	writeCoverageLine(&b, runCoverage{})
	assert.Empty(t, b.String(), "a bulk or diff run's summary is unchanged")
}

func TestCoverageGate(t *testing.T) {
	fixture := readRunCoverage(t.Context(), coverageFixtureDir(t))

	var stderr bytes.Buffer
	err := coverageGate(&stderr, fixture, 90)
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err))
	assert.Contains(t, err.Error(), "64/128 files covered (50.0%)")

	assert.NoError(t, coverageGate(&stderr, fixture, 50), "at the threshold passes")

	stderr.Reset()
	assert.NoError(t, coverageGate(&stderr, runCoverage{}, 90), "nothing binned: nothing to gate")
	assert.Contains(t, stderr.String(), "--require-coverage not applied")

	err = coverageGate(&stderr, runCoverage{err: io.ErrUnexpectedEOF}, 0)
	require.Error(t, err, "an unreadable record fails closed even at 0%")
	assert.Equal(t, exitFailure, exitCode(err))
}

func TestReview_RequireCoverageOutOfRangeIsUsageError(t *testing.T) {
	isolate(t)
	for _, v := range []string{"150", "-1", "abc"} {
		code := execCmd(t, "review", "--require-coverage", v)
		assert.Equal(t, 2, code, "--require-coverage %s", v)
	}
}

// installCoverageFixture copies the committed chunks.json records into dir's pool.
func installCoverageFixture(t *testing.T, fixture, dir string) {
	t.Helper()
	src := filepath.Join(fixture, "sources", "pool", "raw", "agent")
	for _, agent := range []string{"brad", "kai"} {
		data, err := os.ReadFile(filepath.Join(src, agent, "chunks.json"))
		require.NoError(t, err)
		dst := filepath.Join(dir, "sources", "pool", "raw", "agent", agent, "chunks.json")
		require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, os.WriteFile(dst, data, 0o644))
	}
}

// The all-complete resume path runs no fan-out but still prints the coverage
// line, honours --require-coverage (accepted on --resume, decided on EST-816),
// and exits 1 only after reconciling; unflagged, the partial run exits 0.
func TestResume_AllCompleteReportsAndGatesCoverage(t *testing.T) {
	fixture := coverageFixtureDir(t) // resolved before isolate changes directory
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")
	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^"))
	installCoverageFixture(t, fixture, latestReviewDir(t))

	code, out := execResume(t, "review", "--resume", "latest", "--base", "HEAD^")
	require.Equal(t, 0, code, "unflagged partial coverage keeps exit 0: %s", out)
	assert.Contains(t, out, fixtureCoverageLine)

	code, out = execResume(t, "review", "--resume", "latest", "--base", "HEAD^", "--require-coverage", "90")
	require.Equal(t, 1, code, out)
	assert.Contains(t, out, fixtureCoverageLine, "summary is written before the gate fails")
	assert.Contains(t, out, "reconciled", "reconcile ran before the gate failed")
	assert.Contains(t, out, "below the threshold")

	code, out = execResume(t, "review", "--resume", "latest", "--base", "HEAD^", "--require-coverage", "50")
	require.Equal(t, 0, code, out)

	code, stdout, _ := execCmdSplit(t, "review", "--resume", "latest", "--base", "HEAD^", "--axi", "--require-coverage", "90")
	require.Equal(t, 1, code)
	assert.Contains(t, stdout, "bins_reviewed")
	assert.Contains(t, stdout, ",68,128,64,128")
}

// A diff review bins nothing: no coverage line, and --require-coverage is a
// noted no-op rather than a failure.
func TestReview_DiffRunCoverageUnmeasured(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, stderr := execCmdSplit(t, "review", "--base", "HEAD^", "--require-coverage", "90")
	require.Equal(t, 0, code, stderr)
	assert.NotContains(t, stdout, "Coverage:")
	assert.Contains(t, stderr, "--require-coverage not applied")
}

// failingContentProvider is liveMockProvider, except any request carrying
// marker answers 400 so that bin fails.
func failingContentProvider(t *testing.T, marker string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte(marker)) {
			http.Error(w, `{"error":{"message":"bad request"}}`, http.StatusBadRequest)
			return
		}
		content := "CRITICAL|a.txt:1|Unchecked call|Guard it|security|15|evidence"
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A fresh multi-chunk --all run where one bin fails: the coverage line names the
// shortfall, the unflagged run still exits 0, and --require-coverage 90 exits 1
// after the artifacts are written.
func TestReviewAll_PartialRunCoverageLineAndGate(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	// Each file exceeds the per-agent budget, so each is its own baseline bin.
	for name, ch := range map[string]byte{"big1.txt": 'x', "big2.txt": 'x', "big3.txt": 'y'} {
		require.NoError(t, os.WriteFile(name, bytes.Repeat([]byte{ch}, 80_000), 0o644))
	}
	gitRun(t, "add", "-A")
	gitRun(t, "commit", "-qm", "add big files")
	srv := failingContentProvider(t, strings.Repeat("y", 64))
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, stderr := execCmdSplit(t, "review", "--all", "--no-file-cache")
	require.Equal(t, 0, code, stderr)
	cov, ok, err := fanout.ReadCoverage(latestReviewDir(t))
	require.NoError(t, err)
	require.True(t, ok, "a multi-chunk --all run writes chunks.json")
	require.Less(t, cov.FilesCovered, cov.FilesInScope, "big3's bin failed")
	assert.Equal(t, cov.BinsPlanned-1, cov.BinsReviewed)
	assert.Contains(t, stdout, "Coverage: ")
	assert.Contains(t, stdout, "persona-bins reviewed")

	code, _, stderr = execCmdSplit(t, "review", "--all", "--no-file-cache", "--require-coverage", "90")
	require.Equal(t, 1, code, stderr)
	_, ok, err = fanout.ReadCoverage(latestReviewDir(t))
	require.NoError(t, err)
	assert.True(t, ok, "artifacts are written before the gate fails")
}

// An unreadable record is reported as unavailable, never as a figure.
func TestReadRunCoverage_CorruptRecordIsUnavailable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources", "pool", "raw", "agent", "brad", "chunks.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("{torn"), 0o644))

	cov := readRunCoverage(t.Context(), dir)
	require.Error(t, cov.err)
	assert.False(t, cov.measured)
	var b bytes.Buffer
	writeCoverageLine(&b, cov)
	assert.True(t, strings.HasPrefix(b.String(), "Coverage: unavailable ("), b.String())
}

// The resume path that dispatches pending agents reads coverage after its
// fan-out and gates on it last: --require-coverage 90 exits 1 after the summary,
// while the unflagged and at-threshold runs exit 0. Each case gets a fresh
// review, because a drained resume is all-complete on its next run.
func TestResume_PendingAgentsReportsAndGatesCoverage(t *testing.T) {
	fixture := coverageFixtureDir(t)
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"unflagged", nil, 0},
		{"at threshold", []string{"--require-coverage", "50"}, 0},
		{"below threshold", []string{"--require-coverage", "90"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			t.Setenv(testReviewKeyEnv, "secret")
			initGitRepoWithChange(t)
			srv := liveMockProvider(t)
			liveReviewConfig(t, srv.URL, "bruce", "robin")
			base := gitRevParse(t, "HEAD^")
			head := gitRevParse(t, "HEAD")
			dir := writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})
			installCoverageFixture(t, fixture, dir)

			args := append([]string{"review", "--resume", "latest", "--base", "HEAD^"}, tc.args...)
			code, out := execResume(t, args...)
			require.Equal(t, tc.code, code, out)
			assert.Contains(t, out, "1 completed, 1 pending", "the pending agent was dispatched")
			assert.Contains(t, out, fixtureCoverageLine, "summary is written before any gate failure")
			if tc.code != 0 {
				assert.Contains(t, out, "below the threshold")
			}
		})
	}
}
