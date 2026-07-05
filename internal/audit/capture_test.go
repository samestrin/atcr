package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writePoolFindings lays down a minimal review dir with an 8-column per-source
// pool findings.txt (the artifact every review run writes via WritePool).
func writePoolFindings(t *testing.T, reviewDir, body string) {
	t.Helper()
	poolDir := filepath.Join(reviewDir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	content := "# atcr-findings/v1\n" + body
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "findings.txt"), []byte(content), 0o644))
}

func TestRecordReview_WritesExactlyOneRecordWithSeveritySummary(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, ".atcr", "reviews", "r1")
	writePoolFindings(t, reviewDir,
		"HIGH|internal/registry/load.go:42|unchecked error|handle it|CORRECTNESS|15|ev|greta\n"+
			"LOW|cmd/atcr/review.go:10|nit|rename|STYLE|5|ev|kai\n")

	auditPath := filepath.Join(root, ".atcr", "audit.log.jsonl")
	ts := time.Date(2026, 7, 5, 9, 30, 0, 0, time.UTC)
	n, err := RecordReview(auditPath, reviewDir, ts, 1234, "basesha", "headsha")
	require.NoError(t, err)
	assert.Equal(t, 1, n) // AC1: exactly one audit record per run

	recs, err := Load(auditPath)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, 1234, recs[0].PR)
	assert.Equal(t, "basesha", recs[0].Base)
	assert.Equal(t, "headsha", recs[0].Head)
	assert.True(t, ts.Equal(recs[0].Timestamp))
	assert.Equal(t, map[string]int{"HIGH": 1, "LOW": 1}, recs[0].Findings)
}

func TestRecordReview_MissingPoolStillWritesOneRecord(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, ".atcr", "reviews", "empty") // no sources/pool
	auditPath := filepath.Join(root, ".atcr", "audit.log.jsonl")

	n, err := RecordReview(auditPath, reviewDir, time.Now(), 0, "b", "h")
	require.NoError(t, err) // a missing pool file must never fail the review
	assert.Equal(t, 1, n)   // still exactly one record (AC1)

	recs, err := Load(auditPath)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "b", recs[0].Base)
	assert.Empty(t, recs[0].Findings) // no findings summary, but the run is recorded
	assert.Equal(t, 0, recs[0].PR)    // PR omitted for a non-PR run
}

func TestRecordReview_BadHeaderStillWritesOneRecord(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, ".atcr", "reviews", "badheader")
	poolDir := filepath.Join(reviewDir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	// A torn or tampered first line that lacks a valid version header must not
	// cause RecordReview to return an error; AC1 requires exactly one record.
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "findings.txt"), []byte("garbage header\nHIGH|a.go:1|p|f|C|1|e|greta\n"), 0o644))

	auditPath := filepath.Join(root, ".atcr", "audit.log.jsonl")
	n, err := RecordReview(auditPath, reviewDir, time.Now(), 7, "b", "h")
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	recs, err := Load(auditPath)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Empty(t, recs[0].Findings)
}

func TestRecordReview_DedupesByFindingKeepingMaxSeverity(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, ".atcr", "reviews", "dup")
	// The same finding reported by two reviewers at different severities: counts
	// must reflect ONE distinct finding at the highest severity, order-independent.
	writePoolFindings(t, reviewDir,
		"LOW|internal/registry/load.go:42|unchecked error|handle it|CORRECTNESS|15|ev|greta\n"+
			"HIGH|internal/registry/load.go:42|unchecked error|handle it|CORRECTNESS|15|ev|kai\n")

	auditPath := filepath.Join(root, ".atcr", "audit.log.jsonl")
	n, err := RecordReview(auditPath, reviewDir, time.Now(), 5, "b", "h")
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	recs, err := Load(auditPath)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, map[string]int{"HIGH": 1}, recs[0].Findings)
}
