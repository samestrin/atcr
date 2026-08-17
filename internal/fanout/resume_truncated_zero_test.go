package fanout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RebuildPool reconstructs the pool on the resume path, and it reconstructs it from the
// same AgentStatus records writePool derives its run-level tallies from — but it never
// recomputed PoolSummary.TruncatedZeroFindings and never emitted the truncated-to-zero
// warning. A resumed review therefore loses both the tally (pre-existing) and the console
// signal (new with the warning), which is the exact gap the warning was added to close:
// the tally alone had reached no operator surface for four epics.
//
// Reviewers that contributed nothing do not become harmless because the review was
// resumed — if anything a resume is where an operator is least likely to re-read the
// original run's output.
func TestRebuildPool_RecoversTruncatedZeroFindings(t *testing.T) {
	poolDir := filepath.Join(t.TempDir(), "sources", "pool")
	require.NoError(t, writeResumedAgents(poolDir, []Result{
		// greta and kai: HTTP 200, response truncated, zero parsed findings.
		{Agent: "greta", Status: StatusOK, ResponseTruncated: true, Content: ""},
		{Agent: "kai", Status: StatusOK, ResponseTruncated: true, Content: ""},
		// bruce landed a finding and is not a runaway.
		{Agent: "bruce", Status: StatusOK, Content: "HIGH|x.go:1|p|f|correctness|5|e"},
	}, nil))

	var err error
	out := captureStderr(t, func() {
		_, _, err = RebuildPool(poolDir, []string{"greta", "kai", "bruce"})
	})
	require.NoError(t, err)

	data, rerr := os.ReadFile(filepath.Join(poolDir, summaryFile))
	require.NoError(t, rerr)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(data, &ps))

	assert.Equal(t, 2, ps.TruncatedZeroFindings,
		"the resumed summary must carry the same runaway tally the original run recorded")

	assert.Contains(t, out, "truncated",
		"a resumed review must not lose the console signal — the tally alone reaches no operator")
	assert.Contains(t, out, "greta", "the warning must name WHICH agents contributed nothing")
	assert.Contains(t, out, "kai")
	assert.NotContains(t, out, "bruce", "an agent that landed findings is not a runaway")
}

// RebuildPool tallies over the union of ALL on-disk statuses, including agents that
// completed in the ORIGINAL run and that this resume never re-ran. That is correct for
// the tally — summary.json records the review, not the attempt — but it makes the
// console warning misattribute: worded as a fresh event it reads as "this resume
// produced reviewers that contributed nothing", and because those agents stay StatusOK
// their status.json is never rewritten, so every subsequent resume re-prints the same
// names as if it were news.
//
// The signal must NOT be scoped away (an operator resuming a review is exactly who needs
// to know the original run had runaways), so it is marked as a restatement instead.
func TestRebuildPool_WarningIsMarkedAsARestatementNotAFreshEvent(t *testing.T) {
	poolDir := filepath.Join(t.TempDir(), "sources", "pool")
	require.NoError(t, writeResumedAgents(poolDir, []Result{
		{Agent: "greta", Status: StatusOK, ResponseTruncated: true, Content: ""},
	}, nil))

	out := captureStderr(t, func() {
		_, _, err := RebuildPool(poolDir, []string{"greta"})
		require.NoError(t, err)
	})

	require.Contains(t, out, "greta", "precondition: the warning fired and named the agent")
	assert.Contains(t, out, "across this review",
		"the rebuild restates the review's cumulative tally — it must not read as something "+
			"this resume just caused")
	assert.Contains(t, out, "including agents this resume did not re-run",
		"the operator must know the named agents may predate this attempt")
}

// The fresh path keeps its unqualified wording: there, every counted agent really did
// truncate in the run just performed.
func TestWritePool_WarningIsNotMarkedAsARestatement(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	out := captureStderr(t, func() {
		_, err := WritePool(pool, []Result{
			{Agent: "greta", Status: StatusOK, ResponseTruncated: true, Content: ""},
		}, nil)
		require.NoError(t, err)
	})

	require.Contains(t, out, "greta", "precondition: the warning fired")
	assert.NotContains(t, out, "did not re-run",
		"a fresh run has no earlier attempt to restate")
}

// The silent-at-zero rule holds on the resume path for the same reason it holds on the
// fresh path: a warning printed on every resume is one nobody reads.
func TestRebuildPool_SilentWhenNoAgentTruncatedToZero(t *testing.T) {
	poolDir := filepath.Join(t.TempDir(), "sources", "pool")
	require.NoError(t, writeResumedAgents(poolDir, []Result{
		{Agent: "bruce", Status: StatusOK, Content: "HIGH|x.go:1|p|f|correctness|5|e"},
	}, nil))

	out := captureStderr(t, func() {
		_, _, err := RebuildPool(poolDir, []string{"bruce"})
		require.NoError(t, err)
	})
	assert.NotContains(t, out, "truncated")
}
