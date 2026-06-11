package payload

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func entries(pairs ...any) []FileEntry {
	var out []FileEntry
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, FileEntry{Path: pairs[i].(string), Size: int64(pairs[i+1].(int))})
	}
	return out
}

func keptPaths(kept []FileEntry) []string {
	out := make([]string, len(kept))
	for i, e := range kept {
		out[i] = e.Path
	}
	return out
}

func TestBudget_UnderLimit(t *testing.T) {
	in := entries("a", 60000, "b", 30000, "c", 20000, "d", 10000)
	kept, tr := ApplyByteBudget(in, 200000)
	assert.False(t, tr.Truncated)
	assert.Empty(t, tr.FilesDropped)
	assert.Len(t, kept, 4)
}

func TestBudget_OverLimit_DropsSmallestFirst(t *testing.T) {
	// A=60KB B=30KB C=20KB D=10KB, budget 100KB → drop D then C → keep A,B.
	in := entries("A", 60000, "B", 30000, "C", 20000, "D", 10000)
	kept, tr := ApplyByteBudget(in, 100000)
	assert.True(t, tr.Truncated)
	assert.Equal(t, []string{"C", "D"}, tr.FilesDropped)
	assert.ElementsMatch(t, []string{"A", "B"}, keptPaths(kept))
}

func TestBudget_SingleFileExceeds(t *testing.T) {
	in := entries("big", 15000)
	kept, tr := ApplyByteBudget(in, 10000)
	assert.True(t, tr.Truncated)
	assert.Equal(t, []string{"big"}, tr.FilesDropped)
	assert.Empty(t, kept)
}

func TestBudget_AllFilesDropped(t *testing.T) {
	in := entries("a", 500, "b", 600, "c", 700)
	kept, tr := ApplyByteBudget(in, 100)
	assert.True(t, tr.Truncated)
	assert.Equal(t, []string{"a", "b", "c"}, tr.FilesDropped)
	assert.Empty(t, kept)
}

func TestBudget_ZeroIsUnlimited(t *testing.T) {
	in := entries("a", 1_000_000, "b", 2_000_000)
	kept, tr := ApplyByteBudget(in, 0)
	assert.False(t, tr.Truncated)
	assert.Empty(t, tr.FilesDropped)
	assert.Len(t, kept, 2)
}

func TestBudget_ExactFit(t *testing.T) {
	in := entries("a", 30000, "b", 20000)
	kept, tr := ApplyByteBudget(in, 50000)
	assert.False(t, tr.Truncated)
	assert.Len(t, kept, 2)
}

func TestBudget_Deterministic(t *testing.T) {
	in := entries("a", 60000, "b", 30000, "c", 20000, "d", 10000)
	k1, t1 := ApplyByteBudget(in, 100000)
	k2, t2 := ApplyByteBudget(in, 100000)
	assert.Equal(t, t1, t2)
	assert.Equal(t, keptPaths(k1), keptPaths(k2))
}

func TestBudget_TieBreaking(t *testing.T) {
	// Equal sizes: drop the alphabetically-first paths first. Budget fits one.
	in := entries("zeta", 100, "alpha", 100, "mike", 100)
	_, tr := ApplyByteBudget(in, 100)
	// total 300, budget 100 → drop two smallest-by-path: alpha, mike.
	assert.Equal(t, []string{"alpha", "mike"}, tr.FilesDropped)
}

func TestBudget_DuplicatePaths(t *testing.T) {
	// Two entries share a path; each must be accounted for independently.
	in := []FileEntry{
		{Path: "dup", Size: 40000},
		{Path: "dup", Size: 40000},
		{Path: "small", Size: 5000},
	}
	kept, tr := ApplyByteBudget(in, 50000)
	// total 85000 > 50000. Drop smallest-first: small(5000)→80000 over,
	// then a dup(40000)→40000 under. Two files dropped, one dup remains.
	assert.True(t, tr.Truncated)
	assert.Len(t, tr.FilesDropped, 2)
	var keptBytes int64
	for _, e := range kept {
		keptBytes += e.Size
	}
	assert.LessOrEqual(t, keptBytes, int64(50000))
}

func TestBudget_ZeroSizeFiles(t *testing.T) {
	in := entries("empty", 0, "big", 60000, "small", 10000)
	_, tr := ApplyByteBudget(in, 50000)
	// zero-size files sort first (smallest); they are dropped per the
	// smallest-first rule even though they free no bytes.
	assert.True(t, tr.Truncated)
	assert.Contains(t, tr.FilesDropped, "empty")
}

func TestBudget_NegativeBudget(t *testing.T) {
	err := ValidateBudget(-1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "byte budget must be >= 0, got -1")
	assert.NoError(t, ValidateBudget(0))
	assert.NoError(t, ValidateBudget(1000))
}

func TestBudget_NegativeSizeCannotBypassTruncation(t *testing.T) {
	// A negative size must not offset real bytes: the real 60000-byte file
	// exceeds the 50000 budget, so the pass must truncate, never silently
	// keep an over-budget payload.
	in := []FileEntry{
		{Path: "big", Size: 60000},
		{Path: "neg", Size: -59000},
	}
	_, tr := ApplyByteBudget(in, 50000)
	assert.True(t, tr.Truncated)
	assert.NotEmpty(t, tr.FilesDropped)
}

func TestBudget_OverflowCannotBypassTruncation(t *testing.T) {
	// Summing pathological sizes must saturate, not wrap negative — a wrapped
	// total would compare <= budget and skip truncation entirely.
	in := []FileEntry{
		{Path: "huge1", Size: math.MaxInt64},
		{Path: "huge2", Size: math.MaxInt64},
		{Path: "tiny", Size: 100},
	}
	_, tr := ApplyByteBudget(in, 1000)
	assert.True(t, tr.Truncated)
	assert.NotEmpty(t, tr.FilesDropped)
}

func TestBudget_KeepsMostFiles_DropsLargestFirst(t *testing.T) {
	// The TD-flagged case: sizes [1,2,3,4,90], budget 90. Keep-most-files
	// policy drops the single 90-byte file (generated/lockfile shaped) and
	// keeps the four small source files — not the inverse.
	in := entries("e", 90, "a", 1, "b", 2, "c", 3, "d", 4)
	kept, tr := ApplyByteBudget(in, 90)
	assert.True(t, tr.Truncated)
	assert.Equal(t, []string{"e"}, tr.FilesDropped)
	assert.ElementsMatch(t, []string{"a", "b", "c", "d"}, keptPaths(kept))
}
