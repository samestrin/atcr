package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/ghaction"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// --- helpers ---------------------------------------------------------------

// autoFixCmd builds a bare command carrying only the auto-fix flags, so a gate
// test can drive validateAutoFixBackend without the whole review command tree.
func autoFixCmd(t *testing.T, repo, token, apiURL string) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "review"}
	addAutoFixFlags(c)
	if repo != "" {
		require.NoError(t, c.Flags().Set("repo", repo))
	}
	if token != "" {
		require.NoError(t, c.Flags().Set("token", token))
	}
	if apiURL != "" {
		require.NoError(t, c.Flags().Set("api-url", apiURL))
	}
	return c
}

// clearGitHubEnv removes the ambient GitHub env vars so a test controls token/
// repo resolution entirely through flags.
func clearGitHubEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_API_URL", "")
}

// writeGoMod drops a minimal go.mod at dir so verify.ResolveValidateCommand
// yields the built-in `go build ./...` default (a present validation command).
func writeGoMod(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o644))
}

// fakeDockerShim writes a POSIX `docker` shim and returns its path. When pass is
// true the shim satisfies Preflight (the `info` subcommand reports a generous host
// so validateHostCaps passes; every other invocation exits 0); when false the
// `info` subcommand exits non-zero so Preflight fails. It mirrors the
// fakeDockerRecording shim in internal/verify/autofix_exec_test.go so the cmd/atcr
// gate tests stay hermetic (no live Docker daemon).
func fakeDockerShim(t *testing.T, pass bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-docker shim is POSIX-only")
	}
	dir := t.TempDir()
	var infoBody string
	if pass {
		infoBody = `  echo '{"MemTotal": 8589934592, "NCPU": 8}'
  exit 0`
	} else {
		infoBody = `  echo 'Cannot connect to the Docker daemon' 1>&2
  exit 1`
	}
	body := "if [ \"$1\" = \"info\" ]; then\n" + infoBody + "\nfi\nexit 0"
	p := filepath.Join(dir, "docker")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755))
	return p
}

// sandboxConfig returns a valid SandboxConfig wired to the fakeDockerShim at
// dockerPath, so a gate test can supply the gate's fourth (sandbox) piece.
func sandboxConfig(dockerPath string) *registry.SandboxConfig {
	return &registry.SandboxConfig{
		Backend:     "docker",
		DockerPath:  dockerPath,
		Image:       "alpine:3.20",
		TestCommand: []string{"go", "test", "./..."},
	}
}

// --- 06-01: flag off by default, discoverable in help ----------------------

// TestReviewCmd_AutoFixFlagExists: `atcr review --help` lists --auto-fix and the
// flag defaults to false when unset (AC 06-01 Scenario 1/3).
func TestReviewCmd_AutoFixFlagExists(t *testing.T) {
	_, help := execCmdCapture(t, "review", "--help")
	require.Contains(t, help, "--auto-fix")

	v, err := newReviewCmd().Flags().GetBool("auto-fix")
	require.NoError(t, err)
	require.False(t, v, "--auto-fix must default to false")
}

// TestReviewCmd_AutoFixHelpMentionsFailOnBypass: the --auto-fix help documents
// that a successful auto-fix run bypasses the --fail-on exit gate, so operators
// using --auto-fix in CI are not surprised when unfixable findings no longer
// fail the build (TD-018).
func TestReviewCmd_AutoFixHelpMentionsFailOnBypass(t *testing.T) {
	_, help := execCmdCapture(t, "review", "--help")
	require.Contains(t, help, "--auto-fix")
	require.Contains(t, help, "--fail-on")
	require.Contains(t, help, "bypassed")
}

// TestReviewCmd_AutoFixAbsentFromOtherCommands: the flag is registered only on
// review, not on unrelated commands (AC 06-01 Scenario 1).
func TestReviewCmd_AutoFixAbsentFromOtherCommands(t *testing.T) {
	for _, c := range newRootCmd().Commands() {
		if c.Name() == "status" {
			_, err := c.Flags().GetBool("auto-fix")
			require.Error(t, err, "--auto-fix must not be registered on `status`")
		}
	}
}

// --- 06-02: gate refuses on any missing/malformed piece --------------------

