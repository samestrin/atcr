package fanout

import (
	"reflect"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/require"
)

// perFileModes must record only the files that escalated ABOVE their payload's
// configured mode, and only files that survived the byte budget. A review where nothing escalated returns nil so the manifest
// omits per_file_payload entirely and stays byte-identical to earlier versions'.
func TestPerFileModes_OnlyRecordsEscalatedFiles(t *testing.T) {
	payloads := map[string]modePayload{
		"diff": {Kept: []payload.FileEntry{
			{Path: "a.go", Mode: payload.ModeDiff},
			{Path: "b.go", Mode: payload.ModeBlocks},
			{Path: "c.go", Mode: payload.ModeFiles},
		}},
	}

	require.Equal(t, map[string]string{"b.go": "blocks", "c.go": "files"}, perFileModes(payloads))
}

func TestPerFileModes_NothingEscalatedYieldsNil(t *testing.T) {
	payloads := map[string]modePayload{
		"blocks": {Kept: []payload.FileEntry{
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
		"files": {Kept: []payload.FileEntry{{Path: "a.go"}, {Path: "b.go"}}},
	}

	require.Nil(t, perFileModes(payloads))
}

// A roster mixing payload modes builds one payload per mode, so the same file
// can escalate differently in each. The recorded value must be the most-context
// mode any reviewer saw, and must not depend on map iteration order.
func TestPerFileModes_MultiModeFoldsToHighestContextDeterministically(t *testing.T) {
	payloads := map[string]modePayload{
		"diff":   {Kept: []payload.FileEntry{{Path: "a.go", Mode: payload.ModeBlocks}}},
		"blocks": {Kept: []payload.FileEntry{{Path: "a.go", Mode: payload.ModeFiles}}},
	}

	// Run repeatedly: Go randomizes map iteration order, so a fold that depended
	// on it would flake here rather than in production.
	for i := 0; i < 50; i++ {
		require.Equal(t, map[string]string{"a.go": "files"}, perFileModes(payloads))
	}
}

// A file the byte budget dropped never reached a reviewer, so it must not
// appear in per_file_payload — the field documents what a reviewer SAW.
func TestPerFileModes_BudgetDroppedFilesAreNotRecorded(t *testing.T) {
	payloads := map[string]modePayload{
		"diff": {
			// Pre-budget: both escalated.
			Entries: []payload.FileEntry{
				{Path: "kept.go", Mode: payload.ModeFiles},
				{Path: "dropped.go", Mode: payload.ModeFiles},
			},
			// Post-budget: only one survived.
			Kept: []payload.FileEntry{{Path: "kept.go", Mode: payload.ModeFiles}},
		},
	}

	require.Equal(t, map[string]string{"kept.go": "files"}, perFileModes(payloads),
		"a file dropped by the byte budget was never seen and must not be recorded")
}

// TestPayloadEscalationMirrorsPayloadOverrides pins
// registry.PayloadEscalationConfig to payload.EscalationOverrides field for
// field.
//
// The two types are duplicated on purpose: registry imports nothing under
// internal/, so it cannot reference payload's type, and payload must not import
// registry (see internal/payload/sprintplan.go) — the same boundary that keeps
// validPayloadModes hand-synced in registry/payload.go. buildPayloads copies
// between them by hand, so a field added to one and not the other would silently
// drop an operator's setting.
//
// It lives in fanout rather than registry deliberately: fanout is the package
// that performs the copy AND the only one whose import allowlist already covers
// both. Asserting it from inside registry would add a registry -> payload edge
// that internal/boundaries_test.go cannot see, because its dependency walk skips
// _test.go files.
func TestPayloadEscalationMirrorsPayloadOverrides(t *testing.T) {
	reg := reflect.TypeOf(registry.PayloadEscalationConfig{})
	pay := reflect.TypeOf(payload.EscalationOverrides{})

	require.Equal(t, pay.NumField(), reg.NumField(),
		"registry.PayloadEscalationConfig and payload.EscalationOverrides must have the same fields")

	for i := 0; i < reg.NumField(); i++ {
		rf := reg.Field(i)
		pf, ok := pay.FieldByName(rf.Name)
		require.Truef(t, ok, "payload.EscalationOverrides is missing field %s", rf.Name)
		require.Equalf(t, pf.Type, rf.Type, "field %s type drifted between the two structs", rf.Name)
	}
}
