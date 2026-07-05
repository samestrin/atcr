package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
