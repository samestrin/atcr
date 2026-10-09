package fanout

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestStatusFor_SalvagedOnStopChunksNameOnlyTheStopReasonBins pins the on-disk
// split: salvaged_chunks keeps naming every salvaged bin, and
// salvaged_on_stop_chunks names the subset that salvaged on a stop reason, so an
// operator can tell a kept stop-reason bin from a refused truncated one.
func TestStatusFor_SalvagedOnStopChunksNameOnlyTheStopReasonBins(t *testing.T) {
	merged := mergeResultGroup([]Result{
		{Agent: "bruce", Status: StatusOK, Content: "MEDIUM|b.go:2|clean|f|correctness|2|e"},
		{Agent: "bruce", Status: StatusOK, Content: "MEDIUM|b.go:3|stop|f|correctness|2|e",
			Salvaged: true, SalvagedOnStop: true},
		{Agent: "bruce", Status: StatusOK, Content: "chain of thought only",
			Salvaged: true, ResponseTruncated: true},
	}, nil)

	for name, st := range map[string]AgentStatus{
		"statusFor":    statusFor(merged, findingsResult{}),
		"resultStatus": resultStatus(merged),
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, []int{1, 2}, st.SalvagedChunks, "every salvaged bin, as before")
			assert.Equal(t, []int{1}, st.SalvagedOnStopChunks, "only the stop-reason bin")
			assert.False(t, st.SalvagedOnStop, "the bit is unchunked-only")
		})
	}

	raw, err := json.Marshal(statusFor(merged, findingsResult{}))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"salvaged_chunks":[1,2]`)
	assert.Contains(t, string(raw), `"salvaged_on_stop_chunks":[1]`)
	assert.NotContains(t, string(raw), `"salvaged_on_stop":`)
}

// TestStatusFor_SalvagedOnStopBitIsUnchunkedOnly pins the unchunked bit, and that
// a merged Result never publishes it even though the merge copies bin 0's
// SalvagedOnStop onto the persona record.
func TestStatusFor_SalvagedOnStopBitIsUnchunkedOnly(t *testing.T) {
	stop := Result{Agent: "bruce", Status: StatusOK, Content: "x", Salvaged: true, SalvagedOnStop: true}
	for name, st := range map[string]AgentStatus{
		"statusFor":    statusFor(stop, findingsResult{}),
		"resultStatus": resultStatus(stop),
	} {
		t.Run(name, func(t *testing.T) {
			assert.True(t, st.SalvagedOnStop)
			assert.Nil(t, st.SalvagedOnStopChunks)
		})
	}
	raw, err := json.Marshal(statusFor(stop, findingsResult{}))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"salvaged_on_stop":true`)

	truncated := Result{Agent: "bruce", Status: StatusOK, Content: "x", Salvaged: true, ResponseTruncated: true}
	assert.False(t, statusFor(truncated, findingsResult{}).SalvagedOnStop)

	// A stray on-stop flag without a salvage is not a salvage reason.
	stray := Result{Agent: "bruce", Status: StatusOK, Content: "x", SalvagedOnStop: true}
	assert.False(t, statusFor(stray, findingsResult{}).SalvagedOnStop)

	merged := mergeResultGroup([]Result{stop, {Agent: "bruce", Status: StatusOK, Content: "y"}}, nil)
	assert.True(t, merged.SalvagedOnStop, "precondition: the merge inherits bin 0's bit")
	assert.False(t, statusFor(merged, findingsResult{}).SalvagedOnStop)
	assert.Equal(t, []int{0}, statusFor(merged, findingsResult{}).SalvagedOnStopChunks)
}

// TestSalvagedOnStop_MisalignedPublishesNothing mirrors the salvagedChunkIndices
// guard: a wrong index is worse than none.
func TestSalvagedOnStop_MisalignedPublishesNothing(t *testing.T) {
	tests := []struct {
		name     string
		salvaged []bool
		onStop   []bool
	}{
		{"on-stop flags shorter", []bool{true, true}, []bool{true}},
		{"on-stop flags absent", []bool{true, true}, nil},
		{"salvage flags misaligned", []bool{true}, []bool{true, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bit, got := salvagedOnStop(Result{
				chunkContents: []string{"a", "b"}, chunkSalvaged: tt.salvaged, chunkSalvagedOnStop: tt.onStop,
			})
			assert.False(t, bit)
			assert.Nil(t, got)
		})
	}
	bit, got := salvagedOnStop(Result{
		chunkContents:       []string{"a", "b"},
		chunkSalvaged:       []bool{false, true},
		chunkSalvagedOnStop: []bool{true, true},
	})
	assert.False(t, bit)
	assert.Equal(t, []int{1}, got, "an on-stop flag on an unsalvaged bin is not named")
}

// TestAgentStatus_LegacyRecordWithoutOnStopKeys reads a status.json written
// before the keys existed: both are zero-valued, and re-marshalling a record that
// never set them adds neither key, so salvaged_chunks output is unchanged.
func TestAgentStatus_LegacyRecordWithoutOnStopKeys(t *testing.T) {
	legacy := `{"agent":"bruce","status":"ok","salvaged":true,"salvaged_chunks":[1,3]}`
	var st AgentStatus
	require.NoError(t, json.Unmarshal([]byte(legacy), &st))
	assert.False(t, st.SalvagedOnStop)
	assert.Nil(t, st.SalvagedOnStopChunks)
	assert.Equal(t, []int{1, 3}, st.SalvagedChunks)

	raw, err := json.Marshal(st)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "salvaged_on_stop")
	assert.Contains(t, string(raw), `"salvaged_chunks":[1,3]`)
}
