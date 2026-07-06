package history

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The storage layout lives in exactly one place: ShardDir and LegacyLedgerPath.
// The write hooks (atcr review, atcr resume) and the read path (atcr history)
// all derive locations from these helpers, so they cannot drift on where the
// monthly shards or the legacy flat ledger live.
func TestShardDir_Layout(t *testing.T) {
	assert.Equal(t, filepath.Join("repo", ".planning", "history"), ShardDir("repo"))
}

func TestLegacyLedgerPath_Layout(t *testing.T) {
	assert.Equal(t, filepath.Join("repo", ".atcr", "findings-history.jsonl"), LegacyLedgerPath("repo"))
}

// A record written under a root via the shared layout helper is always visible
// to a query rooted the same way. This is the contract the write hooks and the
// history read path both depend on — the fix for review/resume writing to a
// CWD-relative dir that `atcr history` (repo-root-relative) never read.
func TestShardDir_WriteReadAgree(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, Append(ShardPath(ShardDir(root), ts), []Record{{Timestamp: ts, ID: "x", File: "a.go"}}))

	recs, err := LoadAll(ShardDir(root), LegacyLedgerPath(root))
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "x", recs[0].ID)
}
