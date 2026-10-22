package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPersonaStamp_WriteThenReadRoundTrips pins the stamp's contract: every
// entry written, `_base.md` included, reads back with the same hash.
func TestPersonaStamp_WriteThenReadRoundTrips(t *testing.T) {
	dir := t.TempDir()

	contents := map[string]string{
		"_base.md": "# base\n",
		"bruce.md": "# bruce\n\n## Focus\n1. Correctness\n",
		"greta.md": "# greta\n",
	}
	stamp := NewPersonaStamp()
	for name, content := range contents {
		stamp.Record(name, []byte(content))
	}
	require.NoError(t, WritePersonaStamp(dir, stamp))

	got, ok := ReadPersonaStamp(dir)
	require.True(t, ok)
	require.Equal(t, stamp, got)
	require.Len(t, got.Files, len(contents))
	for name, content := range contents {
		sum := sha256.Sum256([]byte(content))
		require.Equal(t, hex.EncodeToString(sum[:]), got.Files[name], name)
	}

	// The stamp lands at .atcr/personas/.stamp.json with no temp file left over.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, PersonaStampFile, entries[0].Name())
}

// TestPersonaStamp_RecordKeepsOtherEntries pins that a re-run's Record
// replaces only the entries for the files it wrote.
func TestPersonaStamp_RecordKeepsOtherEntries(t *testing.T) {
	dir := t.TempDir()
	first := NewPersonaStamp()
	first.Record("_base.md", []byte("base v1"))
	first.Record("bruce.md", []byte("bruce v1"))
	require.NoError(t, WritePersonaStamp(dir, first))

	again, ok := ReadPersonaStamp(dir)
	require.True(t, ok)
	again.Record("bruce.md", []byte("bruce v2"))
	require.NoError(t, WritePersonaStamp(dir, again))

	got, ok := ReadPersonaStamp(dir)
	require.True(t, ok)
	require.Equal(t, HashPersonaContent([]byte("base v1")), got.Files["_base.md"])
	require.Equal(t, HashPersonaContent([]byte("bruce v2")), got.Files["bruce.md"])
}

// TestPersonaStamp_EmptyRoundTrips pins that a stamp with no entries (and a
// zero-value one) still writes a readable stamp rather than "no stamp".
func TestPersonaStamp_EmptyRoundTrips(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WritePersonaStamp(dir, PersonaStamp{}))
	got, ok := ReadPersonaStamp(dir)
	require.True(t, ok)
	require.Empty(t, got.Files)
}

// TestPersonaStamp_RecordOnZeroValue pins that Record works on a zero-value
// PersonaStamp (nil Files) instead of panicking on a nil-map write.
func TestPersonaStamp_RecordOnZeroValue(t *testing.T) {
	dir := t.TempDir()
	var stamp PersonaStamp
	stamp.Record("_base.md", []byte("base"))
	require.NoError(t, WritePersonaStamp(dir, stamp))

	got, ok := ReadPersonaStamp(dir)
	require.True(t, ok)
	require.Equal(t, map[string]string{"_base.md": HashPersonaContent([]byte("base"))}, got.Files)
}

