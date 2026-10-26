package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSuggestions_RenderEveryRegistryTemplate is the T1 success criterion: every
// registry template renders with the allowlisted flags of the current invocation
// carried forward and <id> left as a literal placeholder, never a concrete id.
func TestSuggestions_RenderEveryRegistryTemplate(t *testing.T) {
	inv := invocation{Flags: map[string]string{"repo-root": "/src/app"}, Pending: true}
	want := map[suggestionKey][]string{
		suggestReview: {
			"atcr report <id>",
			"atcr verify <id>",
		},
		suggestReviewInterrupted: {
			"atcr status <id>",
			"atcr review --resume <id>",
		},
		suggestReport: {
			"atcr debt list",
			"atcr verify <id>",
		},
		suggestReconcile: {
			"atcr debt list",
			"atcr verify <id> --repo-root=/src/app",
		},
		suggestVerify: {"atcr report <id>"},
		suggestDebate: {"atcr report <id>"},
		suggestStatus: {
			"atcr report <id>",
			"atcr review --resume <id>",
		},
		suggestHome: {
			"atcr review",
			"atcr report <id>",
		},
	}
	require.Len(t, suggestionRegistry, len(want), "a registry entry has no expected rendering here")
	for key, steps := range want {
		got := renderSuggestions(key, inv)
		assert.Equal(t, steps, got, "key %s", key)
		for _, line := range got {
			assert.NotContains(t, line, "/src/app/", "no guessed value leaks into %q", line)
		}
	}
}

// TestSuggestions_UnusedFlagIsOmitted: a carried flag the invocation did not set
// is left out entirely rather than rendered with a default or a placeholder.
func TestSuggestions_UnusedFlagIsOmitted(t *testing.T) {
	got := renderSuggestions(suggestReconcile, invocation{})
	assert.Equal(t, []string{"atcr debt list", "atcr verify <id>"}, got)
}

// TestSuggestions_PendingOnlyStepDroppedWhenNotPending: the status entry offers
// `review --resume <id>` only when the review is pending.
func TestSuggestions_PendingOnlyStepDroppedWhenNotPending(t *testing.T) {
	got := renderSuggestions(suggestStatus, invocation{})
	assert.Equal(t, []string{"atcr report <id>"}, got)
}

// TestSuggestions_NonAllowlistedFlagRendersPlaceholder: a template that carries a
// flag outside the allowlist renders it as --flag <flag>, never the value the
// user typed (AC5).
func TestSuggestions_NonAllowlistedFlagRendersPlaceholder(t *testing.T) {
	step := nextStep{Command: "verify", Args: []string{idPlaceholder}, Carry: []string{"min-severity", "repo-root"}}
	inv := invocation{Flags: map[string]string{"min-severity": "HIGH", "repo-root": "r"}}
	assert.Equal(t, "atcr verify <id> --min-severity <min-severity> --repo-root=r", step.render(inv))
}

// TestSuggestions_CarriedValueIsSanitizedAndQuoted: a carried value passes
// goaxi.SanitizeString (control bytes and invalid UTF-8 removed) and is
// shell-quoted when it is not a plain word, so one line stays one runnable line.
func TestSuggestions_CarriedValueIsSanitizedAndQuoted(t *testing.T) {
	step := nextStep{Command: "verify", Args: []string{idPlaceholder}, Carry: []string{"repo-root"}}
	cases := map[string]string{
		"/plain/path-1.0_x":   "--repo-root=/plain/path-1.0_x",
		"/a b":                "--repo-root='/a b'",
		"it's":                `--repo-root='it'\''s'`,
		"/x\x1b[31m\ny":       "--repo-root='/x[31m y'",
		"<id>":                "--repo-root='<id>'",
		"":                    "--repo-root=''",
		"/bad\xffutf8":        "--repo-root=/badutf8",
		"$(rm -rf ~)":         "--repo-root='$(rm -rf ~)'",
		"-dash-leading":       "--repo-root=-dash-leading",
		"C:\\Users\\me\\repo": `--repo-root='C:\Users\me\repo'`,
	}
	for in, want := range cases {
		got := step.render(invocation{Flags: map[string]string{"repo-root": in}})
		assert.Equal(t, "atcr verify <id> "+want, got, "input %q", in)
		assert.NotContains(t, got, "\n")
	}
}

// TestSuggestions_UnknownKeyRendersNothing: a key with no registry entry yields
// no lines, so a caller never prints an empty help block by accident.
func TestSuggestions_UnknownKeyRendersNothing(t *testing.T) {
	assert.Empty(t, renderSuggestions("no-such-command", invocation{}))
}

// TestSuggestions_RegistryCarriesOnlyAllowlistedFlags keeps the shipped registry
// honest: every flag it carries is on the allowlist, so no registry line emits
// a placeholder where a real value was expected.
func TestSuggestions_RegistryCarriesOnlyAllowlistedFlags(t *testing.T) {
	for key, steps := range suggestionRegistry {
		for _, s := range steps {
			for _, f := range s.Carry {
				assert.True(t, carryAllowlist[f], "%s -> %s carries non-allowlisted --%s", key, s.Command, f)
			}
			assert.False(t, strings.HasPrefix(s.Command, "atcr"), "%s: Command must omit the binary name", key)
		}
	}
}

