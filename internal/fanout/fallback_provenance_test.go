package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fallback-provenance surfacing (Epic 19.10 F5, Task 06). These tests prove the
// bulk (non-chunked) path's fallback substitution survives to disk end-to-end:
// per-agent status.json carries fallback_used/fallback_from, and the run-level
// summary.json carries fallback_count. Before this task no test asserted the
// bulk path recorded a fallback at all — this closes AC5's "verified by
// test/fixture" gap for the bulk path.

// TestWritePool_RecordsFallbackProvenance is the fixture proof: a bulk-shaped
// Result slice (no chunking) with one fallback-served slot writes both the
// per-agent and run-level provenance.
func TestWritePool_RecordsFallbackProvenance(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	results := []Result{
		// fallback-served slot: primary overflowed, fallback rescued it.
		{Agent: "dax", Status: StatusOK, FallbackUsed: true, FallbackFrom: "dax", Content: "HIGH|a.go:1|b|f|correctness|5|e|dax"},
		// clean slot, no fallback.
		{Agent: "otto", Status: StatusOK, Content: "HIGH|b.go:2|b|f|correctness|5|e|otto"},
	}
	_, err := WritePool(pool, results, nil)
	require.NoError(t, err)

	// Run-level: summary.json fallback_count reflects the one substitution.
	data, err := os.ReadFile(filepath.Join(pool, "summary.json"))
	require.NoError(t, err)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(data, &ps))
	assert.Equal(t, 1, ps.FallbackCount, "one fallback-served slot is counted at the run level")

	// Per-agent: status.json serializes fallback_used/fallback_from for the served
	// slot, and leaves them off for the clean slot.
	daxSt := readAgentStatus(t, pool, "dax")
	assert.True(t, daxSt.FallbackUsed)
	assert.Equal(t, "dax", daxSt.FallbackFrom)

	ottoSt := readAgentStatus(t, pool, "otto")
	assert.False(t, ottoSt.FallbackUsed)
	assert.Empty(t, ottoSt.FallbackFrom)
}

// TestWritePool_FallbackCountAlwaysPresent locks the always-present discipline:
// a zero-fallback run's raw summary.json bytes contain the literal
// "fallback_count":0, never an omitted key (matching TruncatedZeroFindings).
func TestWritePool_FallbackCountAlwaysPresent(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	results := []Result{
		{Agent: "clean", Status: StatusOK, Content: "HIGH|b.go:2|b|f|correctness|5|e|clean"},
	}
	_, err := WritePool(pool, results, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, "summary.json"))
	require.NoError(t, err)
	// The key must be physically present (never omitempty), matching the
	// TruncatedZeroFindings discipline. Assert on the raw key token (indentation
	// puts a space after the colon) and confirm the decoded value is 0.
	assert.True(t, bytes.Contains(data, []byte(`"fallback_count"`)),
		"fallback_count key must be present on a zero-fallback run, not omitted")
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	got, ok := raw["fallback_count"]
	require.True(t, ok, "fallback_count key present in summary.json")
	assert.Equal(t, "0", string(got), "zero-fallback run records fallback_count 0")
}

// TestE2E_Fallback_RecordedInArtifacts is the strongest signal: a real Engine
// runs a primary-fails/fallback-succeeds slot (bulk, un-chunked), the results are
// piped through WritePool, and the on-disk artifacts record the substitution —
// the direct AC5 proof for the bulk path.
func TestE2E_Fallback_RecordedInArtifacts(t *testing.T) {
	f := newFake()
	f.failFor["dax"] = errors.New("context window exceeded") // primary model fails
	e := NewEngine(f)
	slots := []Slot{slotWithFallback("dax", "dax-fb")}

	results := e.Run(context.Background(), slots)
	require.Len(t, results, 1)
	require.Equal(t, StatusOK, results[0].Status, "fallback rescued the slot")
	require.True(t, results[0].FallbackUsed)
	require.Equal(t, "dax", results[0].FallbackFrom, "attribution stays with the slot's primary name")
	require.Equal(t, "dax-fb", results[0].FallbackModel, "the served fallback model is recorded as the collapse key")

	pool := filepath.Join(t.TempDir(), "pool")
	_, err := WritePool(pool, results, nil)
	require.NoError(t, err)

	// summary.json run-level tally.
	data, err := os.ReadFile(filepath.Join(pool, "summary.json"))
	require.NoError(t, err)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(data, &ps))
	assert.Equal(t, 1, ps.FallbackCount)

	// status.json per-agent provenance under the slot's (primary) name: FallbackFrom
	// is the substituted-from primary (attribution); FallbackModel is the served net
	// model (the de-weighting collapse key reconcile reads).
	st := readAgentStatus(t, pool, "dax")
	assert.Equal(t, StatusOK, st.Status)
	assert.True(t, st.FallbackUsed)
	assert.Equal(t, "dax", st.FallbackFrom, "provenance names the substituted-from slot")
	assert.Equal(t, "dax-fb", st.FallbackModel, "provenance names the served fallback model")
}