// TestPersonaStamp_MissingOrUnparseableIsNoStamp pins the degrade-safely
// contract: a workspace initialized before the stamp existed, or one whose
// stamp is damaged, reads as "no stamp" — never an error.
func TestPersonaStamp_MissingOrUnparseableIsNoStamp(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		got, ok := ReadPersonaStamp(t.TempDir())
		require.False(t, ok)
		require.Nil(t, got.Files)
	})
	t.Run("missing directory", func(t *testing.T) {
		_, ok := ReadPersonaStamp(filepath.Join(t.TempDir(), "absent"))
		require.False(t, ok)
	})
	for name, body := range map[string]string{
		"truncated":       `{"files": {"bruce.md": "ab`,
		"not json":        "not json at all\n",
		"empty file":      "",
		"wrong shape":     `{"files": ["bruce.md"]}`,
		"no files object": `{}`,
		"null files":      `{"files": null}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(PersonaStampPath(dir), []byte(body), 0o644))
			got, ok := ReadPersonaStamp(dir)
			require.False(t, ok)
			require.Nil(t, got.Files)
		})
	}
}

// TestPersonaStamp_WriteReplacesExisting pins that the writer overwrites a
// prior (even damaged) stamp in place.
func TestPersonaStamp_WriteReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(PersonaStampPath(dir), []byte("garbage"), 0o644))
	stamp := NewPersonaStamp()
	stamp.Record("bruce.md", []byte("x"))
	require.NoError(t, WritePersonaStamp(dir, stamp))
	got, ok := ReadPersonaStamp(dir)
	require.True(t, ok)
	require.Equal(t, stamp, got)
}

// TestPersonaStampDamaged pins the damaged-stamp signal (Epic 66): a stamp
// file that exists but does not parse, or has no "files" object, is damaged;
// a missing stamp and a valid one are not. ReadPersonaStamp still treats a
// damaged stamp as no stamp, so the staleness verdict is unchanged.
func TestPersonaStampDamaged(t *testing.T) {
	for name, body := range map[string]string{
		"unparseable":     "{",
		"no files object": `{"version": 1}`,
		"null files":      `{"files": null}`,
		"empty file":      "",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(PersonaStampPath(dir), []byte(body), 0o644))
			require.True(t, PersonaStampDamaged(dir))
			got, ok := ReadPersonaStamp(dir)
			require.False(t, ok)
			require.Nil(t, got.Files)
		})
	}
	t.Run("unreadable", func(t *testing.T) {
		// A directory at the stamp path exists but cannot be read as a file.
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(PersonaStampPath(dir), 0o755))
		require.True(t, PersonaStampDamaged(dir))
		_, ok := ReadPersonaStamp(dir)
		require.False(t, ok)
	})
	t.Run("missing file", func(t *testing.T) {
		require.False(t, PersonaStampDamaged(t.TempDir()))
	})
	t.Run("missing directory", func(t *testing.T) {
		require.False(t, PersonaStampDamaged(filepath.Join(t.TempDir(), "absent")))
	})
	t.Run("valid stamp", func(t *testing.T) {
		dir := t.TempDir()
		stamp := NewPersonaStamp()
		stamp.Record("bruce.md", []byte("# bruce\n"))
		require.NoError(t, WritePersonaStamp(dir, stamp))
		require.False(t, PersonaStampDamaged(dir))
	})
	t.Run("valid empty stamp", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, WritePersonaStamp(dir, NewPersonaStamp()))
		require.False(t, PersonaStampDamaged(dir))
	})
}

// TestPersonaStamp_DanglingSymlinkIsDamaged pins TD persona_stamp.go:78: a
// stamp path that is a symlink to nothing exists (Lstat sees it) but cannot say
// what init wrote, so it is damaged, not silently missing. It still reads as no
// stamp.
func TestPersonaStamp_DanglingSymlinkIsDamaged(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "absent.json"), PersonaStampPath(dir)); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	require.True(t, PersonaStampDamaged(dir))
	got, ok := ReadPersonaStamp(dir)
	require.False(t, ok)
	require.Nil(t, got.Files)
}

// TestPersonaStamp_SymlinkToValidStampIsDamaged pins the Lstat rule: the stamp
// must be a regular file itself. init writes it by an atomic rename, so it is
// never a symlink; one that is (even to a valid stamp) is not what init wrote,
// and following it is how a tracked stamp reaches a FIFO or a device.
func TestPersonaStamp_SymlinkToValidStampIsDamaged(t *testing.T) {
	dir := t.TempDir()
	target := t.TempDir()
	stamp := NewPersonaStamp()
	stamp.Record("bruce.md", []byte("# bruce\n"))
	require.NoError(t, WritePersonaStamp(target, stamp))
	if err := os.Symlink(PersonaStampPath(target), PersonaStampPath(dir)); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	require.True(t, PersonaStampDamaged(dir))
	_, ok := ReadPersonaStamp(dir)
	require.False(t, ok)
}

// TestPersonaStamp_OversizedIsDamaged pins TD persona_stamp.go:65's read bound:
// a stamp over maxPersonaStampBytes is damaged and reads as no stamp, even when
// it is valid JSON with a "files" object, so neither read takes it whole.
func TestPersonaStamp_OversizedIsDamaged(t *testing.T) {
	dir := t.TempDir()
	pad := strings.Repeat(" ", 1<<20)
	require.NoError(t, os.WriteFile(PersonaStampPath(dir), []byte(`{"files": {}}`+pad), 0o644))
	require.True(t, PersonaStampDamaged(dir))
	got, ok := ReadPersonaStamp(dir)
	require.False(t, ok)
	require.Nil(t, got.Files)

	// At the cap exactly it still reads: the bound refuses only what is over it.
	body := []byte(`{"files": {}}`)
	body = append(body, []byte(strings.Repeat(" ", 1<<20-len(body)))...)
	require.Len(t, body, 1<<20)
	require.NoError(t, os.WriteFile(PersonaStampPath(dir), body, 0o644))
	require.False(t, PersonaStampDamaged(dir))
	_, ok = ReadPersonaStamp(dir)
	require.True(t, ok)
}

// TestPersonaStampDamaged_StatErrorIsDamaged pins the second half of TD
// persona_stamp.go:78: an Lstat error other than "not exist" (here a path
// component that is a file, so the lookup fails with ENOTDIR) is damaged rather
// than silently missing.
func TestPersonaStampDamaged_StatErrorIsDamaged(t *testing.T) {
	parent := t.TempDir()
	notDir := filepath.Join(parent, "personas")
	require.NoError(t, os.WriteFile(notDir, []byte("x"), 0o644))
	_, err := os.Lstat(PersonaStampPath(notDir))
	require.Error(t, err)
	if os.IsNotExist(err) {
		t.Skipf("platform reports %v as not-exist", err)
	}
	require.True(t, PersonaStampDamaged(notDir))
	_, ok := ReadPersonaStamp(notDir)
	require.False(t, ok)
}
