package localdebt

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The generator is a measurement fixture (epic 35.21 AC7): its value is that two
// runs, on two machines or two commits, build the SAME store. So the contract
// pinned here is determinism, uniqueness (no id folds into another, or the
// "50k-record" store would render fewer rows than it claims) and the shape the
// 35.13 measurement table was taken over.
func TestSyntheticRecord_IsDeterministicAndValid(t *testing.T) {
	for _, i := range []uint{0, 1, 99, 100, 49_999} {
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
	for i := uint(0); i < n; i++ {
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
	for i := uint(0); i < n; i++ {
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

// A negative index is unrepresentable: the parameter is unsigned, so a negative
// constant does not compile and a negative variable needs an explicit conversion.
// Whatever index arrives, the record is well-formed — no index-out-of-range panic
// on the severity table and no "2026--3-..." timestamp, as an int index produced.
func TestSyntheticRecord_RejectsNegativeIndexAndIsTotal(t *testing.T) {
	require.Equal(t, reflect.Uint, reflect.TypeOf(SyntheticRecord).In(0).Kind(),
		"the index type must rule out i < 0")
	for _, i := range []uint{0, 3, 4, 59, math.MaxUint32, math.MaxUint - 3, math.MaxUint} {
		r := SyntheticRecord(i)
		_, err := time.Parse(time.RFC3339, r.Timestamp)
		require.NoError(t, err, "record %d timestamp %q", i, r.Timestamp)
		_, err = monthFromRunID(r.RunID)
		require.NoError(t, err, "record %d run id %q", i, r.RunID)
		require.Positive(t, r.Line)
		require.Contains(t, syntheticSeverities, r.Severity)
	}
}

func TestSyntheticRecord_ReviewersAreNotShared(t *testing.T) {
	a := SyntheticRecord(0)
	a.Reviewers[0] = "mutated"
	require.Equal(t, "security-auditor", SyntheticRecord(1).Reviewers[0],
		"mutating one record's reviewers must not reach any other record")
	require.Equal(t, "security-auditor", syntheticReviewers[0])
}

func TestGenerateSynthetic_RejectsCountAboveCeiling(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "debt")
	err := GenerateSynthetic(dir, MaxSyntheticRecords+1)
	require.ErrorContains(t, err, fmt.Sprint(MaxSyntheticRecords))
	_, statErr := os.Stat(dir)
	require.True(t, os.IsNotExist(statErr), "a rejected count writes nothing")
}

// A re-run over the same store would silently double the corpus (every id
// appended twice), so the generator refuses a store that already holds shards.
func TestGenerateSynthetic_RefusesAStoreThatHoldsRecords(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "debt")
	require.NoError(t, GenerateSynthetic(dir, 3))
	err := GenerateSynthetic(dir, 3)
	require.ErrorContains(t, err, "already holds records")
	recs, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 3, "the refused re-run appended nothing")
}

// An existing directory with no shards in it is an empty store, not a refusal.
func TestGenerateSynthetic_AcceptsAnExistingEmptyStore(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, GenerateSynthetic(dir, 2))
	recs, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 2)
}