func TestValidateAutoFixBackend_Refusals(t *testing.T) {
	cases := []struct {
		name     string
		goMod    bool     // write go.mod (present validation command)
		validate []string // configured validate_command
		target   string   // apply_target config value ("" -> default ".")
		repo     string
		token    string
		wantSubs []string
	}{
		{
			name:     "missing validation command",
			goMod:    false,
			target:   ".",
			repo:     "o/r",
			token:    "tok",
			wantSubs: []string{"validation command", "auto_fix.validate_command"},
		},
		{
			name:     "missing github token",
			goMod:    true,
			target:   ".",
			repo:     "o/r",
			token:    "",
			wantSubs: []string{"a GitHub token is required (pass --token or set GITHUB_TOKEN)"},
		},
		{
			name:     "malformed repo slug",
			goMod:    true,
			target:   ".",
			repo:     "notaslug",
			token:    "tok",
			wantSubs: []string{"--repo must be owner/name"},
		},
		{
			name:     "missing apply target",
			goMod:    true,
			target:   "does-not-exist",
			repo:     "o/r",
			token:    "tok",
			wantSubs: []string{"apply target", "auto_fix.apply_target"},
		},
		{
			name:     "all missing aggregates every piece",
			goMod:    false,
			target:   "does-not-exist",
			repo:     "",
			token:    "",
			wantSubs: []string{"validation command", "GitHub token", "apply target"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearGitHubEnv(t)
			root := t.TempDir()
			if tc.goMod {
				writeGoMod(t, root)
			}
			proj := &registry.ProjectConfig{
				Agents:  []string{"a"},
				AutoFix: &registry.AutoFixConfig{ApplyTarget: tc.target, ValidateCommand: tc.validate},
			}
			cmd := autoFixCmd(t, tc.repo, tc.token, "")
			_, err := validateAutoFixBackend(cmd, proj, root)
			require.Error(t, err)
			require.Equal(t, 2, exitCode(err), "gate refusal must be a usage error (exit 2)")
			for _, sub := range tc.wantSubs {
				require.Contains(t, err.Error(), sub)
			}
		})
	}
}

// TestValidateAutoFixBackend_RejectsInsecureAPIURL: a malformed or insecure
// (non-loopback http) --api-url is a fail-closed gate refusal, caught up front by
// shape-checking rather than surfacing lazily at the first HTTP call (TD-014).
func TestValidateAutoFixBackend_RejectsInsecureAPIURL(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "."}}
	cmd := autoFixCmd(t, "o/r", "tok", "http://ghe.internal/api/v3")
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err), "a malformed/insecure api-url is a fail-closed gate refusal")
	require.Contains(t, err.Error(), "API URL")
}

// TestValidateAutoFixBackend_ApplyTargetResolvedAbsolute: runReview passes
// repoRoot="."; the resolved apply target must still be absolute so it is
// independent of the caller's CWD (TD-019).
func TestValidateAutoFixBackend_ApplyTargetResolvedAbsolute(t *testing.T) {
	clearGitHubEnv(t)
	dir := t.TempDir()
	writeGoMod(t, dir)
	t.Chdir(dir)
	proj := &registry.ProjectConfig{
		Agents:  []string{"a"},
		AutoFix: &registry.AutoFixConfig{ApplyTarget: "."},
		Sandbox: sandboxConfig(fakeDockerShim(t, true)),
	}
	cmd := autoFixCmd(t, "o/r", "tok", "")
	be, err := validateAutoFixBackend(cmd, proj, ".")
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(be.applyTarget), "apply target must be absolute regardless of caller CWD")
}

// TestValidateAutoFixBackend_RejectsSubdirApplyTarget: a subdirectory apply_target
// would apply/validate under the subdir but commit repo-root-relative paths GitHub
// never receives, so the gate refuses it until path translation exists
// (autofix.go:274 commit-path-mismatch finding).
func TestValidateAutoFixBackend_RejectsSubdirApplyTarget(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "sub"}}
	cmd := autoFixCmd(t, "o/r", "tok", "")
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err))
	require.Contains(t, err.Error(), "repository root")
}

// TestValidateAutoFixBackend_NoFilesystemMutationOnRefusal: a refused gate leaves
// the apply target untouched — it only stats, never writes (AC 06-02 DoD).
func TestValidateAutoFixBackend_NoFilesystemMutationOnRefusal(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "."}}
	cmd := autoFixCmd(t, "", "", "") // everything missing
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err)
	entries, rerr := os.ReadDir(root)
	require.NoError(t, rerr)
	require.Empty(t, entries, "gate must not create any file in the apply target")
}

