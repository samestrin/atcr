package audit

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

func writePoolToon(t *testing.T, reviewDir string, body []byte) {
	t.Helper()
	poolDir := filepath.Join(reviewDir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "findings.toon"), body, 0o644))
}

func v2Pool(t *testing.T, findings []stream.Finding) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, stream.WriteSourceV2(&b, findings))
	return b.Bytes()
}

// The dedupe key uses the lossless PROBLEM from findings.toon: "x | y" and
// "x / y" are two findings, where the lossy .txt would merge them.
func TestRecordReview_DedupeKeyUsesLosslessToon(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, "r")
	writePoolFindings(t, reviewDir, "LOW|z.go:9|from txt|f|STYLE|1|e|kai\n")
	writePoolToon(t, reviewDir, v2Pool(t, []stream.Finding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x | y", Reviewer: "greta"},
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x / y", Reviewer: "kai"},
	}))

	auditPath := filepath.Join(root, "audit.jsonl")
	_, err := RecordReview(auditPath, reviewDir, time.Now(), 0, "b", "h")
	require.NoError(t, err)
	recs, err := Load(auditPath)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, map[string]int{"HIGH": 2}, recs[0].Findings)
}

// A corrupt findings.toon warns and records an empty summary, as a corrupt
// findings.txt does, and never falls back to the valid .txt beside it.
func TestRecordReview_CorruptToonWarnsWithoutFallback(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, "r")
	writePoolFindings(t, reviewDir, "HIGH|a.go:1|p|f|C|1|e|greta\n")
	writePoolToon(t, reviewDir, []byte(stream.VersionV2+"\nnot a table\n"))

	auditPath := filepath.Join(root, "audit.jsonl")
	var n int
	var err error
	stderr := captureStderr(t, func() { n, err = RecordReview(auditPath, reviewDir, time.Now(), 0, "b", "h") })
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Contains(t, stderr, "atcr: warning: audit: ")
	assert.Contains(t, stderr, "; writing empty severity summary")
	assert.Contains(t, stderr, filepath.Join(reviewDir, "sources", "pool", "findings.toon"), "the warning names the file that failed (TD-032)")
	recs, err := Load(auditPath)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Empty(t, recs[0].Findings, "the .txt's HIGH must not appear")
}

func TestRecordReview_UnreadableToonIsAReadError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-0 file")
	}
	root := t.TempDir()
	reviewDir := filepath.Join(root, "r")
	writePoolFindings(t, reviewDir, "HIGH|a.go:1|p|f|C|1|e|greta\n")
	writePoolToon(t, reviewDir, v2Pool(t, []stream.Finding{{Severity: "HIGH", File: "a.go", Line: 1, Reviewer: "greta"}}))
	toon := filepath.Join(reviewDir, "sources", "pool", "findings.toon")
	require.NoError(t, os.Chmod(toon, 0o000))
	t.Cleanup(func() { _ = os.Chmod(toon, 0o644) })

	_, err := RecordReview(filepath.Join(root, "audit.jsonl"), reviewDir, time.Now(), 0, "b", "h")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading pool findings: ")
}

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
