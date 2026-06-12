package payload

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManifest_RecordsDefaultAndPerAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := &Manifest{
		Base:          "aaa",
		Head:          "bbb",
		DetectionMode: "auto",
		CommitCount:   3,
		PayloadMode:   "blocks",
		PerAgentPayload: map[string]string{
			"bruce": "diff",
			"greta": "blocks",
		},
		Roster:    []string{"bruce", "greta"},
		StartedAt: time.Now().UTC(),
	}
	require.NoError(t, WriteManifest(path, m))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var got Manifest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "blocks", got.PayloadMode)
	assert.Equal(t, "diff", got.PerAgentPayload["bruce"])
	assert.Equal(t, "blocks", got.PerAgentPayload["greta"])
	assert.Equal(t, []string{"bruce", "greta"}, got.Roster)
	assert.Equal(t, "auto", got.DetectionMode)
}

func TestWriteManifest_ArtifactFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := &Manifest{Base: "aaa", Head: "bbb", StartedAt: time.Now().UTC()}
	require.NoError(t, WriteManifest(path, m))

	info, err := os.Stat(path)
	require.NoError(t, err)
	// manifest.json must carry the same 0644 artifact mode as every other
	// review artifact (AC 01-03); os.CreateTemp would otherwise leave 0600.
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

// --- Epic 1.1: reserved manifest stages array ---

// TestManifest_StagesRoundTrip verifies the reserved stages array is written
// and read back. In 1.x a review records exactly ["review"].
func TestManifest_StagesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := &Manifest{Base: "a", Head: "b", StartedAt: time.Now().UTC(), Stages: []string{"review"}}
	require.NoError(t, WriteManifest(path, m))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"stages"`)
	var got Manifest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, []string{"review"}, got.Stages)
}

// TestManifest_StagesTolerantWhenAbsent verifies a manifest written without the
// reserved stages field parses cleanly (tolerant reader; field stays nil).
func TestManifest_StagesTolerantWhenAbsent(t *testing.T) {
	var got Manifest
	require.NoError(t, json.Unmarshal([]byte(`{"base":"a","head":"b","roster":["greta"],"partial":false}`), &got))
	assert.Nil(t, got.Stages)
}

// --- Epic 1.4: max_parallel and timeout_secs in manifest ---

// TestManifest_RecordsMaxParallelAndTimeoutSecs verifies that the effective
// fan-out settings are written to manifest.json so post-hoc diagnosis of
// throttled or timed-out runs can inspect the active cap from disk.
func TestManifest_RecordsMaxParallelAndTimeoutSecs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := &Manifest{
		Base:        "aaa",
		Head:        "bbb",
		StartedAt:   time.Now().UTC(),
		MaxParallel: 4,
		TimeoutSecs: 300,
	}
	require.NoError(t, WriteManifest(path, m))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var got Manifest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, 4, got.MaxParallel, "max_parallel must round-trip through manifest.json")
	assert.Equal(t, 300, got.TimeoutSecs, "timeout_secs must round-trip through manifest.json")
	assert.Contains(t, string(data), `"max_parallel"`, "max_parallel key must appear in JSON")
	assert.Contains(t, string(data), `"timeout_secs"`, "timeout_secs key must appear in JSON")
}
