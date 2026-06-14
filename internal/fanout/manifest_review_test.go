package fanout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC 05-04 Scenario 1 + Edge 3: tool-enabled agents are listed in the review
// stage; non-tool (1.x) agents are excluded.
func TestReviewStageFor_ListsToolEnabledOnly(t *testing.T) {
	results := []Result{
		{Agent: "agent-a", ToolsRequested: true, Tools: true},
		{Agent: "agent-b", ToolsRequested: true, Tools: true},
		{Agent: "agent-c"}, // non-tool, ToolsRequested=false
	}
	rs := reviewStageFor(results)
	require.NotNil(t, rs)
	assert.Equal(t, []string{"agent-a", "agent-b"}, rs.ToolsEnabled)
	assert.Empty(t, rs.ToolsDegraded)
	assert.NotContains(t, rs.ToolsEnabled, "agent-c")
}

// AC 05-04 Scenario 2 + Edge 4: a degraded agent is in BOTH tools_enabled and
// tools_degraded (it started with tools at invocation time).
func TestReviewStageFor_DegradedInBothLists(t *testing.T) {
	results := []Result{
		{Agent: "agent-a", ToolsRequested: true, Tools: true},
		{Agent: "agent-b", ToolsRequested: true, Tools: true, ToolsDegraded: true},
	}
	rs := reviewStageFor(results)
	require.NotNil(t, rs)
	assert.Equal(t, []string{"agent-a", "agent-b"}, rs.ToolsEnabled)
	assert.Equal(t, []string{"agent-b"}, rs.ToolsDegraded)
}

// AC 05-04 Scenario 3: a budget-tripped agent is in tools_enabled but NOT in
// tools_degraded (a budget trip is not degradation).
func TestReviewStageFor_BudgetTrippedNotDegraded(t *testing.T) {
	results := []Result{
		{Agent: "agent-b", ToolsRequested: true, Tools: true, TrippedBudgets: []string{"tool_budget_bytes"}},
	}
	rs := reviewStageFor(results)
	require.NotNil(t, rs)
	assert.Equal(t, []string{"agent-b"}, rs.ToolsEnabled)
	assert.Empty(t, rs.ToolsDegraded)
}

// AC 05-04 Scenario 4: every completion path (normal, degraded, budget-tripped,
// provider error) for a tools:true agent appears in tools_enabled; only the
// degraded one appears in tools_degraded.
func TestReviewStageFor_AllCompletionPaths(t *testing.T) {
	results := []Result{
		{Agent: "a", ToolsRequested: true, Tools: true},                                        // normal
		{Agent: "b", ToolsRequested: true, Tools: true, ToolsDegraded: true},                   // degraded
		{Agent: "c", ToolsRequested: true, Tools: true, TrippedBudgets: []string{"max_turns"}}, // tripped
		{Agent: "d", ToolsRequested: true, Tools: true, Status: StatusFailed},                  // provider error
	}
	rs := reviewStageFor(results)
	require.NotNil(t, rs)
	assert.Equal(t, []string{"a", "b", "c", "d"}, rs.ToolsEnabled)
	assert.Equal(t, []string{"b"}, rs.ToolsDegraded)
}

// AC 05-04 Scenario 5: a roster with no tool-enabled agents → nil (so the
// manifest omits the review entry).
func TestReviewStageFor_NilWhenNoToolAgents(t *testing.T) {
	results := []Result{{Agent: "a"}, {Agent: "b"}}
	assert.Nil(t, reviewStageFor(results))
}

// AC 05-04 Edge Case 1: a single tool agent roster lists that one agent.
func TestReviewStageFor_SingleAgent(t *testing.T) {
	rs := reviewStageFor([]Result{{Agent: "solo", ToolsRequested: true, Tools: true}})
	require.NotNil(t, rs)
	assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)
}

// AC 03-03 Scenario 5: a fast-path snapshot (root == repo) is recorded as live
// mode with an empty worktree path; head is the resolved head_sha verbatim.
func TestSnapshotManifestFields_LiveMode(t *testing.T) {
	mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")
	assert.Equal(t, "live", mode)
	assert.Equal(t, "abc1234", headSHA)
	assert.Equal(t, "", wt)
}

// AC 03-03 Scenario 4: a slow-path snapshot (root != repo) is recorded as
// worktree mode and carries the worktree path SnapshotFor returned.
func TestSnapshotManifestFields_WorktreeMode(t *testing.T) {
	mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")
	assert.Equal(t, "worktree", mode)
	assert.Equal(t, "abc1234", headSHA)
	assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)
}