// --- 06-03: gate passes silently when fully configured ---------------------

// TestValidateAutoFixBackend_PassesWhenConfigured: all three pieces present ->
// nil error and a populated backend, with flag values winning over env
// (AC 06-03 Scenario 1/3).
func TestValidateAutoFixBackend_PassesWhenConfigured(t *testing.T) {
	clearGitHubEnv(t)
	t.Setenv("GITHUB_TOKEN", "envtok")
	t.Setenv("GITHUB_REPOSITORY", "env/repo")
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{
		Agents:  []string{"a"},
		AutoFix: &registry.AutoFixConfig{ApplyTarget: "."},
		Sandbox: sandboxConfig(fakeDockerShim(t, true)),
	}
	cmd := autoFixCmd(t, "flag/repo", "flagtok", "")

	be, err := validateAutoFixBackend(cmd, proj, root)
	require.NoError(t, err)
	require.Equal(t, "flag", be.owner, "flag --repo must win over env")
	require.Equal(t, "repo", be.repo)
	require.Equal(t, "flagtok", be.token, "flag --token must win over env")
	require.Equal(t, []string{"go", "build", "./..."}, be.validateArgv)
	require.Equal(t, defaultValidationTimeout, be.validateTimeout, "unset timeout resolves to the default")
	require.NotEmpty(t, be.applyTarget)
}

// TestValidateAutoFixBackend_NoNetworkCall: the gate never makes a live GitHub
// call — an api-url pointing at a server that fails the test if hit still passes
// the shape-only gate (AC 06-02 Edge Case 4 / no-network DoD).
func TestValidateAutoFixBackend_NoNetworkCall(t *testing.T) {
	clearGitHubEnv(t)
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{
		Agents:  []string{"a"},
		AutoFix: &registry.AutoFixConfig{ApplyTarget: "."},
		Sandbox: sandboxConfig(fakeDockerShim(t, true)),
	}
	cmd := autoFixCmd(t, "o/r", "tok", srv.URL)

	be, err := validateAutoFixBackend(cmd, proj, root)
	require.NoError(t, err)
	require.Equal(t, srv.URL, be.apiURL)
	require.False(t, hit, "gate must make no network call")
}

// TestValidateAutoFixBackend_ConfiguredCommandWins: an explicit validate_command
// is used even when go.mod is absent (AC 06-02 shape reuse of Story 2).
func TestValidateAutoFixBackend_ConfiguredCommandWins(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir() // no go.mod
	proj := &registry.ProjectConfig{
		Agents:  []string{"a"},
		AutoFix: &registry.AutoFixConfig{ApplyTarget: ".", ValidateCommand: []string{"make", "check"}},
		Sandbox: sandboxConfig(fakeDockerShim(t, true)),
	}
	cmd := autoFixCmd(t, "o/r", "tok", "")
	be, err := validateAutoFixBackend(cmd, proj, root)
	require.NoError(t, err)
	require.Equal(t, []string{"make", "check"}, be.validateArgv)
}

// TestValidateAutoFixBackend_ConfiguredTimeout: an operator-set
// auto_fix.validate_timeout is resolved into the backend (covers the configured
// branch of resolveValidateTimeout).
func TestValidateAutoFixBackend_ConfiguredTimeout(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{
		Agents:  []string{"a"},
		AutoFix: &registry.AutoFixConfig{ApplyTarget: ".", ValidateTimeout: "30s"},
		Sandbox: sandboxConfig(fakeDockerShim(t, true)),
	}
	cmd := autoFixCmd(t, "o/r", "tok", "")
	be, err := validateAutoFixBackend(cmd, proj, root)
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, be.validateTimeout)
}

// TestValidateAutoFixBackend_BadTimeoutRefuses: a malformed configured timeout is
// a fail-closed refusal (defensive; also validated at config load).
func TestValidateAutoFixBackend_BadTimeoutRefuses(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{
		Agents:  []string{"a"},
		AutoFix: &registry.AutoFixConfig{ApplyTarget: ".", ValidateTimeout: "nope"},
	}
	cmd := autoFixCmd(t, "o/r", "tok", "")
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err))
	require.Contains(t, err.Error(), "validate_timeout")
}

