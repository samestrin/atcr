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

// TestManifest_NilStagesNormalizedToReview verifies that WriteManifest with nil
// Stages produces "stages":["review"] so readers never see an absent field and
// cannot confuse 1.x manifests with pre-field older ones.
func TestManifest_NilStagesNormalizedToReview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := &Manifest{Base: "aaa", Head: "bbb", StartedAt: time.Now().UTC()}
	// Stages intentionally nil — WriteManifest must default to ["review"]
	require.NoError(t, WriteManifest(path, m))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"stages"`, "nil Stages must be normalized to [\"review\"] on write")
	assert.Contains(t, string(data), `"review"`, "nil Stages must produce [\"review\"] not absent field")
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

// TestManifest_MaxParallelZeroSerializes verifies that an explicitly-unbounded
// run (MaxParallel=0) is serialized with the max_parallel key present in the
// JSON, so it is distinguishable from an older manifest that never carried the
// field at all.
func TestManifest_MaxParallelZeroSerializes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := &Manifest{
		Base:        "aaa",
		Head:        "bbb",
		StartedAt:   time.Now().UTC(),
		MaxParallel: 0, // explicit unbounded
	}
	require.NoError(t, WriteManifest(path, m))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"max_parallel"`, "MaxParallel=0 must appear in JSON to distinguish explicit-unbounded from absent")
}

// --- Epic 1.5: timeout_secs backward compatibility (stale-inference input) ---

// TestManifest_TimeoutSecsTolerantWhenAbsent verifies a manifest written before
// the timeout_secs field existed parses cleanly with TimeoutSecs == 0. The
// status reader relies on this to disable stale inference for old manifests
// (zero value = unknown deadline) rather than failing to load them.
func TestManifest_TimeoutSecsTolerantWhenAbsent(t *testing.T) {
	var got Manifest
	require.NoError(t, json.Unmarshal(
		[]byte(`{"base":"a","head":"b","roster":["greta"],"started_at":"2020-01-01T00:00:00Z","partial":false}`), &got))
	assert.Zero(t, got.TimeoutSecs, "absent timeout_secs must parse to zero, not error")
}

// TestManifest_TimeoutSecsOmittedWhenZero verifies omitempty keeps a zero
// timeout_secs out of the JSON, so an old-style manifest round-trips clean and
// is indistinguishable from one never carrying the field.
func TestManifest_TimeoutSecsOmittedWhenZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	require.NoError(t, WriteManifest(path, &Manifest{Base: "a", Head: "b", StartedAt: time.Now().UTC()}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "timeout_secs", "zero timeout_secs must be omitted via omitempty")
}
