package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_AbsentLedgerIsEmptyNotError(t *testing.T) {
	recs, err := Load(filepath.Join(t.TempDir(), "nope.jsonl"))
	require.NoError(t, err) // a project that never ran a review is a valid empty audit trail
	assert.Empty(t, recs)
}

func TestLoad_SkipsBlankAndMalformedLines(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "audit.log.jsonl")
	good := `{"ts":"2026-07-05T12:00:00Z","pr":7,"base":"aaa","head":"bbb","findings":{"HIGH":1}}`
	content := strings.Join([]string{
		good,
		"",                // blank
		"not json at all", // malformed
		good,
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	recs, err := Load(path)
	require.NoError(t, err) // one torn/stray line must not brick the whole read
	require.Len(t, recs, 2)
	assert.Equal(t, 7, recs[0].PR)
	assert.Equal(t, "aaa", recs[0].Base)
	assert.True(t, time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC).Equal(recs[0].Timestamp))
}

func TestLoad_RecoversRecordsAroundOversizedLine(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "audit.log.jsonl")
	rec := `{"ts":"2026-07-05T12:00:00Z","pr":7,"base":"aaa","head":"bbb"}`

	var b strings.Builder
	b.WriteString(rec + "\n")
	b.WriteString(strings.Repeat("x", 2*1024*1024) + "\n") // > 1MiB: trips the scanner's token cap
	b.WriteString(rec + "\n")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))

	// The record before AND after the oversized line are both recovered; the
	// oversized fragment is skipped, not fatal.
	recs, err := Load(path)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, 7, recs[0].PR)
	assert.Equal(t, 7, recs[1].PR)
}
