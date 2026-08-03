package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddSyncCloudFlags_RegisteredOnReviewAndReconcile covers AC 04-01: both
// subcommands expose --sync-cloud (bool, default false) and --cloud-endpoint
// (string, default = the documented dashboard endpoint).
func TestAddSyncCloudFlags_RegisteredOnReviewAndReconcile(t *testing.T) {
	for _, mk := range []func() *cobra.Command{newReviewCmd, newReconcileCmd} {
		cmd := mk()

		sc := cmd.Flags().Lookup("sync-cloud")
		require.NotNil(t, sc, "sync-cloud must be registered on %q", cmd.Name())
		assert.Equal(t, "bool", sc.Value.Type())
		assert.Equal(t, "false", sc.DefValue)

		ce := cmd.Flags().Lookup("cloud-endpoint")
		require.NotNil(t, ce, "cloud-endpoint must be registered on %q", cmd.Name())
		assert.Equal(t, "string", ce.Value.Type())
		assert.Equal(t, "", ce.DefValue, "there is no default --sync-cloud destination in this build")
	}
}

// TestAddSyncCloudFlags_DefaultEndpointRefuses covers Epic 35.12 AC1d. The premise
// is deliberately inverted from the old warn-and-proceed test: there is no longer a
// default destination to warn ABOUT, so proceeding would either POST a scorecard at
// whatever the placeholder happened to resolve to, or fail in
// ValidateCloudEndpoint with "must be a valid https:// URL" — an error that blames
// the user for a flag they never set. The run must instead refuse, name the absent
// destination as the cause, and exit 2 (usage), matching validateRangeFlags rather
// than the auth path (a missing destination is a configuration fault, not a
// rejected credential).
func TestAddSyncCloudFlags_DefaultEndpointRefuses(t *testing.T) {
	cmd := newReviewCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud"}))
	require.NotNil(t, cmd.PreRunE)

	err := cmd.PreRunE(cmd, nil)
	require.Error(t, err, "--sync-cloud with no destination must refuse, not proceed")
	assert.Equal(t, exitUsage, exitCode(err))
	// Assert on literal text, never on defaultCloudEndpoint: that constant is now
	// the empty string, and every string contains "" — an assertion against it
	// would pass vacuously and stop guarding anything.
	assert.Contains(t, err.Error(), "no default destination")
	assert.Contains(t, err.Error(), "--cloud-endpoint")
}

// TestAddSyncCloudFlags_RefusalPrecedesAPIKeyCheck pins the deliberate ordering
// decision: with BOTH the destination and ATCR_API_KEY absent, the refusal reports
// the missing destination (exit 2) rather than the missing key (exit 3). Supplying
// a key cannot rescue a run that has nowhere to push.
func TestAddSyncCloudFlags_RefusalPrecedesAPIKeyCheck(t *testing.T) {
	t.Setenv("ATCR_API_KEY", "valid-key")
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud"}))
	require.NotNil(t, cmd.PreRunE)

	err := cmd.PreRunE(cmd, nil)
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err), "a present API key must not make an absent destination acceptable")
}

// TestAddSyncCloudFlags_ExplicitEndpointBypassesRefusal pins the other half of
// AC1d: the refusal fires ONLY at the compiled-in default. An explicit
// --cloud-endpoint — including the loopback http form the test suite depends on —
// must pass PreRunE untouched.
func TestAddSyncCloudFlags_ExplicitEndpointBypassesRefusal(t *testing.T) {
	for _, endpoint := range []string{"https://ingest.example.com", "http://127.0.0.1:8080/ingest"} {
		cmd := newReviewCmd()
		require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud", "--cloud-endpoint", endpoint}))
		require.NotNil(t, cmd.PreRunE)
		assert.NoError(t, cmd.PreRunE(cmd, nil), "explicit --cloud-endpoint %q must bypass the refusal", endpoint)
	}
}

// TestAddSyncCloudFlags_PreservesPriorPreRunE covers AC 04-01 EC2: review
// registers addRangeFlags THEN addSyncCloudFlags, and the range-validation
// PreRunE must still fire (chained, not overwritten) — --head without --base is
// still a usage error.
func TestAddSyncCloudFlags_PreservesPriorPreRunE(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--head", "x"}))
	require.NotNil(t, cmd.PreRunE)
	err := cmd.PreRunE(cmd, nil)
	require.Error(t, err, "addRangeFlags PreRunE must survive addSyncCloudFlags")
	assert.Equal(t, exitUsage, exitCode(err))
}

