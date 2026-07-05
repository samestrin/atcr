package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppend_EmptyRecordsIsNoOp(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".atcr", "findings-history.jsonl")
	require.NoError(t, Append(path, nil))
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "no ledger file should be created for an empty batch")
}

func TestLoad_AbsentLedgerIsEmptyNotError(t *testing.T) {
	recs, err := Load(filepath.Join(t.TempDir(), "nope.jsonl"))
	require.NoError(t, err)
	assert.Nil(t, recs)
}

func TestLoad_SkipsBlankLinesAndParsesInOrder(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "h.jsonl")
	ts := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	require.NoError(t, Append(path, []Record{
		{Timestamp: ts, Package: "a", Severity: "HIGH", ID: "1"},
	}))
	// Inject a blank line between records; it must be skipped, not fail parsing.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString("\n   \n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, Append(path, []Record{
		{Timestamp: ts, Package: "b", Severity: "LOW", ID: "2"},
	}))

	recs, err := Load(path)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, "a", recs[0].Package)
	assert.Equal(t, "b", recs[1].Package)
}

func TestLoad_SkipsMalformedLineAndKeepsRest(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bad.jsonl")
	ts := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	// A torn/garbage line between two valid records must not brick the read: the
	// bad line is skipped and the two valid records survive.
	good1, _ := jsonLine(t, Record{Timestamp: ts, Package: "a", Severity: "HIGH", ID: "1"})
	good2, _ := jsonLine(t, Record{Timestamp: ts, Package: "b", Severity: "LOW", ID: "2"})
	require.NoError(t, os.WriteFile(path, []byte(good1+"{not json}\n"+good2), 0o644))

	recs, err := Load(path)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, "a", recs[0].Package)
	assert.Equal(t, "b", recs[1].Package)
}

func TestRecordReview_DedupesDuplicateReviewerRowsWithinRun(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, ".atcr", "reviews", "dup")
	// Two reviewers (greta, kai) report the byte-identical finding, plus one
	// distinct finding. The identical pair shares an id and must collapse to one
	// record; total = 2 records, not 3.
	writePoolFindings(t, reviewDir,
		"HIGH|internal/registry/load.go:42|unchecked error|handle it|CORRECTNESS|15|ev|greta\n"+
			"HIGH|internal/registry/load.go:42|unchecked error|handle it|CORRECTNESS|15|ev|kai\n"+
			"LOW|cmd/atcr/review.go:10|nit|rename|STYLE|5|ev|greta\n")

	histPath := filepath.Join(root, ".atcr", "findings-history.jsonl")
	n, err := RecordReview(histPath, reviewDir, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 2, n, "duplicate reviewer rows for the same finding must dedupe to one record")

	recs, err := Load(histPath)
	require.NoError(t, err)
	require.Len(t, recs, 2)
}

// jsonLine encodes a record to a single JSONL line (trailing newline).
func jsonLine(t *testing.T, r Record) (string, error) {
	t.Helper()
	b, err := json.Marshal(r)
	require.NoError(t, err)
	return string(b) + "\n", nil
}

func TestRecordReview_MalformedFindingsHeaderIsError(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(root, ".atcr", "reviews", "bad")
	poolDir := filepath.Join(reviewDir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	// Missing the required "# atcr-findings/v1" version header => ParseSource errors.
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "findings.txt"),
		[]byte("HIGH|a/b.go:1|p|f|C|5|e|greta\n"), 0o644))

	_, err := RecordReview(filepath.Join(root, ".atcr", "findings-history.jsonl"), reviewDir, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing pool findings")
}