// TestOrchestrateAutoFix_EmptyBaseBranch: with no resolvable base branch the
// adapter refuses before reading findings or touching disk.
func TestOrchestrateAutoFix_EmptyBaseBranch(t *testing.T) {
	err := orchestrateAutoFix(context.Background(), io.Discard, autoFixBackend{applyTarget: t.TempDir()}, t.TempDir(), "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "base branch")
}

// TestOrchestrateAutoFix_NoApplicableFix: reconciled findings with no Fix produce
// no entries — the adapter reports "nothing to apply" and returns nil without
// resolving a base commit or constructing a client (covers the select path).
func TestOrchestrateAutoFix_NoApplicableFix(t *testing.T) {
	isolate(t)
	id := verifyFixture(t, "af", []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM", Fix: ""},
	})
	reviewDir := filepath.Join(".atcr", "reviews", id)
	var buf strings.Builder
	err := orchestrateAutoFix(context.Background(), &buf, autoFixBackend{applyTarget: t.TempDir()}, reviewDir, "", "main")
	require.NoError(t, err)
	require.Contains(t, buf.String(), "nothing to apply")
}

// TestOrchestrateAutoFix_AllBelowThresholdMessage: when every fix-carrying finding
// is below the resolved --fail-on threshold, the "nothing to apply" message must
// say so (the default fail_on: HIGH silently drops MEDIUM/LOW fixes — TD-016),
// distinct from "no finding carried a fix".
func TestOrchestrateAutoFix_AllBelowThresholdMessage(t *testing.T) {
	isolate(t)
	id := verifyFixture(t, "belowthresh", []reconcile.JSONFinding{
		{Severity: "LOW", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM", Fix: diffFor("a.go")},
	})
	reviewDir := filepath.Join(".atcr", "reviews", id)
	var buf strings.Builder
	err := orchestrateAutoFix(context.Background(), &buf, autoFixBackend{applyTarget: t.TempDir()}, reviewDir, "HIGH", "main")
	require.NoError(t, err)
	require.Contains(t, buf.String(), "below the --fail-on HIGH threshold",
		"an all-below-threshold run must name the threshold, not report no fixes")
}

// --- runAutoFix sequencing (the Story-4/5 gate: no GitHub before validation) -

// fakeGitHub records calls and lets a test assert the sequencing guarantee.
type fakeGitHub struct {
	branchCalls int
	commitCalls int
	findCalls   int
	createPR    int
	updatePR    int
	// findFound/findNumber let a test drive the create-vs-update decision:
	// the zero value reports "no open PR" (create path); set findFound=true to
	// exercise the update path (AC 05-02).
	findFound  bool
	findNumber int
	// Per-call errors simulate post-CreateBranch failures so the remote-cleanup
	// notice in runAutoFix can be exercised.
	commitErr   error
	findErr     error
	createPRErr error
}

func (f *fakeGitHub) CreateBranch(_ context.Context, _, _, _, _ string) error {
	f.branchCalls++
	return nil
}
func (f *fakeGitHub) CreateCommit(_ context.Context, _, _ string, _ ghaction.CommitRequest) (string, error) {
	f.commitCalls++
	if f.commitErr != nil {
		return "", f.commitErr
	}
	return "deadbeef", nil
}
func (f *fakeGitHub) FindOpenPullRequest(_ context.Context, _, _, _ string) (int, bool, error) {
	f.findCalls++
	if f.findErr != nil {
		return 0, false, f.findErr
	}
	return f.findNumber, f.findFound, nil
}
func (f *fakeGitHub) CreatePullRequest(_ context.Context, _, _ string, _ ghaction.PullRequestRequest) (int, error) {
	f.createPR++
	if f.createPRErr != nil {
		return 0, f.createPRErr
	}
	return 7, nil
}
func (f *fakeGitHub) UpdatePullRequest(_ context.Context, _, _ string, _ int, _ ghaction.PullRequestRequest) error {
	f.updatePR++
	return nil
}

// diffFor builds a minimal modify unified diff turning "old\n" into "new\n" for
// rel, so ApplyPatch has a real hunk to apply.
func diffFor(rel string) string {
	return "--- a/" + rel + "\n+++ b/" + rel + "\n@@ -1 +1 @@\n-old\n+new\n"
}

// TestRunAutoFix_ValidationFailRevertsAndSkipsGitHub: when local validation
// fails, the tree is reverted and ZERO GitHub calls fire (the core AC4/AC5
// sequencing guarantee).
func TestRunAutoFix_ValidationFailRevertsAndSkipsGitHub(t *testing.T) {
	root := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	gh := &fakeGitHub{}
	err := runAutoFix(context.Background(), io.Discard, gh, autoFixRun{
		Backend: autoFixBackend{
			applyTarget:     root,
			validateArgv:    []string{"false"}, // always exits non-zero
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok",
		},
		Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
		BaseSHA: "base", Branch: "atcr/auto-fix",
	})
	require.Error(t, err)
	require.Equal(t, 0, gh.branchCalls+gh.commitCalls+gh.createPR+gh.updatePR, "no GitHub call may fire on validation failure")
	got, rerr := os.ReadFile(filepath.Join(root, rel))
	require.NoError(t, rerr)
	require.Equal(t, "old\n", string(got), "the working tree must be reverted to pre-patch content")
}

// TestRunAutoFix_ValidationPassCreatesPR: on validation success, the flow creates
// a branch/commit and opens a PR (no existing one).
func TestRunAutoFix_ValidationPassCreatesPR(t *testing.T) {
	root := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	gh := &fakeGitHub{}
	err := runAutoFix(context.Background(), io.Discard, gh, autoFixRun{
		Backend: autoFixBackend{
			applyTarget:     root,
			validateArgv:    []string{"true"}, // always exits zero
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok",
		},
		Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
		BaseSHA: "base", Base: "main", Branch: "atcr/auto-fix", Title: "fix", Body: "b", Message: "m",
	})
	require.NoError(t, err)
	require.Equal(t, 1, gh.branchCalls)
	require.Equal(t, 1, gh.commitCalls)
	require.Equal(t, 1, gh.createPR)
	require.Equal(t, 0, gh.updatePR)
	got, _ := os.ReadFile(filepath.Join(root, rel))
	require.Equal(t, "new\n", string(got), "validated content stays applied")
}

// TestRunAutoFix_ValidationPassUpdatesExistingPR: on validation success, when an
// open PR already exists for the branch, runAutoFix UPDATES it and never opens a
// duplicate — the create-vs-update decision (AC 05-02) verified at the
// orchestration seam. The fakeGitHub reports an existing open PR so runAutoFix
// takes the found=true branch (the create-path test above exercises found=false).
func TestRunAutoFix_ValidationPassUpdatesExistingPR(t *testing.T) {
	root := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	gh := &fakeGitHub{findFound: true, findNumber: 17}
	err := runAutoFix(context.Background(), io.Discard, gh, autoFixRun{
		Backend: autoFixBackend{
			applyTarget:     root,
			validateArgv:    []string{"true"}, // always exits zero
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok",
		},
		Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
		BaseSHA: "base", Base: "main", Branch: "atcr/auto-fix", Title: "fix", Body: "b", Message: "m",
	})
	require.NoError(t, err)
	require.Equal(t, 1, gh.findCalls, "the existence check must run")
	require.Equal(t, 1, gh.updatePR, "an existing open PR must be updated")
	require.Equal(t, 0, gh.createPR, "no duplicate PR may be created when one is already open")
	got, _ := os.ReadFile(filepath.Join(root, rel))
	require.Equal(t, "new\n", string(got), "validated content stays applied")
}

// TestRunAutoFix_EmptyBaseRefusesBeforeGitHub: a missing PR base branch must be
// caught AFTER a clean validation but BEFORE any GitHub call, so no orphan
// branch/commit is ever pushed (adversarial 5.2.A HIGH regression guard).
func TestRunAutoFix_EmptyBaseRefusesBeforeGitHub(t *testing.T) {
	root := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	gh := &fakeGitHub{}
	err := runAutoFix(context.Background(), io.Discard, gh, autoFixRun{
		Backend: autoFixBackend{
			applyTarget:     root,
			validateArgv:    []string{"true"}, // validation passes
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok",
		},
		Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
		BaseSHA: "base", Base: "", Branch: "atcr/auto-fix", // no base branch
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "base branch")
	require.Equal(t, 0, gh.branchCalls+gh.commitCalls+gh.createPR+gh.updatePR,
		"no GitHub call may fire when the PR base is unresolved")
}

// TestRunAutoFix_RemoteLeftoverNotice: after CreateBranch succeeds, any failure
// in CreateCommit / FindOpenPullRequest / CreatePullRequest must tell the
// operator that a remote branch (and possibly commit) was left behind and must
// be deleted manually — the local RevertPatch cannot undo remote state.
func TestRunAutoFix_RemoteLeftoverNotice(t *testing.T) {
	cases := []struct {
		name      string
		gh        *fakeGitHub
		wantCalls int
	}{
		{
			name:      "CreateCommit fails",
			gh:        &fakeGitHub{commitErr: errors.New("boom")},
			wantCalls: 2, // branch created, commit attempted and failed
		},
		{
			name:      "FindOpenPullRequest fails",
			gh:        &fakeGitHub{findErr: errors.New("boom")},
			wantCalls: 2, // branch + commit created, find attempted
		},
		{
			name:      "CreatePullRequest fails",
			gh:        &fakeGitHub{createPRErr: errors.New("boom")},
			wantCalls: 2, // branch + commit created, find succeeded, create attempted
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			rel := "f.txt"
			require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

			err := runAutoFix(context.Background(), io.Discard, tc.gh, autoFixRun{
				Backend: autoFixBackend{
					applyTarget:     root,
					validateArgv:    []string{"true"},
					validateTimeout: 5 * time.Second,
					owner:           "o", repo: "r", token: "tok",
				},
				Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
				BaseSHA: "base", Base: "main", Branch: "atcr/auto-fix", Title: "fix", Body: "b", Message: "m",
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "atcr/auto-fix")
			require.Contains(t, err.Error(), "remote branch", "error must warn that a remote branch was created")
			require.Contains(t, err.Error(), "delete", "error must tell the operator to delete the remote branch manually")
			require.Equal(t, tc.wantCalls, tc.gh.branchCalls+tc.gh.commitCalls,
				"branch and optional commit calls must match the failure point")
		})
	}
}

// --- thin finding->entry selection ----------------------------------------

// TestSelectAutoFixEntries_FiltersByThresholdAndFix: only findings carrying a Fix
// at/above the threshold are turned into entries.
func TestSelectAutoFixEntries_FiltersByThresholdAndFix(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.txt", Line: 1, Fix: diffFor("a.txt")},
		{Severity: "LOW", File: "b.txt", Line: 1, Fix: diffFor("b.txt")}, // below threshold
		{Severity: "CRITICAL", File: "c.txt", Line: 1, Fix: ""},          // no fix
	}
	entries, _, err := selectAutoFixEntries(findings, "HIGH")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "a.txt", entries[0].Path)

	// No threshold -> every finding carrying a Fix is included.
	all, _, err := selectAutoFixEntries(findings, "")
	require.NoError(t, err)
	require.Len(t, all, 2)
}

