package localdebt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestManualRunID_RejectsOutOfRangeYears pins the validated (string, error)
// contract (TD internal/localdebt/paths.go:51): years outside 1-9999 format
// through RFC3339 as "10000-01-…" or "-0001-01-…" — neither matches monthRe,
// so Append would reject the record outright — and the ZERO time formats as
// "0001-01-…", which monthRe ACCEPTS, silently creating a permanent phantom
// .atcr/debt/0001-01.jsonl shard. The helper now errors instead of formatting
// blindly, so the precondition is enforced at the boundary rather than trusted
// to every caller's reading of a doc comment.
func TestManualRunID_RejectsOutOfRangeYears(t *testing.T) {
	cases := []struct {
		name    string
		ts      time.Time
		wantErr bool
	}{
		{"zero time (phantom 0001-01 shard)", time.Time{}, true},
		{"year 0", time.Date(0, 6, 14, 10, 0, 0, 0, time.UTC), true},
		{"negative year", time.Date(-5, 6, 14, 10, 0, 0, 0, time.UTC), true},
		{"year 1 (floor, valid)", time.Date(1, 6, 14, 10, 0, 0, 0, time.UTC), false},
		{"year 9999 (ceiling, valid)", time.Date(9999, 6, 14, 10, 0, 0, 0, time.UTC), false},
		{"year 10000", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"ordinary present day", time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := ManualRunID(tc.ts)
			if tc.wantErr {
				require.Error(t, err)
				assert.Empty(t, id, "a rejected timestamp must not produce a run_id")
			} else {
				require.NoError(t, err)
				// Every accepted timestamp must resolve to a month shard.
				_, merr := monthFromRunID(id)
				require.NoError(t, merr, "an accepted run_id must carry a resolvable YYYY-MM prefix")
			}
		})
	}
}

// TestEnsureStoreDir_MissingAnchorNamesTheConfiguredStore pins row 993fffb0c24de643
// (TD internal/localdebt/paths.go:45). ensureStoreDir's existence check sits two
// levels above the store dir — the repo root ONLY for the default <root>/.atcr/debt
// layout. A configured debt.store (StoreDir) may sit at any depth: for
// docs/planning/debt the checked anchor is <root>/docs, which is not the repo root.
// When that anchor is missing, the refusal must name the CONFIGURED STORE and the
// directory that is actually missing, not label the anchor "store root" — the wrong
// diagnosis sends the operator looking for a vanished checkout instead of a missing
// parent of their configured store.
func TestEnsureStoreDir_MissingAnchorNamesTheConfiguredStore(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "planning", "debt")

	err := ensureStoreDir(dir)
	require.Error(t, err, "a store whose grandparent is missing must be refused, not created")
	assert.Contains(t, err.Error(), `"debt"`, "the refusal must name the configured store")
	assert.Contains(t, err.Error(), `"docs"`, "the refusal must name the directory that is actually missing")
	assert.NotContains(t, err.Error(), "store root",
		"the missing grandparent of a configured store is not the store root; calling it that misdiagnoses the cause")
	_, statErr := os.Stat(filepath.Join(root, "docs"))
	assert.True(t, os.IsNotExist(statErr), "the refusal must not create anything: %v", statErr)
}

// TestEnsureStoreDir_DefaultLayoutStillRefusesAVanishedRoot keeps the original
// stale-root stop signal intact under the new message: for the default layout the
// grandparent IS the repo root, and a root that vanished between validation and the
// write must still be an error, never a resurrection (TD internal/localdebt/root.go:123).
func TestEnsureStoreDir_DefaultLayoutStillRefusesAVanishedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "gone-repo")

	err := ensureStoreDir(DefaultDir(root))
	require.Error(t, err, "a vanished repo root must be refused, not recreated")
	assert.Contains(t, err.Error(), `"debt"`, "the refusal names the store")
	assert.Contains(t, err.Error(), `"gone-repo"`, "the refusal names the missing root")
	_, statErr := os.Stat(root)
	assert.True(t, os.IsNotExist(statErr), "the missing root must not be created: %v", statErr)
}

// TestEnsureStoreDir_ConfiguredStoreUnderExistingGrandparentIsCreated guards that
// the message fix changes no behavior: when the grandparent exists, a configured
// store at a non-default depth is still created on demand (the intermediate
// docs/planning is ordinary first-write behavior, paths.go MkdirAll).
func TestEnsureStoreDir_ConfiguredStoreUnderExistingGrandparentIsCreated(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	dir := filepath.Join(root, "docs", "planning", "debt")

	require.NoError(t, ensureStoreDir(dir))
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "the configured store must exist as a directory")
}

// TestRedactPathErr pins the exported path-reduction contract (TD
// internal/localdebt/paths.go:51 — the row's FIX cell was a reconcile
// copy-paste of the ManualRunID item; this is the clarified fix): a surfaced
// *os.PathError is reduced to its base name so an absolute, username-bearing
// store path never reaches a diagnostic sink or an error string, while the
// operational Op and underlying Err are preserved; a non-PathError passes
// through unchanged.
func TestRedactPathErr(t *testing.T) {
	abs := filepath.Join(string(filepath.Separator), "home", "somebody", "repo", ".atcr", "debt", "2026-08.jsonl")
	pathErr := &os.PathError{Op: "open", Path: abs, Err: os.ErrPermission}

	redacted := RedactPathErr(pathErr)
	var pe *os.PathError
	require.ErrorAs(t, redacted, &pe, "a *os.PathError stays a *os.PathError")
	assert.Equal(t, filepath.Base(abs), pe.Path, "the absolute path is reduced to its base name")
	assert.Equal(t, "open", pe.Op, "the operational Op is preserved")
	assert.ErrorIs(t, redacted, os.ErrPermission, "the wrapped Err is preserved")
	assert.NotContains(t, redacted.Error(), "somebody", "no username-bearing path segment reaches the error string")

	other := errors.New("boom")
	assert.Same(t, other, RedactPathErr(other), "a non-PathError passes through unchanged")
}
