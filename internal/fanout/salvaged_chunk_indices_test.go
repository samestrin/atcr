package fanout

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSalvagedChunkIndices_MisalignedPairPublishesNothing pins the fail-closed
// guard that survived mutation: deleting the length check left the whole suite
// green.
//
// chunkSalvaged and chunkContents are written as a pair by mergeResultGroup, so
// they agree for every Result the chunked path produces. If they ever do not,
// there is no way to say WHICH bin salvaged — and a wrong index in
// salvaged_chunks is worse than none, because an operator reads it as the
// identity of the bin that was refused. The guard is the only thing that keeps
// the misaligned case from publishing one.
func TestSalvagedChunkIndices_MisalignedPairPublishesNothing(t *testing.T) {
	tests := []struct {
		name     string
		contents []string
		salvaged []bool
	}{
		{"flags shorter than bins", []string{"a", "b", "c"}, []bool{false, true}},
		{"flags longer than bins", []string{"a"}, []bool{false, true, true}},
		{"flags absent entirely", []string{"a", "b"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := salvagedChunkIndices(Result{chunkContents: tt.contents, chunkSalvaged: tt.salvaged})
			assert.Nil(t, got,
				"a misaligned pair names no bin — publishing an index here would name the wrong one")
		})
	}
}

// TestSalvagedChunkIndices_AlignedPairNamesTheRefusedBins is the complement: on
// the aligned pair the guard must NOT fire, or the disclosure the sprint added
// would be silently empty for every chunked persona.
func TestSalvagedChunkIndices_AlignedPairNamesTheRefusedBins(t *testing.T) {
	got := salvagedChunkIndices(Result{
		chunkContents: []string{"a", "b", "c"},
		chunkSalvaged: []bool{false, true, true},
	})
	assert.Equal(t, []int{1, 2}, got, "the refused bins are named by index, in order")

	none := salvagedChunkIndices(Result{
		chunkContents: []string{"a", "b"},
		chunkSalvaged: []bool{false, false},
	})
	assert.Nil(t, none, "no bin salvaged — nothing to publish")
}
