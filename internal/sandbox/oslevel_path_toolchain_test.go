package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mkdirMode creates dir under parent with an explicit mode, defeating umask.
func mkdirMode(t *testing.T, parent, name string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(parent, name)
	require.NoError(t, os.Mkdir(p, 0o755))
	require.NoError(t, os.Chmod(p, mode)) // Chmod, not Mkdir's perm: umask masks the latter
	return p
}

// TestSanitizeSandboxPath_KeepsGroupWritableToolchainPrefixes covers the measured
// breakage: a standard Homebrew install owns /opt/homebrew/bin as 0775 (group
// `admin`), so the blanket group-writable rejection dropped it from the PATH
// handed to the workload — and `node` exists ONLY there, so a real sandboxed run
// reported `command not found`. `go` survived by accident via a 0755 Cellar bin
// dir, which made WHICH tools worked depend on each formula's layout.
//
// The profile generator had ALREADY reviewed and accepted group-writability at
// these named prefixes (darwinSystemReadDirs' toolchain tier); the sanitizer
// simply did not consult that decision. This asserts the two now agree.
func TestSanitizeSandboxPath_KeepsGroupWritableToolchainPrefixes(t *testing.T) {
	root := t.TempDir()
	vetted := mkdirMode(t, root, "vetted-toolchain", 0o755)

	orig := vettedToolchainPrefixes
	vettedToolchainPrefixes = []string{vetted}
	t.Cleanup(func() { vettedToolchainPrefixes = orig })

	// 0775, exactly Homebrew's measured mode: group-writable, not world-writable.
	brewBin := mkdirMode(t, vetted, "bin", 0o775)
	// The same mode OUTSIDE any vetted prefix stays rejected — that is the
	// plantable-directory case the sanitizer exists for, and the exemption must
	// be anchored to the named prefixes, not to the mode.
	strayBin := mkdirMode(t, root, "stray-bin", 0o775)
	safeBin := mkdirMode(t, root, "safe-bin", 0o755)

	got := sanitizeSandboxPath(strings.Join([]string{safeBin, brewBin, strayBin}, ":"))
	kept := strings.Split(got, ":")

	assert.Contains(t, kept, brewBin,
		"a group-writable dir under a vetted toolchain prefix must survive, or the sandbox cannot run Homebrew tools")
	assert.Contains(t, kept, safeBin, "the ordinary 0755 case must be unaffected")
	assert.NotContains(t, kept, strayBin,
		"the exemption must be anchored to the vetted prefixes, never to the mode")
}

// TestSanitizeSandboxPath_StillRejectsWorldWritableUnderVettedPrefix keeps the
// exemption as narrow as the decision that justified it.
//
// The accepted argument is about GROUP-writability at these prefixes (Homebrew's
// 0775, gid `admin`): an attacker in that group has already replaced the
// operator's toolchain for every unsandboxed use, so excluding the directory buys
// nothing. A WORLD-writable toolchain dir is a different claim — any local user,
// no group membership required — and nothing has reviewed and accepted that.
func TestSanitizeSandboxPath_StillRejectsWorldWritableUnderVettedPrefix(t *testing.T) {
	root := t.TempDir()
	vetted := mkdirMode(t, root, "vetted-toolchain", 0o755)

	orig := vettedToolchainPrefixes
	vettedToolchainPrefixes = []string{vetted}
	t.Cleanup(func() { vettedToolchainPrefixes = orig })

	worldWritable := mkdirMode(t, vetted, "bin", 0o777)
	safeBin := mkdirMode(t, root, "safe-bin", 0o755)

	kept := strings.Split(sanitizeSandboxPath(safeBin+":"+worldWritable), ":")

	assert.NotContains(t, kept, worldWritable,
		"world-writable is a strictly larger claim than the group-writable case that was accepted")
	assert.Contains(t, kept, safeBin)
}

// TestUnderVettedToolchainPrefix_MatchesOnPathBoundaries pins the matcher against
// the bug a bare strings.HasPrefix would introduce: /usr/locale and
// /opt/homebrewery are NOT under /usr/local and /opt/homebrew, and admitting them
// would hand the exemption to exactly the attacker-plantable sibling directory
// the sanitizer exists to reject.
func TestUnderVettedToolchainPrefix_MatchesOnPathBoundaries(t *testing.T) {
	prefixes := []string{"/opt/homebrew", "/usr/local"}

	for _, in := range []string{"/opt/homebrew", "/opt/homebrew/bin", "/usr/local/bin", "/opt/homebrew/"} {
		assert.True(t, underVettedToolchainPrefix(in, prefixes), "%s must be admitted", in)
	}
	for _, in := range []string{"/opt/homebrewery/bin", "/usr/locale/bin", "/opt", "/usr", "/opt/homebrew-evil", "/"} {
		assert.False(t, underVettedToolchainPrefix(in, prefixes), "%s must NOT be admitted", in)
	}
}

// TestVettedToolchainPrefixes_AreTheProfilesToolchainTier proves the two consumers
// share ONE declaration. If someone re-adds a literal prefix to the profile list
// without adding it here, the sanitizer would silently keep dropping it — the
// exact drift this consolidation removes.
func TestVettedToolchainPrefixes_AreTheProfilesToolchainTier(t *testing.T) {
	for _, p := range vettedToolchainPrefixes {
		assert.Contains(t, darwinSystemReadDirs, p,
			"the profile's read tier must grant every prefix the PATH filter trusts")
	}
	assert.Contains(t, vettedToolchainPrefixes, "/opt/homebrew",
		"the measured breakage was Homebrew; losing this entry silently reopens it")
}