// TestSelectAutoFixEntries_DedupesByPath: two findings whose fixes touch the same
// file yield a single entry for that path — a double backup would clobber the
// original and break RevertPatch (gate 5.5 MEDIUM regression guard).
func TestSelectAutoFixEntries_DedupesByPath(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "same.txt", Line: 1, Fix: diffFor("same.txt")},
		{Severity: "HIGH", File: "same.txt", Line: 9, Fix: diffFor("same.txt")},
	}
	entries, _, err := selectAutoFixEntries(findings, "")
	require.NoError(t, err)
	require.Len(t, entries, 1, "one entry per target path per run")
	require.Equal(t, "same.txt", entries[0].Path)
}

// TestSelectAutoFixEntries_CountsSkippedDuplicates: when a second fix for the
// same path is dropped, the caller receives a count so the operator can be told.
func TestSelectAutoFixEntries_CountsSkippedDuplicates(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "same.txt", Line: 1, Fix: diffFor("same.txt")},
		{Severity: "HIGH", File: "same.txt", Line: 9, Fix: diffFor("same.txt")},
	}
	entries, skipped, err := selectAutoFixEntries(findings, "")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, 1, skipped, "second fix for the same path must be counted as skipped")
}

// TestOrchestrateAutoFix_SkippedDuplicatesNotice: when duplicate-path fixes are
// dropped, orchestrateAutoFix surfaces the count before proceeding.
func TestOrchestrateAutoFix_SkippedDuplicatesNotice(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeGoMod(t, root)
	rel := "same.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	// Stub HEAD resolution so the test does not need a real git repo.
	oldResolve := resolveHeadSHAFn
	resolveHeadSHAFn = func(context.Context, string) (string, error) { return "deadbeef", nil }
	t.Cleanup(func() { resolveHeadSHAFn = oldResolve })

	id := verifyFixture(t, "dup", []reconcile.JSONFinding{
		{Severity: "HIGH", File: "same.txt", Line: 1, Fix: diffFor("same.txt")},
		{Severity: "HIGH", File: "same.txt", Line: 9, Fix: diffFor("same.txt")},
	})

	var buf strings.Builder
	be := autoFixBackend{
		applyTarget:     root,
		validateArgv:    []string{"true"},
		validateTimeout: 5 * time.Second,
		owner:           "o", repo: "r", token: "tok",
	}
	err := orchestrateAutoFix(context.Background(), &buf, be, filepath.Join(".atcr", "reviews", id), "", "main")
	require.Error(t, err) // the live GitHub client fails without network/auth
	require.Contains(t, buf.String(), "skipped", "output must tell the operator that duplicate-path fixes were skipped")
}

