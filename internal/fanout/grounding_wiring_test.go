package fanout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWritePool_DropsUngrounded is the Epic 14.1 AC2 integration point: WritePool
// drops a finding whose FILE:LINE is not anchored in the patch grounding data
// before it reaches the merged pool findings, while keeping a grounded one.
func TestWritePool_DropsUngrounded(t *testing.T) {
	dir := t.TempDir()
	content := "MEDIUM|auth.go:42|real bug in changed line|guard it|correctness|10|token := parseToken(r)\n" +
		"HIGH|auth.go:999|hallucinated issue far from any change|n/a|correctness|10|invented unrelated snippet\n"
	changed := payload.ChangedLines{
		"auth.go": {
			Ranges:      []payload.LineRange{{Start: 40, End: 45}},
			ChangedText: []string{"token := parseToken(r)"},
		},
	}
	pool := filepath.Join(dir, "pool")
	_, err := WritePool(pool, []Result{okResult("greta", content)}, changed)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, findingsFile))
	require.NoError(t, err)
	parsed, err := stream.ParseSource(data)
	require.NoError(t, err)
	require.Len(t, parsed.Findings, 1, "the ungrounded auth.go:999 finding must be dropped")
	assert.Equal(t, 42, parsed.Findings[0].Line)
}

// TestWritePool_NoGroundingKeepsAll confirms the gate is opt-in: with no
// ChangedLines passed (the existing call shape used across the suite), every
// finding is preserved so unrelated tests and the diff-ingestion path are
// unaffected.
func TestWritePool_NoGroundingKeepsAll(t *testing.T) {
	dir := t.TempDir()
	content := "MEDIUM|auth.go:42|a|f|correctness|10|x\n" +
		"HIGH|ghost.go:999|b|f|correctness|10|y\n"
	pool := filepath.Join(dir, "pool")
	_, err := WritePool(pool, []Result{okResult("greta", content)}, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, findingsFile))
	require.NoError(t, err)
	parsed, err := stream.ParseSource(data)
	require.NoError(t, err)
	require.Len(t, parsed.Findings, 2, "no grounding data means keep everything")
}

// TestWriteResumedAgents_DropsUngrounded guards the Epic 14.1 consistency gap:
// a resumed agent's fresh output must be grounded identically to a first-run
// agent's, so re-running a failed agent cannot reintroduce a hallucination a
// normal run would have dropped.
func TestWriteResumedAgents_DropsUngrounded(t *testing.T) {
	dir := t.TempDir()
	content := "MEDIUM|auth.go:42|real bug|f|correctness|10|token := parseToken(r)\n" +
		"HIGH|auth.go:999|hallucinated|f|correctness|10|invented unrelated snippet\n"
	changed := payload.ChangedLines{
		"auth.go": {
			Ranges:      []payload.LineRange{{Start: 40, End: 45}},
			ChangedText: []string{"token := parseToken(r)"},
		},
	}
	pool := filepath.Join(dir, "pool")
	require.NoError(t, writeResumedAgents(pool, []Result{okResult("greta", content)}, changed))

	data, err := os.ReadFile(filepath.Join(pool, poolRawAgentDir, "greta", findingsFile))
	require.NoError(t, err)
	parsed, err := stream.ParseSource(data)
	require.NoError(t, err)
	require.Len(t, parsed.Findings, 1, "resumed agent's ungrounded finding must be dropped")
	assert.Equal(t, 42, parsed.Findings[0].Line)
}

// TestWritePool_RecordsGroundingAudit is the Epic 14.1 audit signal: the pool
// summary records whether the grounding gate ran, and — when it did not — WHY,
// so a git failure that silently disabled the gate for a whole run is visible in
// summary.json instead of only on stderr.
func TestWritePool_RecordsGroundingAudit(t *testing.T) {
	readSummary := func(t *testing.T, pool string) PoolSummary {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(pool, summaryFile))
		require.NoError(t, err)
		var ps PoolSummary
		require.NoError(t, json.Unmarshal(data, &ps))
		return ps
	}
	body := "MEDIUM|auth.go:42|a|f|correctness|10|x\n"

	// Disabled with a reason: grounding_enabled=false and the reason is recorded.
	off := filepath.Join(t.TempDir(), "pool")
	_, err := writePool(off, []Result{okResult("greta", body)}, nil, "changed-lines computation failed: boom")
	require.NoError(t, err)
	psOff := readSummary(t, off)
	require.NotNil(t, psOff.GroundingEnabled, "grounding_enabled must be recorded, not omitted")
	assert.False(t, *psOff.GroundingEnabled)
	assert.Equal(t, "changed-lines computation failed: boom", psOff.GroundingDisabledReason)

	// Enabled: grounding_enabled=true and no reason is recorded.
	on := filepath.Join(t.TempDir(), "pool")
	changed := payload.ChangedLines{"auth.go": {Ranges: []payload.LineRange{{Start: 40, End: 45}}}}
	_, err = writePool(on, []Result{okResult("greta", body)}, changed, "")
	require.NoError(t, err)
	psOn := readSummary(t, on)
	require.NotNil(t, psOn.GroundingEnabled)
	assert.True(t, *psOn.GroundingEnabled)
	assert.Empty(t, psOn.GroundingDisabledReason)
}
