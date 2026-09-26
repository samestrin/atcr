package history

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()
	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	return buf.String()
}

func TestFindingID_StableAndSeverityIndependent(t *testing.T) {
	// Same file/line/problem => same id, regardless of severity (severity is
	// mutably re-settled by debate/verify, so it must not participate in the key).
	a := FindingID("internal/registry/load.go", 42, "unchecked error")
	b := FindingID("internal/registry/load.go", 42, "unchecked error")
	require.Equal(t, a, b)

	// A different problem or line yields a different id.
	assert.NotEqual(t, a, FindingID("internal/registry/load.go", 43, "unchecked error"))
	assert.NotEqual(t, a, FindingID("internal/registry/load.go", 42, "other problem"))
	assert.NotEqual(t, a, FindingID("internal/other/load.go", 42, "unchecked error"))

	// Id is a short hex string (16 hex chars = 8 bytes), never empty.
	assert.Len(t, a, 16)
}

func TestPackageOf(t *testing.T) {
	assert.Equal(t, "internal/registry", PackageOf("internal/registry/load.go"))
	assert.Equal(t, "cmd/atcr", PackageOf("cmd/atcr/review.go"))
	// A bare filename has no directory component => ".".
	assert.Equal(t, ".", PackageOf("main.go"))
}

func TestAppend_WritesJSONLAndCreatesParentDirs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".atcr", "findings-history.jsonl")
	ts := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)

	recs := []Record{
		{Timestamp: ts, Package: "internal/registry", Severity: "HIGH", ID: "abc", File: "internal/registry/load.go", Category: "CORRECTNESS"},
		{Timestamp: ts, Package: "cmd/atcr", Severity: "LOW", ID: "def", File: "cmd/atcr/review.go", Category: "STYLE"},
	}
	require.NoError(t, Append(path, recs))

	// A second append batch must not truncate the first (append-only ledger).
	require.NoError(t, Append(path, recs[:1]))

	loaded, err := Load(path)
	require.NoError(t, err)
	require.Len(t, loaded, 3)
	assert.Equal(t, "internal/registry", loaded[0].Package)
	assert.Equal(t, "HIGH", loaded[0].Severity)
	assert.True(t, ts.Equal(loaded[0].Timestamp))
}

// writePoolFindings lays down a minimal review dir with an 8-column per-source
// pool findings.txt (the artifact every review run writes via WritePool).
func writePoolFindings(t *testing.T, reviewDir, body string) {
	t.Helper()
	poolDir := filepath.Join(reviewDir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	content := "# atcr-findings/v1\n" + body
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "findings.txt"), []byte(content), 0o644))
}

func TestRecordReview_AppendsOneRecordPerPoolFinding(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, ".atcr", "reviews", "2026-07-04_x")
	writePoolFindings(t, reviewDir,
		"HIGH|internal/registry/load.go:42|unchecked error|handle it|CORRECTNESS|15|ev|greta\n"+
			"LOW|cmd/atcr/review.go:10|nit|rename|STYLE|5|ev|kai\n")

	histPath := filepath.Join(root, ".atcr", "findings-history.jsonl")
	ts := time.Date(2026, 7, 4, 9, 30, 0, 0, time.UTC)
	n, err := RecordReview(histPath, reviewDir, ts)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	recs, err := Load(histPath)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, "internal/registry", recs[0].Package)
	assert.Equal(t, "HIGH", recs[0].Severity)
	assert.Equal(t, "internal/registry/load.go", recs[0].File)
	assert.Equal(t, "CORRECTNESS", recs[0].Category)
	assert.Equal(t, FindingID("internal/registry/load.go", 42, "unchecked error"), recs[0].ID)
	assert.True(t, ts.Equal(recs[0].Timestamp))
}

func TestRecordReview_MissingPoolFileIsNoOp(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, ".atcr", "reviews", "empty") // no sources/pool
	histPath := filepath.Join(root, ".atcr", "findings-history.jsonl")

	n, err := RecordReview(histPath, reviewDir, time.Now())
	require.NoError(t, err) // absent pool findings must never fail the review
	assert.Equal(t, 0, n)

	// No history file should be created when there is nothing to record.
	_, statErr := os.Stat(histPath)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRecordReview_EmptyFindingsAppendsNothing(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, ".atcr", "reviews", "clean")
	writePoolFindings(t, reviewDir, "") // header only, no finding rows

	histPath := filepath.Join(root, ".atcr", "findings-history.jsonl")
	n, err := RecordReview(histPath, reviewDir, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestRecordReview_DedupeTakesMaxSeverity(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, ".atcr", "reviews", "2026-07-04_y")
	// Same finding reported by two reviewers with different severities; order must
	// not determine the stored severity.
	writePoolFindings(t, reviewDir,
		"LOW|internal/registry/load.go:42|unchecked error|handle it|CORRECTNESS|15|ev|greta\n"+
			"HIGH|internal/registry/load.go:42|unchecked error|handle it|CORRECTNESS|15|ev|kai\n")

	histPath := filepath.Join(root, ".atcr", "findings-history.jsonl")
	n, err := RecordReview(histPath, reviewDir, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	recs, err := Load(histPath)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "HIGH", recs[0].Severity)
}