// TestAddSyncCloudFlags_NoWarningWhenSyncCloudUnset pins the negative case for
// the absent-destination refusal: without --sync-cloud the endpoint default is
// irrelevant, so an ordinary run must neither refuse nor complain.
func TestAddSyncCloudFlags_NoWarningWhenSyncCloudUnset(t *testing.T) {
	cmd := newReviewCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	require.NoError(t, cmd.ParseFlags(nil))
	require.NotNil(t, cmd.PreRunE)

	require.NoError(t, cmd.PreRunE(cmd, nil))
	assert.NotContains(t, buf.String(), "no default destination")
}

// TestAddSyncCloudFlags_NoWarningWhenEndpointOverridden pins the second negative
// case: with --sync-cloud set but --cloud-endpoint pointed at a real destination,
// the absent-destination refusal must not fire — it exists only for the
// compiled-in default.
func TestAddSyncCloudFlags_NoWarningWhenEndpointOverridden(t *testing.T) {
	cmd := newReviewCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	require.NoError(t, cmd.ParseFlags([]string{"--sync-cloud", "--cloud-endpoint", "https://ingest.example.com"}))
	require.NotNil(t, cmd.PreRunE)

	require.NoError(t, cmd.PreRunE(cmd, nil))
	assert.NotContains(t, buf.String(), "no default destination")
}

// TestAddQualitySignalFlags_RegistersPreviewWithoutPreRunE pins the resolved TD
// item: addQualitySignalFlags has no --preview precondition of its own, so it
// must register the flag and nothing else — wrapping PreRunE in a pass-through
// closure "for a future precondition" is dead indirection plus a wasted closure
// allocation on every command build. Chaining belongs here only when a real
// precondition appears.
func TestAddQualitySignalFlags_RegistersPreviewWithoutPreRunE(t *testing.T) {
	cmd := &cobra.Command{Use: "probe"}
	addQualitySignalFlags(cmd)

	preview := cmd.Flags().Lookup("preview")
	require.NotNil(t, preview, "--preview must be registered")
	assert.Equal(t, "bool", preview.Value.Type())
	assert.Equal(t, "false", preview.DefValue)

	assert.Nil(t, cmd.PreRunE, "no --preview precondition exists; the helper must not wrap PreRunE")
}

// TestAddQualitySignalFlags_LeavesPriorPreRunEIntact proves dropping the wrapper
// is behavior-preserving at the real call sites (review.go, reconcile.go install
// addQualitySignalFlags last): a hook installed earlier stays in place and still
// fires, now invoked directly by cobra instead of through a pass-through closure.
func TestAddQualitySignalFlags_LeavesPriorPreRunEIntact(t *testing.T) {
	cmd := &cobra.Command{Use: "probe"}
	ran := false
	cmd.PreRunE = func(_ *cobra.Command, _ []string) error {
		ran = true
		return nil
	}
	addQualitySignalFlags(cmd)

	require.NotNil(t, cmd.PreRunE)
	require.NoError(t, cmd.PreRunE(cmd, nil))
	assert.True(t, ran, "the previously-installed hook must still run")
}

// TestAddRangeFlags_ChainOrderPrevFirst pins the chain-order invariant shared
// with addSyncCloudFlags: a previously-installed PreRunE runs BEFORE
// addRangeFlags' own validation (prev-first — hooks run in installation order),
// so a command installing both helpers sees one deterministic order instead of
// the opposite orderings the two helpers used historically.
func TestAddRangeFlags_ChainOrderPrevFirst(t *testing.T) {
	cmd := &cobra.Command{Use: "probe"}
	ran := false
	cmd.PreRunE = func(_ *cobra.Command, _ []string) error {
		ran = true
		return nil
	}
	addRangeFlags(cmd)
	require.NoError(t, cmd.ParseFlags([]string{"--head", "x"})) // invalid: --head requires --base
	require.NotNil(t, cmd.PreRunE)

	err := cmd.PreRunE(cmd, nil)
	require.Error(t, err, "range validation must still fire after the prev hook")
	assert.Equal(t, exitUsage, exitCode(err))
	assert.True(t, ran, "prev hook must run before addRangeFlags' own validation (prev-first invariant)")
}

// --- Sprint 35.0 Story 1: `--all` baseline flag (AC 01-01) ------------------

// TestReviewAllFlag_Registered covers AC 01-01 Story-Specific DoD: `--all` is a
// boolean flag on `atcr review`, defaulting to false (a no-op when unset).
func TestReviewAllFlag_Registered(t *testing.T) {
	cmd := newReviewCmd()
	all := cmd.Flags().Lookup("all")
	require.NotNil(t, all, "--all must be registered on `atcr review`")
	assert.Equal(t, "bool", all.Value.Type())
	assert.Equal(t, "false", all.DefValue)
}

