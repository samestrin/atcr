package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/audit"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/history"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toonPoolFindings is what findings.toon holds in these tests. The .txt beside
// it holds different content (stalePoolTxt), so a reader that read the .txt is
// visible from its output.
var toonPoolFindings = []stream.Finding{
	{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x | y", Fix: "a | b", Category: "correctness", EstMinutes: 5, Evidence: "l1\nl2", Reviewer: "greta"},
	{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x / y", Fix: "f", Category: "correctness", EstMinutes: 5, Evidence: "e", Reviewer: "greta"},
}

const stalePoolTxt = stream.Version + "\nLOW|z.go:9|from txt|f|style|1|e|greta\n"

func writeToonPool(t *testing.T, dir string, toon []byte, txt string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	if toon != nil {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "findings.toon"), toon, 0o600))
	}
	if txt != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "findings.txt"), []byte(txt), 0o600))
	}
}

func toonBytes(t *testing.T, findings []stream.Finding) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, stream.WriteSourceV2(&b, findings))
	return b.Bytes()
}

func TestBenchmarkReaders_PreferToon(t *testing.T) {
	dir := t.TempDir()
	writeToonPool(t, filepath.Join(dir, "sources", "pool"), toonBytes(t, toonPoolFindings), stalePoolTxt)

	cats, err := readCaseFindings(dir)
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"greta": {"correctness", "correctness"}}, cats)

	located, categorical, unattributed, missing, err := readCaseFindingsLocated(dir, map[string]bool{"greta": true})
	require.NoError(t, err)
	assert.False(t, missing)
	assert.Zero(t, unattributed, "a well-formed .toon yields no skipped rows")
	assert.Len(t, located["greta"], 2)
	assert.Equal(t, []string{"correctness", "correctness"}, categorical["greta"])
}

func TestBenchmarkReaders_ToonOnlyIsNotMissing(t *testing.T) {
	dir := t.TempDir()
	writeToonPool(t, filepath.Join(dir, "sources", "pool"), toonBytes(t, toonPoolFindings), "")
	_, _, _, missing, err := readCaseFindingsLocated(dir, map[string]bool{"greta": true})
	require.NoError(t, err)
	assert.False(t, missing)
}

// Neither file, or a symlinked .toon with no .txt, reads as missing. The
// selection error is %w-wrapped, so this also proves the readers test absence
// with errors.Is rather than os.IsNotExist.
func TestBenchmarkReaders_NeitherFileIsMissing(t *testing.T) {
	t.Run("empty pool dir", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
		assertBenchmarkMissing(t, dir)
	})
	t.Run("symlinked toon only", func(t *testing.T) {
		dir := t.TempDir()
		pool := filepath.Join(dir, "sources", "pool")
		require.NoError(t, os.MkdirAll(pool, 0o755))
		target := filepath.Join(t.TempDir(), "t.toon")
		require.NoError(t, os.WriteFile(target, toonBytes(t, toonPoolFindings), 0o600))
		if err := os.Symlink(target, filepath.Join(pool, "findings.toon")); err != nil {
			t.Skipf("symlink unsupported: %v", err)
		}
		assertBenchmarkMissing(t, dir)
	})
}

func assertBenchmarkMissing(t *testing.T, dir string) {
	t.Helper()
	cats, err := readCaseFindings(dir)
	require.NoError(t, err)
	assert.Empty(t, cats)
	located, categorical, unattributed, missing, err := readCaseFindingsLocated(dir, map[string]bool{"greta": true})
	require.NoError(t, err)
	assert.True(t, missing)
	assert.Empty(t, located)
	assert.Empty(t, categorical)
	assert.Zero(t, unattributed)
}

// A corrupt .toon is an error for both readers, never a read of the .txt.
func TestBenchmarkReaders_CorruptToonErrorsWithoutFallback(t *testing.T) {
	dir := t.TempDir()
	writeToonPool(t, filepath.Join(dir, "sources", "pool"), []byte(stream.VersionV2+"\nnot a table\n"), stalePoolTxt)

	_, err := readCaseFindings(dir)
	require.Error(t, err)
	_, _, _, missing, err := readCaseFindingsLocated(dir, map[string]bool{"greta": true})
	require.Error(t, err)
	assert.False(t, missing)
}

func TestBenchmarkReaders_UnreadableToonErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-0 file")
	}
	dir := t.TempDir()
	pool := filepath.Join(dir, "sources", "pool")
	writeToonPool(t, pool, toonBytes(t, toonPoolFindings), stalePoolTxt)
	toon := filepath.Join(pool, "findings.toon")
	require.NoError(t, os.Chmod(toon, 0o000))
	t.Cleanup(func() { _ = os.Chmod(toon, 0o644) })

	_, err := readCaseFindings(dir)
	require.Error(t, err)
	_, _, _, missing, err := readCaseFindingsLocated(dir, map[string]bool{"greta": true})
	require.Error(t, err)
	assert.False(t, missing)
}

// AC 07-01 Scenario 4: the six readers of one review directory all read the
// .toon. The four pool readers read sources/pool; Discover and RebuildPool read
// the per-agent leaf. Each directory holds a .toon and a disagreeing .txt.
func TestAllSixReaders_SelectTheSameFile(t *testing.T) {
	reviewDir := t.TempDir()
	poolDir := filepath.Join(reviewDir, "sources", "pool")
	agentDir := filepath.Join(poolDir, "raw", "agent", "greta")
	writeToonPool(t, poolDir, toonBytes(t, toonPoolFindings), stalePoolTxt)
	writeToonPool(t, agentDir, toonBytes(t, toonPoolFindings), stalePoolTxt)
	require.NoError(t, fanout.WriteStatus(filepath.Join(agentDir, "status.json"),
		&fanout.AgentStatus{Agent: "greta", Status: fanout.StatusOK}))

	// history: two distinct ids (the .txt would give one LOW record).
	histPath := filepath.Join(t.TempDir(), "h.jsonl")
	n, err := history.RecordReview(histPath, reviewDir, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 2, n, "history")

	// audit: two HIGH (the .txt would give LOW:1).
	auditPath := filepath.Join(t.TempDir(), "a.jsonl")
	_, err = audit.RecordReview(auditPath, reviewDir, time.Now(), 0, "b", "h")
	require.NoError(t, err)
	recs, err := audit.Load(auditPath)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, map[string]int{"HIGH": 2}, recs[0].Findings, "audit")

	cats, err := readCaseFindings(reviewDir)
	require.NoError(t, err)
	assert.Equal(t, []string{"correctness", "correctness"}, cats["greta"], "readCaseFindings")

	located, _, _, _, err := readCaseFindingsLocated(reviewDir, map[string]bool{"greta": true})
	require.NoError(t, err)
	assert.Len(t, located["greta"], 2, "readCaseFindingsLocated")

	sources, err := reconcile.Discover(filepath.Join(reviewDir, "sources"), nil)
	require.NoError(t, err)
	require.Len(t, sources, 1)
	assert.Equal(t, toonPoolFindings, sources[0].Findings, "Discover")

	// RebuildPool last: it rewrites the merged pool files.
	_, _, err = fanout.RebuildPool(context.Background(), poolDir, []string{"greta"})
	require.NoError(t, err)
	res, err := stream.ReadPoolFindings(poolDir)
	require.NoError(t, err)
	assert.Equal(t, toonPoolFindings, res.Findings, "RebuildPool")
}