// TestInvocationFlags_OnlyChangedFlags: invocationFlags reports the flags the
// user actually set, with their values, and ignores flags left at default.
func TestInvocationFlags_OnlyChangedFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "reconcile", RunE: func(*cobra.Command, []string) error { return nil }}
	cmd.Flags().String("repo-root", ".", "")
	cmd.Flags().String("fail-on", "", "")
	cmd.Flags().Bool("no-scorecard", false, "")
	require.NoError(t, cmd.ParseFlags([]string{"--repo-root", "/src/app", "--no-scorecard"}))
	assert.Equal(t, map[string]string{"repo-root": "/src/app", "no-scorecard": "true"}, invocationFlags(cmd))
}

// TestInvocationFlags_DeprecatedAliasFoldsIntoCanonical: --repo is a deprecated
// alias for --repo-root on reconcile and verify, and reconcile honors it when
// --repo-root is unset (cli/reconcile.go:97). Dropping it would point the
// suggested `verify` at the wrong tree, so it carries as repo-root; an explicit
// --repo-root still wins.
func TestInvocationFlags_DeprecatedAliasFoldsIntoCanonical(t *testing.T) {
	newCmd := func() *cobra.Command {
		cmd := &cobra.Command{Use: "reconcile"}
		cmd.Flags().String("repo-root", ".", "")
		cmd.Flags().String("repo", ".", "")
		return cmd
	}
	cmd := newCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--repo", "/old"}))
	assert.Equal(t, map[string]string{"repo-root": "/old"}, invocationFlags(cmd))

	cmd = newCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--repo", "/old", "--repo-root", "/new"}))
	assert.Equal(t, map[string]string{"repo-root": "/new"}, invocationFlags(cmd))
}

// suggestionSources is every covered command and the registry key it renders.
// The test below derives both sides from the live tree (NewRootCmd) and the live
// registry, the way TestReportInputSchema_Enum ties the MCP enum to
// report.FormatList(). A nil path is the bare-`atcr` home view (the root).
var suggestionSources = map[suggestionKey][]string{
	suggestReview:            {"review"},
	suggestReviewInterrupted: {"review"},
	suggestReport:            {"report"},
	suggestReconcile:         {"reconcile"},
	suggestVerify:            {"verify"},
	suggestDebate:            {"debate"},
	suggestStatus:            {"status"},
	suggestHome:              nil,
}

// findCommand resolves path against a fresh tree and fails unless it lands on
// exactly that command: cobra's Find falls back to the nearest parent for an
// unknown name, so the resolved CommandPath is what proves the command exists.
func findCommand(t *testing.T, path []string) *cobra.Command {
	t.Helper()
	cmd, rest, err := NewRootCmd().Find(path)
	require.NoError(t, err, "atcr %s", strings.Join(path, " "))
	require.Empty(t, rest, "atcr %s", strings.Join(path, " "))
	require.Equal(t, strings.Join(append([]string{"atcr"}, path...), " "), cmd.CommandPath(),
		"atcr %s is not a registered command", strings.Join(path, " "))
	return cmd
}

// TestSuggestions_RegistryMatchesCommandTree is the T6 drift guard (AC3): every
// covered command keeps its registry entry, every registry key names a command
// that is still registered, and every suggested command, fixed flag and carried
// flag resolves on the live cobra tree. Renaming or removing a covered command,
// a suggested command or one of their flags, or deleting a registry entry, fails
// here instead of shipping a suggestion that no longer runs.
func TestSuggestions_RegistryMatchesCommandTree(t *testing.T) {
	for key := range suggestionRegistry {
		_, ok := suggestionSources[key]
		assert.True(t, ok, "registry key %s has no source command in suggestionSources", key)
	}
	for key, path := range suggestionSources {
		t.Run(string(key), func(t *testing.T) {
			steps, ok := suggestionRegistry[key]
			require.True(t, ok, "covered command atcr %s lost its registry entry %s", strings.Join(path, " "), key)
			require.NotEmpty(t, steps, "registry entry %s suggests nothing", key)
			source := findCommand(t, path)
			for _, s := range steps {
				target := findCommand(t, strings.Fields(s.Command))
				for _, arg := range s.Args {
					if name, isFlag := strings.CutPrefix(arg, "--"); isFlag {
						assert.NotNil(t, target.Flags().Lookup(name), "%s -> atcr %s: no --%s flag", key, s.Command, name)
					}
				}
				for _, name := range s.Carry {
					assert.NotNil(t, source.Flags().Lookup(name), "%s carries --%s, which %s does not define", key, name, source.CommandPath())
					assert.NotNil(t, target.Flags().Lookup(name), "%s -> atcr %s: carried --%s is not a flag there", key, s.Command, name)
				}
			}
		})
	}
}