// TestReviewAllFlag_AloneSucceeds covers AC 01-01 Happy Path 1: `--all` with no
// range flags passes PreRunE (no usage error).
func TestReviewAllFlag_AloneSucceeds(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--all"}))
	require.NotNil(t, cmd.PreRunE)
	assert.NoError(t, cmd.PreRunE(cmd, nil))
}

// TestReviewAllFlag_WithUnrelatedFlagsSucceeds covers AC 01-01 Happy Path 2:
// `--all` combined with flags it does not police (--id, --timeout) still passes —
// the validator inspects only --base/--head/--merge-commit.
func TestReviewAllFlag_WithUnrelatedFlagsSucceeds(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--all", "--id", "my-baseline", "--timeout", "300"}))
	require.NotNil(t, cmd.PreRunE)
	assert.NoError(t, cmd.PreRunE(cmd, nil))
}

// TestReviewAllFlag_MutualExclusion covers AC 01-01 Error Scenarios 1-2 and Edge
// Case 1: `--all` combined with any range flag is a usage error (exit 2),
// regardless of whether --base is present.
func TestReviewAllFlag_MutualExclusion(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"all+base", []string{"--all", "--base", "HEAD~1"}},
		{"all+head-no-base", []string{"--all", "--head", "main"}}, // Edge Case 1
		{"all+merge-commit", []string{"--all", "--merge-commit", "abc123"}},
		{"all+base+head", []string{"--all", "--base", "x", "--head", "y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newReviewCmd()
			require.NoError(t, cmd.ParseFlags(tc.args))
			require.NotNil(t, cmd.PreRunE)
			err := cmd.PreRunE(cmd, nil)
			require.Error(t, err, "%v must be rejected", tc.args)
			assert.Equal(t, exitUsage, exitCode(err), "must exit 2 (usageError)")
		})
	}
}

// TestReviewAllFlag_ChangedFalseOnNormalReview covers AC 01-01 Edge Case 2: a
// normal diff-range review leaves Changed("all") false, so the existing
// range-resolution path is unaffected.
func TestReviewAllFlag_ChangedFalseOnNormalReview(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--base", "x", "--head", "y"}))
	assert.False(t, cmd.Flags().Changed("all"), "--all must read as unchanged on a normal review")
}

// TestReviewAllFlag_ExplicitFalseStillChanged covers AC 01-01 Edge Case 4: an
// explicitly-set `--all=false` registers as Changed (the Changed()-presence idiom
// the story mandates), pinning it so a later GetBool short-circuit cannot silently
// change the semantics.
func TestReviewAllFlag_ExplicitFalseStillChanged(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--all=false"}))
	assert.True(t, cmd.Flags().Changed("all"), "--all=false must register as Changed")
	v, _ := cmd.Flags().GetBool("all")
	assert.False(t, v, "--all=false GetBool must be false")
}

// TestReviewAllFlag_WithResumeNoPanic covers AC 01-01 Edge Case 3: `--all`
// alongside `--resume` must not panic in PreRunE; the mutual-exclusion validator
// polices only range flags, so the combination passes flag validation (--resume's
// own precedence is enforced later, in runReview's resume dispatch).
func TestReviewAllFlag_WithResumeNoPanic(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--all", "--resume", "latest"}))
	require.NotNil(t, cmd.PreRunE)
	assert.NotPanics(t, func() {
		_ = cmd.PreRunE(cmd, nil)
	})
}

// TestReviewAllFlag_PreservesPriorPreRunE pins the chaining invariant (mirroring
// TestAddSyncCloudFlags_PreservesPriorPreRunE): the `--all` validator must chain
// prev-first so the range-flag hook still fires — `--head` without `--base`
// remains a usage error even with the baseline validator installed.
func TestReviewAllFlag_PreservesPriorPreRunE(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--head", "x"}))
	require.NotNil(t, cmd.PreRunE)
	err := cmd.PreRunE(cmd, nil)
	require.Error(t, err, "range validation must survive addBaselineFlags")
	assert.Equal(t, exitUsage, exitCode(err))
}

// dirCmd builds a throwaway command with just --dir registered so
// validateDirFlag can be unit-tested independently of the full review command
// wiring. (AC 02-01)
func dirCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "review"}
	cmd.Flags().String("dir", "", "")
	require.NoError(t, cmd.ParseFlags(args))
	return cmd
}