// --- 02-03: sandbox resolution is the gate's fourth checked piece -----------

// TestValidateAutoFixBackend_SandboxUnconfiguredJoinsGate: under the default
// sandboxed-on posture, a nil `sandbox:` block is a hard gate refusal even when
// every other piece is valid — the fail-closed behavior change (AC 02-03 EC3 /
// Scenario 2). The refusal is the standard usage error (exit 2), not a new path.
func TestValidateAutoFixBackend_SandboxUnconfiguredJoinsGate(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "."}}
	cmd := autoFixCmd(t, "o/r", "tok", "")
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err), "an unconfigured sandbox is a fail-closed usage error")
	require.Contains(t, err.Error(), "[sandbox] block", "the combined error must name the sandbox failure")
}

// TestValidateAutoFixBackend_SandboxFailureCombinesWithMissingToken: the sandbox
// check joins the SAME `missing []string` slice rather than returning early, so a
// nil sandbox AND a missing token both appear in one combined error (AC 02-03 EC1).
func TestValidateAutoFixBackend_SandboxFailureCombinesWithMissingToken(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "."}}
	cmd := autoFixCmd(t, "o/r", "", "") // token missing
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "[sandbox] block", "sandbox failure must appear in the combined error")
	require.Contains(t, err.Error(), "a GitHub token is required", "token failure must appear in the SAME combined error")
}

