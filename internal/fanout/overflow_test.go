package fanout

import (
	"errors"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// multiFileDiff builds a small unified diff with n files, each with `lines`
// added lines, so chunkDiff has real file boundaries to split on.
func multiFileDiff(n, lines int) string {
	var b []byte
	for i := 0; i < n; i++ {
		path := string(rune('a' + i))
		b = append(b, []byte("diff --git a/"+path+".go b/"+path+".go\n")...)
		b = append(b, []byte("--- a/"+path+".go\n+++ b/"+path+".go\n")...)
		for j := 0; j < lines; j++ {
			b = append(b, []byte("+line\n")...)
		}
	}
	return string(b)
}

// TestApplyOverflowPolicy_ChunkDelegatesToChunkDiff proves the chunk arm returns
// exactly what chunkDiff would produce directly — no reimplementation, no drift.
func TestApplyOverflowPolicy_ChunkDelegatesToChunkDiff(t *testing.T) {
	diff := multiFileDiff(4, 3) // 4 files, ~5 lines each
	maxLines := 6               // small so it splits into multiple chunks

	res, err := applyOverflowPolicy(OverflowChunk, diff, maxLines, nil, 0)
	require.NoError(t, err)
	assert.Equal(t, "chunk", res.Action)
	assert.Equal(t, chunkDiff(diff, maxLines), res.Chunks, "chunk arm must delegate to chunkDiff")
	assert.Greater(t, len(res.Chunks), 1, "small maxLines should split a 4-file diff")

	// Lossless: concatenating chunk contents reproduces the diff exactly (zero
	// files dropped, F3/AC3).
	var joined string
	for _, c := range res.Chunks {
		joined += c
	}
	assert.Equal(t, diff, joined, "chunk arm must drop zero content")

	// truncate-only fields stay zero on the chunk arm.
	assert.Nil(t, res.Kept)
	assert.False(t, res.Truncation.Truncated)
}

// TestApplyOverflowPolicy_ChunkUnlimitedMaxLines mirrors chunkDiff's documented
// maxLines<=0 "single chunk" behavior through the dispatch.
func TestApplyOverflowPolicy_ChunkUnlimitedMaxLines(t *testing.T) {
	diff := multiFileDiff(3, 2)
	res, err := applyOverflowPolicy(OverflowChunk, diff, 0, nil, 0)
	require.NoError(t, err)
	assert.Equal(t, "chunk", res.Action)
	require.Len(t, res.Chunks, 1, "maxLines<=0 disables chunking -> one chunk")
	assert.Equal(t, diff, res.Chunks[0])
}

// TestApplyOverflowPolicy_TruncateDelegatesToApplyByteBudget proves the truncate
// arm returns the same kept/dropped result ApplyByteBudget would produce, and
// carries the Truncation record unmodified.
func TestApplyOverflowPolicy_TruncateDelegatesToApplyByteBudget(t *testing.T) {
	entries := []payload.FileEntry{
		{Path: "big.go", Size: 1000, Body: "x"},
		{Path: "small.go", Size: 10, Body: "y"},
	}
	budget := int64(100) // forces the 1000-byte file to be shed

	wantKept, wantTrunc := payload.ApplyByteBudget(entries, budget)

	res, err := applyOverflowPolicy(OverflowTruncate, "", 0, entries, budget)
	require.NoError(t, err)
	assert.Equal(t, "truncate", res.Action)
	assert.Equal(t, wantKept, res.Kept, "truncate arm must delegate to ApplyByteBudget")
	assert.Equal(t, wantTrunc, res.Truncation, "Truncation record passed through unmodified")
	assert.True(t, res.Truncation.Truncated)
	assert.Equal(t, []string{"big.go"}, res.Truncation.FilesDropped)

	// chunk-only field stays nil on the truncate arm.
	assert.Nil(t, res.Chunks)
}

// TestApplyOverflowPolicy_TruncateUnderBudget: nothing dropped when entries fit.
func TestApplyOverflowPolicy_TruncateUnderBudget(t *testing.T) {
	entries := []payload.FileEntry{{Path: "a.go", Size: 10, Body: "x"}}
	res, err := applyOverflowPolicy(OverflowTruncate, "", 0, entries, 1000)
	require.NoError(t, err)
	assert.Equal(t, "truncate", res.Action)
	assert.False(t, res.Truncation.Truncated)
	assert.Empty(t, res.Truncation.FilesDropped)
	assert.Len(t, res.Kept, 1)
}

// TestApplyOverflowPolicy_FallbackErrors: fallback is recognized but errors with
// a clear, provenance-referencing message and produces no chunks/truncation.
func TestApplyOverflowPolicy_FallbackErrors(t *testing.T) {
	entries := []payload.FileEntry{{Path: "a.go", Size: 10}}
	res, err := applyOverflowPolicy(OverflowFallback, multiFileDiff(3, 2), 4, entries, 5)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFallbackUnavailable)
	assert.Contains(t, err.Error(), "fallback", "message names the fallback policy")
	// No side effects: dispatch performed no model swap, chunking, or shedding.
	assert.Equal(t, OverflowResult{}, res, "fallback arm returns the zero result")
}

// TestApplyOverflowPolicy_FailErrors: fail returns immediately, no chunking or
// truncation attempted first.
func TestApplyOverflowPolicy_FailErrors(t *testing.T) {
	entries := []payload.FileEntry{{Path: "a.go", Size: 10}}
	res, err := applyOverflowPolicy(OverflowFail, multiFileDiff(3, 2), 4, entries, 5)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOverflowPolicyFail)
	assert.Equal(t, OverflowResult{}, res, "fail arm returns the zero result")
}

// TestApplyOverflowPolicy_UnrecognizedErrors: an unknown/empty string returns a
// clear error distinct from the fallback/fail sentinels — never a silent default.
func TestApplyOverflowPolicy_UnrecognizedErrors(t *testing.T) {
	for _, policy := range []string{"", "CHUNK", "shed", "unknown"} {
		res, err := applyOverflowPolicy(policy, multiFileDiff(2, 2), 4, nil, 0)
		require.Errorf(t, err, "policy %q must error", policy)
		assert.Contains(t, err.Error(), "unrecognized on_overflow policy")
		assert.Contains(t, err.Error(), policy)
		assert.NotErrorIs(t, err, ErrFallbackUnavailable, "distinct from fallback error")
		assert.NotErrorIs(t, err, ErrOverflowPolicyFail, "distinct from fail error")
		assert.Equal(t, OverflowResult{}, res)
	}
}

// TestOverflowSentinelsDistinct guards that the two error arms are not aliased to
// the same sentinel (a copy-paste hazard).
func TestOverflowSentinelsDistinct(t *testing.T) {
	assert.False(t, errors.Is(ErrFallbackUnavailable, ErrOverflowPolicyFail))
	assert.False(t, errors.Is(ErrOverflowPolicyFail, ErrFallbackUnavailable))
}