// TestReviewDirFlag_Registered covers AC 02-01 Story-Specific DoD: `--dir` is a
// string flag on `atcr review`, defaulting to "" (unset = not a directory scan).
func TestReviewDirFlag_Registered(t *testing.T) {
	cmd := newReviewCmd()
	f := cmd.Flags().Lookup("dir")
	require.NotNil(t, f, "--dir must be registered on `atcr review`")
	assert.Equal(t, "string", f.Value.Type())
	assert.Equal(t, "", f.DefValue)
}

// TestValidateDirFlag_HappyPaths covers AC 02-01 Happy Path 1-2 and Edge Cases
// 1-4: valid single/nested paths, trailing-slash and leading-`./` normalization,
// `--dir .` accepted as the whole repo root, and an absolute-but-inside-root path
// normalized to its repo-root-relative form.
func TestValidateDirFlag_HappyPaths(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "fanout"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "payload", "testdata"), 0o755))
	cases := []struct {
		name, arg, want string
	}{
		{"single-segment", "internal/fanout", "internal/fanout"},
		{"nested-multi-segment", "internal/payload/testdata", "internal/payload/testdata"},
		{"trailing-slash", "internal/fanout/", "internal/fanout"},
		{"dot-repo-root", ".", "."},
		{"leading-dot-slash", "./internal/fanout", "internal/fanout"},
		{"absolute-inside-root", filepath.Join(root, "internal", "fanout"), "internal/fanout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := dirCmd(t, "--dir", tc.arg)
			scope, err := validateDirFlag(cmd, root)
			require.NoError(t, err)
			assert.Equal(t, tc.want, scope)
		})
	}
}

// TestValidateDirFlag_Unset covers the not-a-dir-scan case: an unsupplied --dir
// yields an empty scope and no error (the whole-repo / non-`--dir` path).
func TestValidateDirFlag_Unset(t *testing.T) {
	root := t.TempDir()
	cmd := dirCmd(t) // --dir not supplied
	scope, err := validateDirFlag(cmd, root)
	require.NoError(t, err)
	assert.Equal(t, "", scope, "unset --dir yields an empty scope")
}

// TestValidateDirFlag_Errors covers AC 02-01 Error Scenarios 1-4: non-existent,
// file-not-directory, outside-root, and empty --dir values each fail with a
// distinct, coded (exit 2) usage error.
func TestValidateDirFlag_Errors(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "file.go"), []byte("x"), 0o644))
	cases := []struct {
		name, arg, wantMsg string
	}{
		{"not-exist", "does/not/exist", `--dir path "does/not/exist" does not exist`},
		{"not-a-directory", "file.go", `--dir path "file.go" is not a directory`},
		{"outside-root-rel", "../other-repo", `--dir path "../other-repo" resolves outside the repository root`},
		{"empty", "", `--dir must not be empty`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := dirCmd(t, "--dir", tc.arg)
			_, err := validateDirFlag(cmd, root)
			require.Error(t, err)
			assert.Equal(t, exitUsage, exitCode(err), "must exit 2 (usageError)")
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

// TestValidateDirFlag_ErrorsBareConvention pins the usageError text convention
// (TD: flags.go): validateDirFlag rejections must be bare messages like their
// siblings validateRangeFlags and outputDirFromFlags — not prefixed with
// "review failed: ", which is reserved for runtime review/range failures.
func TestValidateDirFlag_ErrorsBareConvention(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "file.go"), []byte("x"), 0o644))
	cases := []struct {
		name, arg string
	}{
		{"empty", ""},
		{"not-exist", "does/not/exist"},
		{"not-a-directory", "file.go"},
		{"outside-root-rel", "../other-repo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := dirCmd(t, "--dir", tc.arg)
			_, err := validateDirFlag(cmd, root)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "review failed:",
				"flag-validation usage errors use bare messages; the prefix is for runtime failures")
		})
	}
}

// TestValidateDirFlag_OutsideRootAbsolute covers AC 02-01 Error Scenario 3 via an
// absolute path that resolves outside the repo root (a path-traversal guard).
func TestValidateDirFlag_OutsideRootAbsolute(t *testing.T) {
	root := t.TempDir()
	cmd := dirCmd(t, "--dir", "/etc")
	_, err := validateDirFlag(cmd, root)
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), "resolves outside the repository root")
}

