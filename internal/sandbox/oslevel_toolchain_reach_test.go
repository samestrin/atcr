package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeExecutable creates an executable file named name inside dir.
func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	return p
}

// TestCheckToolchainReachable_RefusesOnlyWhenSanitizationLostTheTool is the
// discriminating case, and the only one that may refuse.
//
// The sandbox hands the workload a SANITIZED PATH, so a tool the operator can
// run on the host may be unreachable inside the run — which surfaced as an
// opaque `command not found` mid-run, after the review had already started (and,
// for --auto-fix, after the patch was applied). This turns that into a refusal at
// the gate.
//
// The refusal is deliberately narrow: it fires only when the tool IS on the host
// PATH and is NOT on the sanitized one. That difference is what proves
// sanitization specifically dropped it, rather than the tool simply being absent
// — which is a pre-existing configuration error with its own message, and not
// this check's business to relabel.
func TestCheckToolchainReachable_RefusesOnlyWhenSanitizationLostTheTool(t *testing.T) {
	root := t.TempDir()
	// 0777: world-writable, so sanitizeSandboxPath drops it under every tier.
	dropped := filepath.Join(root, "dropped")
	require.NoError(t, os.Mkdir(dropped, 0o755))
	require.NoError(t, os.Chmod(dropped, 0o777))
	writeExecutable(t, dropped, "atcr-probe-tool")

	t.Setenv("PATH", dropped)

	err := CheckToolchainReachable([]string{"atcr-probe-tool", "test", "./..."})

	require.Error(t, err, "a tool the host can run but the sandbox cannot must be refused at the gate")
	assert.Contains(t, err.Error(), "atcr-probe-tool")
	assert.Contains(t, strings.ToLower(err.Error()), "path",
		"the message must point at the PATH sanitization, or the operator cannot act on it")
}

// TestCheckToolchainReachable_SilentWhenTheToolIsAbsentEverywhere is the first
// false-refusal guard. A tool missing from the host PATH too is a pre-existing
// config error that fails loudly on its own with an accurate message; hijacking
// it here would misattribute the cause to sandboxing.
func TestCheckToolchainReachable_SilentWhenTheToolIsAbsentEverywhere(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	assert.NoError(t, CheckToolchainReachable([]string{"atcr-definitely-not-installed-xyz"}))
}

// TestCheckToolchainReachable_SilentForPathQualifiedCommands is the second
// false-refusal guard, and the important one: a repo-local script
// (./scripts/test.sh, bin/validate) is resolved against the run's working
// directory — the snapshot — not through PATH at all. Running it through a PATH
// lookup would refuse a command that works perfectly.
func TestCheckToolchainReachable_SilentForPathQualifiedCommands(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, cmd := range [][]string{
		{"./scripts/test.sh"},
		{"bin/validate", "--all"},
		{"/usr/local/opt/whatever/bin/tool"},
	} {
		assert.NoError(t, CheckToolchainReachable(cmd), "%v is path-qualified, not a PATH lookup", cmd)
	}
}

// TestCheckToolchainReachable_SilentWhenReachable is the paired positive control.
// Without it, a check that refused everything — or nothing — would pass the cases
// above just as well.
func TestCheckToolchainReachable_SilentWhenReachable(t *testing.T) {
	root := t.TempDir()
	kept := filepath.Join(root, "kept")
	require.NoError(t, os.Mkdir(kept, 0o755)) // 0755 survives sanitization
	writeExecutable(t, kept, "atcr-probe-tool")

	t.Setenv("PATH", kept)

	assert.NoError(t, CheckToolchainReachable([]string{"atcr-probe-tool"}),
		"a tool on the sanitized PATH must not be refused")
}

// TestCheckToolchainReachable_EmptyCommandIsNotAnError keeps the check out of the
// business of validating command SHAPE, which registry.SandboxConfig.Validate
// already owns. Two checks refusing the same config with different messages is
// worse than one.
func TestCheckToolchainReachable_EmptyCommandIsNotAnError(t *testing.T) {
	assert.NoError(t, CheckToolchainReachable(nil))
	assert.NoError(t, CheckToolchainReachable([]string{}))
	assert.NoError(t, CheckToolchainReachable([]string{"   "}))
}

// TestCheckToolchainReachable_HomebrewShapedDirIsReachable ties this check to the
// vetted-toolchain-prefix exemption. The exemption exists so Homebrew's 0775
// bin dir survives sanitization; if the two ever disagree, this check would
// refuse exactly the runs the exemption was added to enable.
func TestCheckToolchainReachable_HomebrewShapedDirIsReachable(t *testing.T) {
	root := t.TempDir()
	vetted := filepath.Join(root, "vetted")
	require.NoError(t, os.Mkdir(vetted, 0o755))

	orig := vettedToolchainPrefixes
	vettedToolchainPrefixes = []string{vetted}
	t.Cleanup(func() { vettedToolchainPrefixes = orig })

	brewBin := filepath.Join(vetted, "bin")
	require.NoError(t, os.Mkdir(brewBin, 0o755))
	require.NoError(t, os.Chmod(brewBin, 0o775)) // Homebrew's measured mode
	writeExecutable(t, brewBin, "atcr-probe-tool")

	t.Setenv("PATH", brewBin)

	assert.NoError(t, CheckToolchainReachable([]string{"atcr-probe-tool"}),
		"the vetted-prefix exemption and this check must agree, or Homebrew runs are refused at the gate")
}
