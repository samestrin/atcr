package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/require"
)

// perFileModes must record only the files that escalated ABOVE their payload's
// configured mode. A review where nothing escalated returns nil so the manifest
// omits per_file_payload entirely and stays byte-identical to earlier versions'.
func TestPerFileModes_OnlyRecordsEscalatedFiles(t *testing.T) {
	payloads := map[string]modePayload{
		"diff": {Entries: []payload.FileEntry{
			{Path: "a.go", Mode: payload.ModeDiff},
			{Path: "b.go", Mode: payload.ModeBlocks},
			{Path: "c.go", Mode: payload.ModeFiles},
		}},
	}

	require.Equal(t, map[string]string{"b.go": "blocks", "c.go": "files"}, perFileModes(payloads))
}

func TestPerFileModes_NothingEscalatedYieldsNil(t *testing.T) {
	payloads := map[string]modePayload{
		"blocks": {Entries: []payload.FileEntry{
			{Path: "a.go", Mode: payload.ModeBlocks},
			{Path: "b.go", Mode: payload.ModeBlocks},
		}},
	}

	require.Nil(t, perFileModes(payloads),
		"a run where nothing escalated must omit the manifest field entirely")
}

func TestPerFileModes_EmptyModeIsIgnored(t *testing.T) {
	// Entries built outside the changed-file path (baseline/full-repo scans)
	// carry no Mode and must not appear.
	payloads := map[string]modePayload{
		"files": {Entries: []payload.FileEntry{{Path: "a.go"}, {Path: "b.go"}}},
	}

	require.Nil(t, perFileModes(payloads))
}

// A roster mixing payload modes builds one payload per mode, so the same file
// can escalate differently in each. The recorded value must be the most-context
// mode any reviewer saw, and must not depend on map iteration order.
func TestPerFileModes_MultiModeFoldsToHighestContextDeterministically(t *testing.T) {
	payloads := map[string]modePayload{
		"diff":   {Entries: []payload.FileEntry{{Path: "a.go", Mode: payload.ModeBlocks}}},
		"blocks": {Entries: []payload.FileEntry{{Path: "a.go", Mode: payload.ModeFiles}}},
	}

	// Run repeatedly: Go randomizes map iteration order, so a fold that depended
	// on it would flake here rather than in production.
	for i := 0; i < 50; i++ {
		require.Equal(t, map[string]string{"a.go": "files"}, perFileModes(payloads))
	}
}