// TestValidateDirFlag_RejectsSymlinkEscape is the 3.2.A HIGH regression: a symlink
// INSIDE the repo root whose real target is OUTSIDE root must be rejected. A purely
// lexical containment guard accepts it (rel == "link", no "../"); the guard must
// symlink-resolve the candidate (os.Stat follows the link) so the escape is caught.
// AC 02-01 Security / Story 2 Risk Analysis (path traversal/symlink escape).
func TestValidateDirFlag_RejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // a directory outside the repo root
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	cmd := dirCmd(t, "--dir", "escape")
	_, err := validateDirFlag(cmd, root)
	require.Error(t, err, "a --dir symlink whose target escapes root must be rejected")
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), "resolves outside the repository root")
}

// --- AC 02-03: --dir / --all mutual exclusion --------------------------------

// TestReviewDirFlag_AloneSucceeds covers AC 02-03 Happy Path 1: `--dir` with no
// range flags and no `--all` passes PreRunE.
func TestReviewDirFlag_AloneSucceeds(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--dir", "internal/fanout"}))
	require.NotNil(t, cmd.PreRunE)
	assert.NoError(t, cmd.PreRunE(cmd, nil))
}

// TestReviewDirFlag_MutualExclusion covers AC 02-03 Error Scenarios 1-3 and Edge
// Case 1: `--dir` combined with any of --base/--head/--merge-commit/--all is a
// usage error (exit 2), fired in PreRunE before any path-value validation.
func TestReviewDirFlag_MutualExclusion(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"dir+base", []string{"--dir", "internal/fanout", "--base", "main"}}, // EC1: --base alone (no --head) still rejected
		{"dir+head", []string{"--dir", "internal/fanout", "--head", "x"}},
		{"dir+merge-commit", []string{"--dir", "internal/fanout", "--merge-commit", "abc123"}},
		{"dir+all", []string{"--dir", "internal/fanout", "--all"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newReviewCmd()
			require.NoError(t, cmd.ParseFlags(tc.args))
			require.NotNil(t, cmd.PreRunE)
			err := cmd.PreRunE(cmd, nil)
			require.Error(t, err, "%v must be rejected", tc.args)
			assert.Equal(t, exitUsage, exitCode(err), "must exit 2 (usageError)")
			assert.Contains(t, err.Error(), "--dir cannot be combined with --base/--head/--merge-commit/--all")
		})
	}
}

// TestReviewDirFlag_ExclusionBeforePathValidation covers AC 02-03 Edge Case 2: the
// mutual-exclusion error (PreRunE) fires before AC 02-01's path-value validation,
// so `--dir does/not/exist --all` yields the exclusion error, not "does not exist".
func TestReviewDirFlag_ExclusionBeforePathValidation(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--dir", "does/not/exist", "--all"}))
	require.NotNil(t, cmd.PreRunE)
	err := cmd.PreRunE(cmd, nil)
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), "--dir cannot be combined with")
	assert.NotContains(t, err.Error(), "does not exist", "mutual exclusion must fire before path validation")
}

// TestReviewAllFlag_BaselineMessageWinsOverRange is the TD-001 fix: when `--all`
// is combined with a partial range flag (`--head` without `--base`), the baseline
// "--all cannot be combined with..." message must win over the range validator's
// "--head requires --base" attribution. The combination was always rejected; this
// pins the correct, less-misleading message once the shared exclusion check owns it.
func TestReviewAllFlag_BaselineMessageWinsOverRange(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--all", "--head", "foo"}))
	require.NotNil(t, cmd.PreRunE)
	err := cmd.PreRunE(cmd, nil)
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), "--all cannot be combined with --base/--head/--merge-commit")
	assert.NotContains(t, err.Error(), "--head requires --base", "baseline message must win when --all is present (TD-001)")
}

// TestValidateDirFlag_AcceptsSymlinkedRootAbsolutePath is the 3.2.A MEDIUM
// regression: when the repo root is reached through a symlink and an absolute --dir
// is supplied in the RESOLVED namespace, containment must still accept it (both
// operands are symlink-resolved before comparison), matching the spec's
// "absolute path INSIDE root" acceptance case on macOS (/var -> /private/var).
func TestValidateDirFlag_AcceptsSymlinkedRootAbsolutePath(t *testing.T) {
	realRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(realRoot, "internal", "fanout"), 0o755))
	// A symlink standing in for the repo root; the CLI's root arg points at the link
	// while the user supplies the resolved absolute path (the cross-namespace case).
	linkRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(realRoot, "internal", "fanout"))
	require.NoError(t, err)
	cmd := dirCmd(t, "--dir", resolved)
	scope, err := validateDirFlag(cmd, linkRoot)
	require.NoError(t, err, "an in-root absolute path in resolved form must be accepted through a symlinked root")
	assert.Equal(t, "internal/fanout", scope)
}
