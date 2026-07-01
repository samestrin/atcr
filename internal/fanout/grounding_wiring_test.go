package fanout

import (
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
	_, err := WritePool(pool, []Result{okResult("greta", content)})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, findingsFile))
	require.NoError(t, err)
	parsed, err := stream.ParseSource(data)
	require.NoError(t, err)
	require.Len(t, parsed.Findings, 2, "no grounding data means keep everything")
}