func TestRecordReview_LogsSkippedRows(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, ".atcr", "reviews", "2026-07-04_z")
	// One valid row and one malformed row (too many columns).
	writePoolFindings(t, reviewDir,
		"HIGH|internal/registry/load.go:42|unchecked error|handle it|CORRECTNESS|15|ev|greta|extra\n"+
			"LOW|cmd/atcr/review.go:10|nit|rename|STYLE|5|ev|kai\n")

	histPath := filepath.Join(root, ".atcr", "findings-history.jsonl")
	var n int
	stderr := captureStderr(t, func() {
		var err error
		n, err = RecordReview(histPath, reviewDir, time.Now())
		require.NoError(t, err)
	})

	assert.Equal(t, 1, n)
	recs, err := Load(histPath)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "cmd/atcr/review.go", recs[0].File)
	assert.Contains(t, stderr, "1")
	assert.Contains(t, stderr, "skipped")
}

// AC5: FindingID's hash construction is FROZEN. Epic 35.13 made the .atcr/debt/
// store authoritative and every id in it is a FindingID digest, so changing the
// construction would silently invalidate the whole backlog — a stored id would
// stop matching the finding it names, and resolved items would resurface as new.
//
// The digests below are literals on purpose. Asserting FindingID against itself
// (same inputs => same id) passes no matter how the hash is built; only a pinned
// value catches a changed separator, field order, truncation length, or digest
// algorithm. If this test fails, the fix is to restore the construction, not to
// update the constants.
//
// Construction: sha256 over file + NUL + decimal line + NUL + problem, first 8
// bytes hex-encoded. Severity is deliberately not an input.
func TestFindingID_PinnedDigests(t *testing.T) {
	assert.Equal(t, "0fa44612c176f768", FindingID("internal/registry/load.go", 42, "unchecked error"))
	assert.Equal(t, "6b12f4d562e6d5f9", FindingID("a.go", 1, ""))
}

// writePoolToon writes a v2 pool findings.toon beside whatever findings.txt the
// test laid down.
func writePoolToon(t *testing.T, reviewDir string, findings []stream.Finding) {
	t.Helper()
	poolDir := filepath.Join(reviewDir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	var b bytes.Buffer
	require.NoError(t, stream.WriteSourceV2(&b, findings))
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "findings.toon"), b.Bytes(), 0o644))
}

// With findings.toon present the ledger is built from the lossless bytes: the
// "x | y" / "x / y" pair stays two findings, and the id hashes the real PROBLEM.
// On findings.txt, escapeField would have merged them into one.
func TestRecordReview_ReadsLosslessToon(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, "r")
	writePoolFindings(t, reviewDir, "LOW|z.go:9|from txt|f|STYLE|1|e|kai\n")
	writePoolToon(t, reviewDir, []stream.Finding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x | y", Category: "CORRECTNESS", Reviewer: "greta"},
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x / y", Category: "CORRECTNESS", Reviewer: "kai"},
	})

	histPath := filepath.Join(root, "h.jsonl")
	n, err := RecordReview(histPath, reviewDir, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	recs, err := Load(histPath)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, FindingID("a.go", 1, "x | y"), recs[0].ID)
}

// A corrupt findings.toon is a parse error, never a quiet read of the .txt.
func TestRecordReview_CorruptToonErrorsWithoutFallback(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, "r")
	writePoolFindings(t, reviewDir, "HIGH|a.go:1|p|f|C|1|e|greta\n")
	require.NoError(t, os.WriteFile(filepath.Join(reviewDir, "sources", "pool", "findings.toon"),
		[]byte(stream.VersionV2+"\nnot a table\n"), 0o644))

	histPath := filepath.Join(root, "h.jsonl")
	n, err := RecordReview(histPath, reviewDir, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing pool findings: ")
	assert.Contains(t, err.Error(), filepath.Join(reviewDir, "sources", "pool", "findings.toon"), "the error names the file that failed (TD-032)")
	assert.Zero(t, n)
	assert.NoFileExists(t, histPath)
}

func TestRecordReview_UnreadableToonIsAReadError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-0 file")
	}
	root := t.TempDir()
	reviewDir := filepath.Join(root, "r")
	writePoolFindings(t, reviewDir, "HIGH|a.go:1|p|f|C|1|e|greta\n")
	writePoolToon(t, reviewDir, []stream.Finding{{Severity: "HIGH", File: "a.go", Line: 1, Reviewer: "greta"}})
	toon := filepath.Join(reviewDir, "sources", "pool", "findings.toon")
	require.NoError(t, os.Chmod(toon, 0o000))
	t.Cleanup(func() { _ = os.Chmod(toon, 0o644) })

	_, err := RecordReview(filepath.Join(root, "h.jsonl"), reviewDir, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading pool findings: ")
}

// A findings.toon-only pool is never "missing"; a symlinked findings.toon with
// no findings.txt is.
func TestRecordReview_ToonOnlyAndSymlinkToon(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, "r")
	writePoolToon(t, reviewDir, []stream.Finding{{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Reviewer: "greta"}})
	n, err := RecordReview(filepath.Join(root, "h.jsonl"), reviewDir, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	linked := filepath.Join(root, "linked")
	poolDir := filepath.Join(linked, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	if err := os.Symlink(filepath.Join(reviewDir, "sources", "pool", "findings.toon"), filepath.Join(poolDir, "findings.toon")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	n, err = RecordReview(filepath.Join(root, "h2.jsonl"), linked, time.Now())
	require.NoError(t, err)
	assert.Zero(t, n, "a symlinked findings.toon is absent, so the pool is missing")
}
