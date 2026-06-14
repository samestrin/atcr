package payload

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readManifestJSON(t *testing.T, m *Manifest) map[string]json.RawMessage {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.json")
	require.NoError(t, WriteManifest(path, m))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	return raw
}

// AC 05-04 Scenario 1: the review stage lists tool-enabled agents and the legacy
// stages array is unchanged (backward compatibility).
func TestManifest_ReviewStageSerialized(t *testing.T) {
	m := &Manifest{
		Base: "a", Head: "b",
		Review: &ReviewStage{
			Agents:        []string{"agent-a", "agent-b"},
			ToolsEnabled:  []string{"agent-a", "agent-b"},
			ToolsDegraded: []string{"agent-b"},
		},
	}
	raw := readManifestJSON(t, m)

	require.Contains(t, raw, "review")
	var rs ReviewStage
	require.NoError(t, json.Unmarshal(raw["review"], &rs))
	assert.Equal(t, []string{"agent-a", "agent-b"}, rs.ToolsEnabled)
	assert.Equal(t, []string{"agent-b"}, rs.ToolsDegraded)

	// 1.x stages array still present and unchanged (defaulted to ["review"]).
	require.Contains(t, raw, "stages")
	var stages []string
	require.NoError(t, json.Unmarshal(raw["stages"], &stages))
	assert.Equal(t, []string{"review"}, stages)
}

// AC 05-04 Scenario 5: no tool-enabled agents → review entry absent (omitempty);
// the manifest is still valid and 1.x stages unchanged.
func TestManifest_ReviewStageOmittedWhenNil(t *testing.T) {
	m := &Manifest{Base: "a", Head: "b"} // Review nil
	raw := readManifestJSON(t, m)
	assert.NotContains(t, raw, "review", "review must be omitted for a pure 1.x roster")
	require.Contains(t, raw, "stages")
}

// reviewBlock writes m, then unmarshals the review object into a key→raw map so a
// test can assert both the value AND the presence/absence of a field (e.g.
// snapshot_worktree_path must be present as "" in live mode, not omitted).
func reviewBlock(t *testing.T, m *Manifest) map[string]json.RawMessage {
	t.Helper()
	raw := readManifestJSON(t, m)
	require.Contains(t, raw, "review")
	var review map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["review"], &review))
	return review
}

// AC 03-02 Scenario 5 + AC 03-03 Scenario 4: a worktree-mode snapshot records
// snapshot_mode "worktree", the resolved head_sha, and the worktree path in the
// review stage.
func TestManifest_ReviewStage_SnapshotWorktreeMode(t *testing.T) {
	m := &Manifest{
		Base: "a", Head: "abc1234",
		Review: &ReviewStage{
			Agents:               []string{"agent-a"},
			ToolsEnabled:         []string{"agent-a"},
			SnapshotMode:         "worktree",
			HeadSHA:              "abc1234",
			SnapshotWorktreePath: "/tmp/atcr-snapshot-abc1234",
		},
	}
	review := reviewBlock(t, m)

	var mode, headSHA, wtPath string
	require.NoError(t, json.Unmarshal(review["snapshot_mode"], &mode))
	require.NoError(t, json.Unmarshal(review["head_sha"], &headSHA))
	require.NoError(t, json.Unmarshal(review["snapshot_worktree_path"], &wtPath))
	assert.Equal(t, "worktree", mode)
	assert.Equal(t, "abc1234", headSHA)
	assert.Equal(t, "/tmp/atcr-snapshot-abc1234", wtPath)
}

// AC 03-03 Scenario 5 (and AC 03-02 Scenario 5, live branch): a fast-path snapshot
// records snapshot_mode "live" and snapshot_worktree_path as the explicit empty
// string (present, not omitted) so a reader can distinguish live from missing.
func TestManifest_ReviewStage_SnapshotLiveMode(t *testing.T) {
	m := &Manifest{
		Base: "a", Head: "abc1234",
		Review: &ReviewStage{
			Agents:               []string{"agent-a"},
			ToolsEnabled:         []string{"agent-a"},
			SnapshotMode:         "live",
			HeadSHA:              "abc1234",
			SnapshotWorktreePath: "",
		},
	}
	review := reviewBlock(t, m)

	require.Contains(t, review, "snapshot_worktree_path",
		"live mode must serialize snapshot_worktree_path as \"\", not omit it")
	var mode, wtPath string
	require.NoError(t, json.Unmarshal(review["snapshot_mode"], &mode))
	require.NoError(t, json.Unmarshal(review["snapshot_worktree_path"], &wtPath))
	assert.Equal(t, "live", mode)
	assert.Equal(t, "", wtPath)
}
