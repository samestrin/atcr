package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadCheckpoint returns (nil, nil) when the file does not exist — the
// first-run-of-a-fresh-checkpoint case, distinguished from a real read error so
// the run starts cleanly rather than aborting.
func TestLoadCheckpoint_MissingReturnsNil(t *testing.T) {
	cp, err := loadCheckpoint(filepath.Join(t.TempDir(), "does-not-exist.json"))
	require.NoError(t, err)
	assert.Nil(t, cp, "a missing checkpoint is not an error; it means start fresh")
}

// saveCheckpoint then loadCheckpoint must round-trip the full record, including
// per-reviewer usage-gated cost/latency, so a resumed run can fold the exact same
// values back into the accumulator.
func TestCheckpoint_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "ckpt.json")
	want := &runCheckpoint{
		ReproHash:    "abc123",
		Suite:        "fixture-mini",
		SuiteVersion: "1.0.0",
		Cases: []checkpointCase{
			{
				Index:  0,
				CaseID: "case-01",
				Reviewers: []checkpointReviewer{
					{Agent: "greta", Model: "m-greta", Persona: "greta",
						Expected: []string{"correctness"}, Raised: []string{"correctness"},
						UsageReported: true, CostUSD: 0.0125, LatencyMS: 1200},
				},
			},
		},
	}

	require.NoError(t, saveCheckpoint(path, want))

	got, err := loadCheckpoint(path)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, got, "checkpoint must round-trip byte-for-byte through save/load")
}

// doneIndex maps each completed case's index to its entry so the run loop can
// skip-and-replay it in O(1).
func TestRunCheckpoint_DoneIndex(t *testing.T) {
	cp := &runCheckpoint{Cases: []checkpointCase{
		{Index: 0, CaseID: "a"},
		{Index: 2, CaseID: "c"},
	}}
	done := cp.doneIndex()
	require.Len(t, done, 2)
	assert.Equal(t, "a", done[0].CaseID)
	assert.Equal(t, "c", done[2].CaseID)
	_, ok := done[1]
	assert.False(t, ok, "index 1 was never checkpointed")
}

// validateCheckpoint accepts a matching suite identity and rejects any drift in
// repro_hash, suite, or suite_version with the ErrCheckpointSuiteMismatch sentinel
// (AC4 — never silently mixed).
func TestValidateCheckpoint(t *testing.T) {
	cp := &runCheckpoint{ReproHash: "hash-1", Suite: "fixture-mini", SuiteVersion: "1.0.0"}

	require.NoError(t, validateCheckpoint(cp, "hash-1", "fixture-mini", "1.0.0"),
		"a matching suite identity resumes")

	for _, tc := range []struct {
		name                      string
		hash, suite, suiteVersion string
	}{
		{"changed repro hash", "hash-2", "fixture-mini", "1.0.0"},
		{"changed suite", "hash-1", "other-suite", "1.0.0"},
		{"changed suite version", "hash-1", "fixture-mini", "2.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCheckpoint(cp, tc.hash, tc.suite, tc.suiteVersion)
			require.Error(t, err)
			assert.True(t, errors.Is(err, errCheckpointSuiteMismatch),
				"mismatch must surface the fail-closed sentinel")
		})
	}
}

// A present-but-corrupt checkpoint surfaces a parse error rather than a guessed
// empty state (mirrors fanout ReadManifest's fail-loud-on-corruption contract).
func TestLoadCheckpoint_CorruptErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ckpt.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))
	_, err := loadCheckpoint(path)
	require.Error(t, err)
}

// saveCheckpoint must encode the checkpoint as compact JSON so the on-disk file
// is small and fast to rewrite on every completed case.
func TestSaveCheckpoint_WritesCompactJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ckpt.json")
	cp := &runCheckpoint{
		ReproHash:    "abc123",
		Suite:        "fixture-mini",
		SuiteVersion: "1.0.0",
		Cases: []checkpointCase{
			{
				Index:  0,
				CaseID: "case-01",
				Reviewers: []checkpointReviewer{
					{Agent: "greta", Model: "m-greta", Persona: "greta"},
				},
			},
		},
	}
	require.NoError(t, saveCheckpoint(path, cp))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "\n", "checkpoint JSON must be compact, not indented")
}

// saveCheckpoint must leave the previous valid checkpoint intact when the next
// write fails (e.g., disk full / permissions). The temp-file + rename design
// guarantees the on-disk file always reflects a whole number of cases (AC1).
func TestSaveCheckpoint_PreservesPriorOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpt.json")
	first := &runCheckpoint{
		ReproHash:    "abc123",
		Suite:        "fixture-mini",
		SuiteVersion: "1.0.0",
		Cases: []checkpointCase{
			{Index: 0, CaseID: "case-01"},
		},
	}
	require.NoError(t, saveCheckpoint(path, first))

	// Make the directory unwritable so the second save cannot create its temp file.
	require.NoError(t, os.Chmod(dir, 0o000))
	defer func() { require.NoError(t, os.Chmod(dir, 0o700)) }()

	second := &runCheckpoint{
		ReproHash:    "abc123",
		Suite:        "fixture-mini",
		SuiteVersion: "1.0.0",
		Cases: []checkpointCase{
			{Index: 0, CaseID: "case-01"},
			{Index: 1, CaseID: "case-02"},
		},
	}
	err := saveCheckpoint(path, second)
	require.Error(t, err, "second save must fail when the output directory is unwritable")

	// After restoring permissions, the prior checkpoint must still parse and hold
	// exactly the first case.
	require.NoError(t, os.Chmod(dir, 0o700))
	cp, err := loadCheckpoint(path)
	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Len(t, cp.Cases, 1, "prior valid checkpoint must survive a failed overwrite")
	assert.Equal(t, "case-01", cp.Cases[0].CaseID)
}