// TestValidateAutoFixBackend_SandboxResolvedStoredOnBackend: with all four pieces
// present (including a preflight-passing sandbox), the gate returns nil and stores
// the resolved backend on autoFixBackend.sandboxBackend (AC 02-03 Scenario 1 / EC2).
func TestValidateAutoFixBackend_SandboxResolvedStoredOnBackend(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{
		Agents:  []string{"a"},
		AutoFix: &registry.AutoFixConfig{ApplyTarget: "."},
		Sandbox: sandboxConfig(fakeDockerShim(t, true)),
	}
	cmd := autoFixCmd(t, "o/r", "tok", "")
	be, err := validateAutoFixBackend(cmd, proj, root)
	require.NoError(t, err)
	require.NotNil(t, be.sandboxBackend, "the resolved sandbox backend must be stored on the returned backend")
}

// TestValidateAutoFixBackend_SandboxPreflightFailureJoinsGate: a `sandbox:` block
// whose backend fails Preflight surfaces through the same combined path with the
// resolver's "preflight"-bearing message intact (AC 02-03 Error Scenario 2).
func TestValidateAutoFixBackend_SandboxPreflightFailureJoinsGate(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{
		Agents:  []string{"a"},
		AutoFix: &registry.AutoFixConfig{ApplyTarget: "."},
		Sandbox: sandboxConfig(fakeDockerShim(t, false)),
	}
	cmd := autoFixCmd(t, "o/r", "tok", "")
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err))
	require.Contains(t, err.Error(), "preflight", "a preflight failure must surface through the combined gate error")
}
