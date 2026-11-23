package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/gitrange"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSuggestions_RenderEveryRegistryTemplate is the T1 success criterion: every
// registry template renders with the allowlisted flags of the current invocation
// carried forward and <id> left as a literal placeholder, never a concrete id.
func TestSuggestions_RenderEveryRegistryTemplate(t *testing.T) {
	inv := invocation{Flags: map[string]string{"repo-root": "/src/app"}, Pending: true, Reconciled: true}
	want := map[suggestionKey][]string{
		suggestReview: {
			"atcr report <id>",
			"atcr verify <id>",
			"atcr status <id>",
			"atcr review --resume <id>",
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
			"atcr debt list --store=/src/app/.atcr/debt",
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
			// The store path is derived from the carried root, not guessed, so
			// it is the one token allowed to extend /src/app/.
			line = strings.Replace(line, " --store=/src/app/.atcr/debt", "", 1)
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

// TestSuggestions_ReconcileDebtListCarriesStore (Epic 88 T1): reconcile
// --repo-root X persists findings to X/.atcr/debt, so the suggested `debt list`
// names that store; without it, debt list would read the CWD's store instead.
// The derived path is sanitized and quoted like any carried value, and a blank
// --repo-root (which asserts no root) leaves the suggestion bare.
//
// A relative root is made absolute first: debt list refuses a --store
// containing "..", and persistence resolves the root to an absolute path too.
func TestSuggestions_ReconcileDebtListCarriesStore(t *testing.T) {
	other, err := filepath.Abs("../other")
	require.NoError(t, err)
	cases := map[string]string{
		"/src/app": "atcr debt list --store=" + carryValue(filepath.Join("/src/app", ".atcr", "debt")),
		"../other": "atcr debt list --store=" + carryValue(filepath.Join(other, ".atcr", "debt")),
		"/a b":     "atcr debt list --store=" + carryValue(filepath.Join("/a b", ".atcr", "debt")),
		"":         "atcr debt list",
		"  ":       "atcr debt list",
	}
	for root, want := range cases {
		got := renderSuggestions(suggestReconcile, invocation{Flags: map[string]string{"repo-root": root}})
		require.NotEmpty(t, got)
		assert.Equal(t, want, got[0], "--repo-root %q", root)
	}
	assert.Equal(t, "atcr debt list --store='/a b/.atcr/debt'",
		renderSuggestions(suggestReconcile, invocation{Flags: map[string]string{"repo-root": "/a b"}})[0])
}

// TestSuggestions_ResumableOnlyStepDroppedWhenSettled: the status entry offers
// `review --resume <id>` only when the review is pending or failed.
func TestSuggestions_ResumableOnlyStepDroppedWhenSettled(t *testing.T) {
	got := renderSuggestions(suggestStatus, invocation{Reconciled: true})
	assert.Equal(t, []string{"atcr report <id>"}, got)
}

// TestSuggestions_ReconcileStateGatesSteps (EST-8139): report and verify read
// reconciled/findings.json and exit 2 without it, so they are offered only for
// a reconciled review; a settled unreconciled one gets reconcile instead, and a
// failed one only the resume.
func TestSuggestions_ReconcileStateGatesSteps(t *testing.T) {
	cases := []struct {
		name string
		key  suggestionKey
		inv  invocation
		want []string
	}{
		{"review reconciled", suggestReview, invocation{Reconciled: true}, []string{"atcr report <id>", "atcr verify <id>"}},
		{"review unreconciled", suggestReview, invocation{}, []string{"atcr reconcile <id>"}},
		{"status reconciled", suggestStatus, invocation{Reconciled: true}, []string{"atcr report <id>"}},
		{"status unreconciled", suggestStatus, invocation{}, []string{"atcr reconcile <id>"}},
		{"status pending", suggestStatus, invocation{Pending: true}, []string{"atcr review --resume <id>"}},
		{"status failed", suggestStatus, invocation{Failed: true}, []string{"atcr review --resume <id>"}},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, renderSuggestions(c.key, c.inv), c.name)
	}
}

// TestReviewReconciled: only a regular reconciled/findings.json counts; the
// scaffolded reconciled/ directory alone, a directory in the file's place and a
// blank dir do not.
func TestReviewReconciled(t *testing.T) {
	assert.False(t, reviewReconciled(""))
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
	assert.False(t, reviewReconciled(dir), "the scaffolded dir alone")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "reconciled", "findings.json"), 0o755))
	assert.False(t, reviewReconciled(dir), "a directory is not the findings file")
	require.NoError(t, os.Remove(filepath.Join(dir, "reconciled", "findings.json")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reconciled", "findings.json"), []byte("[]"), 0o644))
	assert.True(t, reviewReconciled(dir))
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
			if s.StoreFrom != "" {
				assert.True(t, carryAllowlist[s.StoreFrom], "%s -> %s derives --store from non-allowlisted --%s", key, s.Command, s.StoreFrom)
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
				if s.CarryRange {
					for _, name := range []string{"base", "head", "merge-commit"} {
						assert.NotNil(t, target.Flags().Lookup(name), "%s -> atcr %s: carried range flag --%s is not a flag there", key, s.Command, name)
					}
				}
				if s.StoreFrom != "" {
					assert.NotNil(t, source.Flags().Lookup(s.StoreFrom), "%s derives --store from --%s, which %s does not define", key, s.StoreFrom, source.CommandPath())
					assert.NotNil(t, target.Flags().Lookup("store"), "%s -> atcr %s: derived --store is not a flag there", key, s.Command)
				}
			}
		})
	}
}

