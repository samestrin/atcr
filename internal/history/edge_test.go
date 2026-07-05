package history

import (
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

func TestLoad_MalformedLineIsError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bad.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("{not json}\n"), 0o644))
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 1")
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
