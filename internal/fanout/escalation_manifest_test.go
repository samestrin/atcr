package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// escalationRepo builds a repo whose base..head range changes n Go files, so a
// low-threshold payload_escalation block has something to score.
func escalationRepo(t *testing.T, changedFiles int) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	for i := 0; i < changedFiles; i++ {
		writeFileAt(t, dir, fmt.Sprintf("f%d.go", i), "package main\n\nfunc a() {}\n")
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	base = gitRun(t, dir, "rev-parse", "HEAD")
	for i := 0; i < changedFiles; i++ {
		writeFileAt(t, dir, fmt.Sprintf("f%d.go", i), "package main\n\nfunc a() { b() }\n\nfunc b() {}\n")
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")
	head = gitRun(t, dir, "rev-parse", "HEAD")
	return dir, base, head
}

// The manifest's escalation disclosure rides two json tags; a typo in either
// silently drops the field from the on-disk artifact while every typed read
// keeps compiling. Pin the wire names and the omitempty contract.
func TestManifest_EscalationFieldsRoundTrip(t *testing.T) {
	m := &payload.Manifest{
		Base:               "a",
		Head:               "b",
		PerFilePayload:     map[string]string{"f0.go": "files"},
		EscalationDegraded: true,
	}
	data, err := json.Marshal(m)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	require.Contains(t, raw, "per_file_payload")
	require.Contains(t, raw, "escalation_degraded")

	var got payload.Manifest
	require.NoError(t, json.Unmarshal(data, &got))
	require.Equal(t, map[string]string{"f0.go": "files"}, got.PerFilePayload)
	require.True(t, got.EscalationDegraded)

	// Zero values must be elided so a no-escalation manifest stays
	// byte-identical to the pre-Epic-35.1 shape.
	zero, err := json.Marshal(&payload.Manifest{Base: "a", Head: "b"})
	require.NoError(t, err)
	var zeroRaw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(zero, &zeroRaw))
	require.NotContains(t, zeroRaw, "per_file_payload")
	require.NotContains(t, zeroRaw, "escalation_degraded")
}

// finalizePreparedReview must write the escalation disclosure into the
// manifest: PerFilePayload from the built payloads and EscalationDegraded from
// the shared RangeBuilder. Dropping either literal line (review.go) passes the
// whole suite without this test.
func TestPrepareReview_ManifestRecordsEscalation(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")

	t.Run("escalated file recorded", func(t *testing.T) {
		repo, base, head := escalationRepo(t, 1)
		srv := mockProvider(t)
		cfg := twoAgentConfig(srv.URL)
		// Force both escalation signals to fire on any change so the file is
		// promoted above the roster's configured blocks mode.
		cfg.Registry.PayloadEscalation = registry.PayloadEscalationConfig{
			MinHunks:      ptrInt(1),
			MinCyclomatic: ptrInt(1),
		}

		prep, err := PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
		require.NoError(t, err)

		m := readManifest(t, prep.Dir)
		require.Equal(t, map[string]string{"f0.go": "files"}, m.PerFilePayload,
			"manifest must record the mode each escalated file was actually rendered in")
		require.False(t, m.EscalationDegraded, "one file is within the default file cap")
	})

	t.Run("degraded run disclosed", func(t *testing.T) {
		repo, base, head := escalationRepo(t, 2)
		srv := mockProvider(t)
		cfg := twoAgentConfig(srv.URL)
		cfg.Registry.PayloadEscalation = registry.PayloadEscalationConfig{
			MinHunks:      ptrInt(1),
			MinCyclomatic: ptrInt(1),
			MaxFiles:      ptrInt(1), // two changed files exceed the cap
		}

		prep, err := PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
		require.NoError(t, err)

		m := readManifest(t, prep.Dir)
		require.True(t, m.EscalationDegraded,
			"a change set above the file cap must be disclosed as degraded")
		require.Empty(t, m.PerFilePayload,
			"a degraded run escalates nothing, so per_file_payload must stay empty")
	})
}

func readManifest(t *testing.T, dir string) payload.Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}
