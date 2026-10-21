package localdebt

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The generator is a measurement fixture (epic 35.21 AC7): its value is that two
// runs, on two machines or two commits, build the SAME store. So the contract
// pinned here is determinism, uniqueness (no id folds into another, or the
// "50k-record" store would render fewer rows than it claims) and the shape the
// 35.13 measurement table was taken over.
func TestSyntheticRecord_IsDeterministicAndValid(t *testing.T) {
	for _, i := range []int{0, 1, 99, 100, 49_999} {
		a, b := SyntheticRecord(i), SyntheticRecord(i)
		require.Equal(t, a, b, "record %d must not depend on anything but its index", i)
		require.Equal(t, SchemaVersion, a.SchemaVersion)
		require.NotEmpty(t, a.ID)
		want := a
		want.StampID()
		require.Equal(t, want.ID, a.ID, "the id is the content hash Append's readers expect")
		require.True(t, IsKnownStatus(a.Status))
		_, err := monthFromRunID(a.RunID)
		require.NoError(t, err)
	}
}

func TestSyntheticRecord_Distribution(t *testing.T) {
	const n = 4000
	ids := map[string]bool{}
	sev := map[string]int{}
	comp := map[string]int{}
	for i := 0; i < n; i++ {
		r := SyntheticRecord(i)
		require.False(t, ids[r.ID], "record %d collides with an earlier id", i)
		ids[r.ID] = true
		require.Empty(t, r.Status, "every synthetic record is open, as in the 35.13 corpus")
		sev[r.Severity]++
		comp[filepath.Dir(r.File)]++
	}
	// A quarter CRITICAL and one component per hundred rows: the selectivities
	// the 35.13 table's `--severity CRITICAL` (~25%) and `--component
	// internal/pkg001` (~1%) rows were measured at.
	require.Equal(t, n/4, sev["CRITICAL"])
	require.Len(t, comp, syntheticComponents)
	require.Equal(t, n/syntheticComponents, comp["internal/pkg001"])
}

func TestGenerateSynthetic_WritesNRecordsThroughAppend(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "debt")
	const n = 250
	require.NoError(t, GenerateSynthetic(dir, n))

	recs, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, n)
	require.Len(t, FoldRecords(recs), n, "no two synthetic records fold together")
	byID := map[string]Record{}
	for _, r := range recs {
		byID[r.ID] = r
	}
	for i := 0; i < n; i++ {
		want := SyntheticRecord(i)
		require.Equal(t, want, byID[want.ID], "record %d round-trips through the store unchanged", i)
	}

	// Sized like the corpus the 35.13 table was measured on (47 MB / 50k, about
	// 940 bytes a record), so numbers taken over it stay comparable.
	var size int64
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			info, err := e.Info()
			require.NoError(t, err)
			size += info.Size()
		}
	}
	perRecord := size / n
	require.GreaterOrEqual(t, perRecord, int64(850), "bytes per record")
	require.LessOrEqual(t, perRecord, int64(1050), "bytes per record")
}

func TestGenerateSynthetic_RejectsNegativeCount(t *testing.T) {
	err := GenerateSynthetic(t.TempDir(), -1)
	require.ErrorContains(t, err, "-1")
}

func TestGenerateSynthetic_SurfacesAppendFailure(t *testing.T) {
	// A regular file where the store directory should be: the first Append fails,
	// and the error names the record it was writing.
	dir := filepath.Join(t.TempDir(), "debt")
	require.NoError(t, os.WriteFile(dir, nil, 0o600))
	err := GenerateSynthetic(dir, 3)
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf("synthetic record 0 (%s)", SyntheticRecord(0).ID))
}
