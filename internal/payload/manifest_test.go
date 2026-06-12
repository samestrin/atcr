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
