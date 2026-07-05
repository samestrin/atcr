package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppend_WritesJSONLAndCreatesParentDirs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".atcr", "audit.log.jsonl")
	ts := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)

	recs := []Record{
		{Timestamp: ts, PR: 1234, Base: "abc123", Head: "def456", Findings: map[string]int{"HIGH": 1, "LOW": 2}},
	}
	require.NoError(t, Append(path, recs))

	// A second append must not truncate the first (append-only ledger).
	require.NoError(t, Append(path, []Record{{Timestamp: ts, Base: "abc123", Head: "def456"}}))

	loaded, err := Load(path)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.Equal(t, 1234, loaded[0].PR)
	assert.Equal(t, "abc123", loaded[0].Base)
	assert.Equal(t, 1, loaded[0].Findings["HIGH"])
	assert.True(t, ts.Equal(loaded[0].Timestamp))
	// The second record has no PR — omitempty leaves it zero on read.
	assert.Equal(t, 0, loaded[1].PR)
}

func TestAppend_EmptySliceIsNoOp(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".atcr", "audit.log.jsonl")
	require.NoError(t, Append(path, nil))
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "no file should be created for an empty batch")
}
