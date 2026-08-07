package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// findSubCmd returns the direct child of parent named name, or nil.
func findSubCmd(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// TD row (cli/verify_diff.go): `verify` names three unrelated operations and is
// simultaneously a leaf and a group, so `atcr verify diff` reads as "verify the
// diff" when it means "scan the diff for reward-hack smells". The clarified fix:
// promote the scanner to top-level `atcr diff-smell` — the name the analyzer, the
// doc, the upstream tool, and the command's own error text already use — keeping
// `atcr verify diff` as a hidden deprecated alias for one minor version.

func TestDiffSmellCmd_TopLevelRegistered(t *testing.T) {
	root := NewRootCmd()
	cmd := findSubCmd(root, "diff-smell")
	require.NotNil(t, cmd, "the diff-smell scanner must be a top-level `atcr diff-smell` command")
	require.NotNil(t, cmd.Flags().Lookup("fail-on"),
		"the documented version probe greps `atcr diff-smell --help` for --fail-on")
	require.NotNil(t, cmd.Flags().Lookup("staged"))
}

func TestVerifyDiff_IsHiddenDeprecatedAlias(t *testing.T) {
	root := NewRootCmd()
	verify := findSubCmd(root, "verify")
	require.NotNil(t, verify)
	diff := findSubCmd(verify, "diff")
	require.NotNil(t, diff, "`atcr verify diff` must stay reachable for the one-minor-version alias window")
	require.True(t, diff.Hidden,
		"`atcr verify diff` must be hidden from `atcr verify --help` (cobra's Deprecated field is unusable: it prints to stdout, breaking payload-only `--json`)")
	require.Empty(t, diff.Deprecated,
		"cobra's Deprecated prints its warning to STDOUT — the deprecation signal lives in the RunE stderr note instead")
}

// TestDiffSmellCmd_ParityWithVerifyDiffAlias runs the same scan through both
// spellings and requires identical stdout and exit codes — the alias is a
// rename, not a second implementation.
func TestDiffSmellCmd_ParityWithVerifyDiffAlias(t *testing.T) {
	f := filepath.Join(t.TempDir(), "smell.diff")
	require.NoError(t, os.WriteFile(f, []byte(smellSoftDiff), 0o600))

	run := func(args ...string) (code int, stdout, stderr string) {
		var outBuf, errBuf bytes.Buffer
		root := NewRootCmd()
		root.SetArgs(args)
		root.SetIn(strings.NewReader(""))
		root.SetOut(&outBuf)
		root.SetErr(&errBuf)
		err := root.Execute()
		if err != nil {
			errBuf.WriteString(err.Error())
		}
		return exitCode(err), outBuf.String(), errBuf.String()
	}

	codeNew, outNew, _ := run("diff-smell", "--diff", f)
	codeOld, outOld, errOld := run("verify", "diff", "--diff", f)

	require.Equal(t, codeOld, codeNew, "alias and canonical name must exit identically")
	require.Equal(t, outOld, outNew, "the deprecated alias must render identically to `atcr diff-smell`")
	require.Contains(t, outNew, "verdict:", "the scan must produce its verdict line")
	require.Contains(t, errOld, "deprecated",
		"invoking the alias must print a deprecation warning on stderr (stdout stays payload-only)")
}