// TestSuggestions_ResumeCarriesRecordedRange (EST-8140): a range review resumes
// only at the range it recorded (fanout.ValidateResumeRange), so its resume step
// carries the flags that re-derive that range. An auto-detected range and a
// baseline scan carry none.
func TestSuggestions_ResumeCarriesRecordedRange(t *testing.T) {
	ranged := invocation{Failed: true, ResumeRange: []string{"--base=aaa", "--head=bbb"}}
	assert.Equal(t, []string{"atcr review --resume <id> --base=aaa --head=bbb"}, renderSuggestions(suggestStatus, ranged))
	assert.Equal(t, []string{"atcr status <id>", "atcr review --resume <id> --base=aaa --head=bbb"}, renderSuggestions(suggestReview, ranged))
	assert.Equal(t, []string{"atcr status <id>", "atcr review --resume <id> --base=aaa --head=bbb"}, renderSuggestions(suggestReviewInterrupted, ranged))
	// Only the resume step takes the range.
	assert.Equal(t, []string{"atcr report <id>"}, renderSuggestions(suggestStatus, invocation{Reconciled: true, ResumeRange: ranged.ResumeRange}))
}

// TestRecordedRangeFlags (EST-8140): the flags come from manifest.json's
// detection mode and resolved SHAs.
func TestRecordedRangeFlags(t *testing.T) {
	write := func(m payload.Manifest) string {
		dir := t.TempDir()
		data, err := json.Marshal(m)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644))
		return dir
	}
	assert.Equal(t, []string{"--base=aaa", "--head=bbb"},
		recordedRangeFlags(write(payload.Manifest{Base: "aaa", Head: "bbb", DetectionMode: gitrange.ModeExplicit})))
	assert.Equal(t, []string{"--merge-commit=bbb"},
		recordedRangeFlags(write(payload.Manifest{Base: "aaa", Head: "bbb", DetectionMode: gitrange.ModeMergeCommit})))
	assert.Nil(t, recordedRangeFlags(write(payload.Manifest{Base: "aaa", Head: "bbb", DetectionMode: gitrange.ModeAuto})))
	assert.Nil(t, recordedRangeFlags(write(payload.Manifest{Baseline: true, DetectionMode: gitrange.ModeExplicit})))
	assert.Nil(t, recordedRangeFlags(t.TempDir()), "no manifest")
	assert.Nil(t, recordedRangeFlags(""))
}

// TestInterruptMessage_ResumeCarriesRecordedRange (EST-8140): the interrupt
// notice reads the range from the review's manifest.json, so its suggested
// resume re-derives the range the review recorded instead of exiting 2.
func TestInterruptMessage_ResumeCarriesRecordedRange(t *testing.T) {
	prepIn := func(m *payload.Manifest) *fanout.PreparedReview {
		dir := t.TempDir()
		if m != nil {
			data, err := json.Marshal(m)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644))
		}
		return &fanout.PreparedReview{ID: "rev-7", Dir: dir}
	}
	cases := []struct {
		name   string
		m      *payload.Manifest
		resume string
	}{
		{"explicit range", &payload.Manifest{Base: "aaa", Head: "bbb", DetectionMode: gitrange.ModeExplicit},
			"'atcr review --resume rev-7 --base=aaa --head=bbb'"},
		{"merge commit", &payload.Manifest{Base: "aaa", Head: "bbb", DetectionMode: gitrange.ModeMergeCommit},
			"'atcr review --resume rev-7 --merge-commit=bbb'"},
		{"auto range", &payload.Manifest{Base: "aaa", Head: "bbb", DetectionMode: gitrange.ModeAuto},
			"'atcr review --resume rev-7' to finish"},
		{"no manifest", nil, "'atcr review --resume rev-7' to finish"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := interruptMessage(nil, prepIn(tc.m))
			assert.Contains(t, msg, "Run 'atcr status rev-7' to inspect, or "+tc.resume)
		})
	}
}

// TestSuggestions_HomeOffersReportOnlyWhenReconciled (EST-8140): report exits 2
// without reconciled data, so a first run, an unreadable pointer and an
// unreconciled latest review get only `atcr review`.
func TestSuggestions_HomeOffersReportOnlyWhenReconciled(t *testing.T) {
	assert.Equal(t, []string{"atcr review"}, renderSuggestions(suggestHome, invocation{}))
	assert.Equal(t, []string{"atcr review", "atcr report <id>"}, renderSuggestions(suggestHome, invocation{Reconciled: true}))
}
