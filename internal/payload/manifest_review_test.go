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
