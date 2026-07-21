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
	"github.com/samestrin/atcr/internal/sandbox"
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

// spyResolveSandbox installs a counting stand-in for the sandbox resolver and
// returns a pointer to its invocation count, restoring the real resolver on
// cleanup. It lets a --no-sandbox test PROVE zero invocations (AC 03-02), which a
// resolver that merely no-ops on enabled==false could not demonstrate. The stub
// returns (nil, nil) so a counted call does not itself fail the gate.
func spyResolveSandbox(t *testing.T) *int {
	t.Helper()
	calls := 0
	orig := resolveAutoFixSandboxFn
	resolveAutoFixSandboxFn = func(context.Context, bool, *registry.SandboxConfig) (sandbox.Backend, error) {
		calls++
		return nil, nil
	}
	t.Cleanup(func() { resolveAutoFixSandboxFn = orig })
	return &calls
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

// TestValidateAutoFixBackend_SandboxSkippedWhenGateAlreadyFailing: the sandbox
// resolver/Preflight (gate check 4) shells out to docker and spawns a throwaway
// container, so it must be skipped when the cheap local checks (1-3) already
// failed — a half-configured run refuses on the missing piece without paying
// the container cost.
func TestValidateAutoFixBackend_SandboxSkippedWhenGateAlreadyFailing(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	calls := spyResolveSandbox(t)
	proj := &registry.ProjectConfig{
		Agents:  []string{"a"},
		AutoFix: &registry.AutoFixConfig{ApplyTarget: "."},
		Sandbox: sandboxConfig(fakeDockerShim(t, true)),
	}
	cmd := autoFixCmd(t, "o/r", "", "") // repo present, token missing -> check (3) fails
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "GitHub token")
	require.Equal(t, 0, *calls, "sandbox resolver must not run when an earlier gate check already failed")
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
			noSandbox: true, // explicit --no-sandbox opt-out (host path)
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
			noSandbox: true, // explicit --no-sandbox opt-out (host path)
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
			noSandbox: true, // explicit --no-sandbox opt-out (host path)
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
			noSandbox: true, // explicit --no-sandbox opt-out (host path)
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
					noSandbox: true, // explicit --no-sandbox opt-out (host path)
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

// --- 01-03: zero behavior change to runAutoFix, validation routed through a
// supplied sandbox.Backend --------------------------------------------------

// fakeSandboxBackend is a sandbox.Backend stand-in for runAutoFix pipeline tests:
// it records that Run was invoked (and the RunSpec it received, so a test can
// assert the runAutoFix→adapter forwarding of argv/timeout/apply-target) and
// returns a preconfigured RunResult/error so a test can drive the sandbox-routed
// validation path without a live container (AC 01-03).
type fakeSandboxBackend struct {
	runCalls int
	gotSpec  sandbox.RunSpec
	result   sandbox.RunResult
	runErr   error
}

func (f *fakeSandboxBackend) Name() string                    { return "fake-sandbox" }
func (f *fakeSandboxBackend) Preflight(context.Context) error { return nil }
func (f *fakeSandboxBackend) Run(_ context.Context, spec sandbox.RunSpec) (sandbox.RunResult, error) {
	f.runCalls++
	f.gotSpec = spec
	return f.result, f.runErr
}

// TestRunAutoFix_SandboxPassDrivesIdenticalPRSequence: when a sandbox.Backend is
// supplied and reports a passing result, runAutoFix drives the identical
// ApplyPatch → CleanupBackups → CreateBranch → CreateCommit → FindOpenPullRequest →
// CreatePullRequest sequence as the host-exec pass case (AC 01-03 Scenario 1). The
// validateArgv is `false` (would FAIL on the host path), so a green result here can
// only come from routing through the fake backend — proving the dispatch, not the
// host os/exec fallback.
func TestRunAutoFix_SandboxPassDrivesIdenticalPRSequence(t *testing.T) {
	root := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	be := &fakeSandboxBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
	gh := &fakeGitHub{}
	err := runAutoFix(context.Background(), io.Discard, gh, autoFixRun{
		Backend: autoFixBackend{
			applyTarget:     root,
			validateArgv:    []string{"false"}, // would FAIL on the host path
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok",
			sandboxBackend: be,
		},
		Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
		BaseSHA: "base", Base: "main", Branch: "atcr/auto-fix", Title: "fix", Body: "b", Message: "m",
	})
	require.NoError(t, err)
	require.Equal(t, 1, be.runCalls, "validation must route through the supplied sandbox backend, not the host path")
	require.Equal(t, []string{"false"}, be.gotSpec.Command,
		"runAutoFix must forward validateArgv into the RunSpec unchanged")
	require.Equal(t, 5*time.Second, be.gotSpec.Timeout,
		"runAutoFix must forward validateTimeout into the RunSpec unchanged")
	require.Equal(t, root, be.gotSpec.SnapshotDir,
		"runAutoFix must forward the apply target as the RunSpec snapshot dir")
	require.Equal(t, 1, gh.branchCalls)
	require.Equal(t, 1, gh.commitCalls)
	require.Equal(t, 1, gh.createPR)
	require.Equal(t, 0, gh.updatePR)
	got, _ := os.ReadFile(filepath.Join(root, rel))
	require.Equal(t, "new\n", string(got), "validated content stays applied on a sandbox pass")
}

// TestRunAutoFix_SandboxFailRevertsWithIdenticalWording: a failing sandbox result
// (non-zero exit) reverts the tree, fires ZERO GitHub calls, and returns the exact
// existing "local validation failed (exit %d)" wording (AC 01-03 Scenario 2). The
// validateArgv is `true` (would PASS on the host path), so a revert here can only
// come from the sandbox result.
func TestRunAutoFix_SandboxFailRevertsWithIdenticalWording(t *testing.T) {
	root := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	be := &fakeSandboxBackend{result: sandbox.RunResult{ExitCode: 3, Output: "boom"}}
	gh := &fakeGitHub{}
	err := runAutoFix(context.Background(), io.Discard, gh, autoFixRun{
		Backend: autoFixBackend{
			applyTarget:     root,
			validateArgv:    []string{"true"}, // would PASS on the host path
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok",
			sandboxBackend: be,
		},
		Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
		BaseSHA: "base", Base: "main", Branch: "atcr/auto-fix", Title: "fix", Body: "b", Message: "m",
	})
	require.Error(t, err)
	require.Equal(t, 1, be.runCalls, "validation must route through the sandbox backend")
	require.Contains(t, err.Error(), "local validation failed (exit 3)",
		"the sandbox-fail branch must reuse the exact host-path failure wording with the sandbox exit code")
	require.Equal(t, 0, gh.branchCalls+gh.commitCalls+gh.createPR+gh.updatePR,
		"no GitHub call may fire on a sandbox validation failure")
	got, _ := os.ReadFile(filepath.Join(root, rel))
	require.Equal(t, "old\n", string(got), "the working tree must be reverted after a sandbox validation failure")
}

// TestRunAutoFix_SandboxFailSurfacesCapturedOutput: a failing validation must not
// discard its captured output — the sandbox path has already collapsed
// stdout+stderr into res.Stdout, so the failure error must carry a bounded tail of
// it, or the operator gets zero diagnostic bytes about WHY the fix was rejected.
func TestRunAutoFix_SandboxFailSurfacesCapturedOutput(t *testing.T) {
	root := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	be := &fakeSandboxBackend{result: sandbox.RunResult{ExitCode: 3, Output: "boom: compile error at x.go:3"}}
	gh := &fakeGitHub{}
	err := runAutoFix(context.Background(), io.Discard, gh, autoFixRun{
		Backend: autoFixBackend{
			applyTarget:     root,
			validateArgv:    []string{"true"}, // would PASS on the host path
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok",
			sandboxBackend: be,
		},
		Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
		BaseSHA: "base", Base: "main", Branch: "atcr/auto-fix", Title: "fix", Body: "b", Message: "m",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "local validation failed (exit 3)")
	require.Contains(t, err.Error(), "boom: compile error at x.go:3",
		"the failure error must surface the validation run's captured output")
}

// TestRunAutoFix_SandboxTimeoutReportsTimeoutNotExitZero: on a validation timeout
// the translated result leaves ExitCode at 0 and sets TimedOut, so the failure
// branch must say so — "local validation failed (exit 0)" is a nonsensical message
// that hides the timeout from the operator.
func TestRunAutoFix_SandboxTimeoutReportsTimeoutNotExitZero(t *testing.T) {
	root := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	be := &fakeSandboxBackend{result: sandbox.RunResult{Output: "partial before deadline", TimedOut: true}}
	gh := &fakeGitHub{}
	err := runAutoFix(context.Background(), io.Discard, gh, autoFixRun{
		Backend: autoFixBackend{
			applyTarget:     root,
			validateArgv:    []string{"true"}, // would PASS on the host path
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok",
			sandboxBackend: be,
		},
		Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
		BaseSHA: "base", Base: "main", Branch: "atcr/auto-fix", Title: "fix", Body: "b", Message: "m",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "timed out", "a timed-out validation must surface AS a timeout")
	require.NotContains(t, err.Error(), "exit 0", "reporting exit 0 for a timeout is nonsensical and misleading")
	require.Contains(t, err.Error(), "partial before deadline",
		"the partial output captured before the deadline must be retained in the error")
	require.Equal(t, 0, gh.branchCalls+gh.commitCalls+gh.createPR+gh.updatePR,
		"no GitHub call may fire on a validation timeout")
}

// TestRunAutoFix_SandboxStartErrorRevertsWithCannotValidateWording: a backend fault
// (Run returns a non-nil error → StartError per AC 01-02) takes the same
// "cannot run validation" branch as a host-path StartError, reverting the tree with
// zero GitHub calls (AC 01-03 Scenario 3). validateArgv is `true` so the host path
// would have passed.
func TestRunAutoFix_SandboxStartErrorRevertsWithCannotValidateWording(t *testing.T) {
	root := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	be := &fakeSandboxBackend{runErr: errors.New("docker daemon unreachable")}
	gh := &fakeGitHub{}
	err := runAutoFix(context.Background(), io.Discard, gh, autoFixRun{
		Backend: autoFixBackend{
			applyTarget:     root,
			validateArgv:    []string{"true"}, // would PASS on the host path
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok",
			sandboxBackend: be,
		},
		Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
		BaseSHA: "base", Base: "main", Branch: "atcr/auto-fix", Title: "fix", Body: "b", Message: "m",
	})
	require.Error(t, err)
	require.Equal(t, 1, be.runCalls, "validation must route through the sandbox backend")
	require.Contains(t, err.Error(), "cannot run validation",
		"a sandbox backend fault must take the same cannot-validate branch as a host StartError")
	require.Equal(t, 0, gh.branchCalls+gh.commitCalls+gh.createPR+gh.updatePR,
		"no GitHub call may fire when the sandbox cannot validate")
	got, _ := os.ReadFile(filepath.Join(root, rel))
	require.Equal(t, "old\n", string(got), "the working tree must be reverted when the sandbox cannot validate")
}

// TestRunAutoFix_NilSandboxUsesHostPath: with no sandbox backend BUT the explicit
// --no-sandbox opt-out (noSandbox=true), validation still runs on the host exactly
// as before. This supersedes AC 01-03's original nil→host zero-behavior baseline:
// under epic 32.2 Task 1 a nil backend is no longer sufficient to reach the host
// path — the opt-out flag is now required, and its absence fails closed (proven by
// TestRunAutoFix_NilSandboxWithoutOptOutRefuses). The host path itself is otherwise
// byte-identical when the opt-out IS present.
func TestRunAutoFix_NilSandboxUsesHostPath(t *testing.T) {
	root := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	gh := &fakeGitHub{}
	err := runAutoFix(context.Background(), io.Discard, gh, autoFixRun{
		Backend: autoFixBackend{
			applyTarget:     root,
			validateArgv:    []string{"true"}, // host path passes
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok",
			sandboxBackend: nil,  // no sandbox resolved
			noSandbox:      true, // explicit --no-sandbox opt-out authorizes the host path
		},
		Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
		BaseSHA: "base", Base: "main", Branch: "atcr/auto-fix", Title: "fix", Body: "b", Message: "m",
	})
	require.NoError(t, err)
	require.Equal(t, 1, gh.createPR, "the host path stays byte-identical when no sandbox backend is supplied")
}

// TestRunAutoFix_NilSandboxWithoutOptOutRefuses: a backend with NO sandbox and NO
// explicit --no-sandbox opt-out (noSandbox=false) must fail closed — refuse before
// any host validation or GitHub call and revert the applied patch — rather than
// silently running unsandboxed on the host (epic 32.2 Task 1, AC1). validateArgv is
// `true`, which WOULD pass on the host path, so a green PR here would prove a silent
// host fallback; the required error proves the fail-closed guard fired instead.
func TestRunAutoFix_NilSandboxWithoutOptOutRefuses(t *testing.T) {
	root := t.TempDir()
	rel := "f.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644))

	gh := &fakeGitHub{}
	err := runAutoFix(context.Background(), io.Discard, gh, autoFixRun{
		Backend: autoFixBackend{
			applyTarget:     root,
			validateArgv:    []string{"true"}, // WOULD pass on the host path
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok",
			sandboxBackend: nil,   // no sandbox resolved
			noSandbox:      false, // and no explicit opt-out
		},
		Entries: []payload.FileEntry{{Path: rel, Body: diffFor(rel)}},
		BaseSHA: "base", Base: "main", Branch: "atcr/auto-fix", Title: "fix", Body: "b", Message: "m",
	})
	require.Error(t, err, "a nil sandbox backend without --no-sandbox must refuse, not silently run on the host")
	require.Equal(t, 0, gh.branchCalls+gh.commitCalls+gh.createPR+gh.updatePR,
		"no GitHub call may fire on a fail-closed refusal")
	got, _ := os.ReadFile(filepath.Join(root, rel))
	require.Equal(t, "old\n", string(got), "the applied patch must be reverted on a fail-closed refusal")
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

	// Stub the GitHub client seam so the run is hermetic: the pre-stub version's
	// require.Error was satisfied by an incidental network/auth failure unrelated
	// to the duplicate-skip behavior under test.
	gh := &fakeGitHub{}
	oldClient := newAutoFixGitHubFn
	newAutoFixGitHubFn = func(string, string) autoFixGitHub { return gh }
	t.Cleanup(func() { newAutoFixGitHubFn = oldClient })

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
		noSandbox: true, // explicit --no-sandbox opt-out (host path)
	}
	err := orchestrateAutoFix(context.Background(), &buf, be, filepath.Join(".atcr", "reviews", id), "", "main")
	require.NoError(t, err, "orchestration must succeed hermetically — a live network/auth failure must not be load-bearing")
	require.Contains(t, buf.String(), "skipped", "output must tell the operator that duplicate-path fixes were skipped")
	require.Equal(t, 1, gh.createPR, "the run must drive the full PR sequence against the stubbed client")
}

// --- 03-01: --no-sandbox flag registration and help text -------------------

// TestNoSandboxFlag_Registered: --no-sandbox is a bool flag defaulting to false,
// present and readable even before any parsing (AC 03-01 Scenario 1/2).
func TestNoSandboxFlag_Registered(t *testing.T) {
	c := &cobra.Command{Use: "review"}
	addAutoFixFlags(c)

	f := c.Flags().Lookup("no-sandbox")
	require.NotNil(t, f, "--no-sandbox must be registered by addAutoFixFlags")
	require.Equal(t, "bool", f.Value.Type(), "--no-sandbox must be a bool flag")
	require.Equal(t, "false", f.DefValue, "--no-sandbox must default to false")

	v, err := c.Flags().GetBool("no-sandbox")
	require.NoError(t, err)
	require.False(t, v, "--no-sandbox must read false when unset (present, not merely absent)")
}

// TestNoSandboxFlag_ParsesTrue: passing --no-sandbox on the command line sets it
// to true (AC 03-01 Scenario 3).
func TestNoSandboxFlag_ParsesTrue(t *testing.T) {
	c := &cobra.Command{Use: "review"}
	addAutoFixFlags(c)
	require.NoError(t, c.Flags().Parse([]string{"--auto-fix", "--no-sandbox"}))
	v, err := c.Flags().GetBool("no-sandbox")
	require.NoError(t, err)
	require.True(t, v)
}

// TestNoSandboxFlag_ParsesWithoutAutoFix: the flag parses cleanly even without
// --auto-fix (cobra never rejects an unused bool); its no-op-without-auto-fix
// behavior is enforced at the call site, not by registration (AC 03-01 EC1).
func TestNoSandboxFlag_ParsesWithoutAutoFix(t *testing.T) {
	c := &cobra.Command{Use: "review"}
	addAutoFixFlags(c)
	require.NoError(t, c.Flags().Parse([]string{"--no-sandbox"}))
	v, err := c.Flags().GetBool("no-sandbox")
	require.NoError(t, err)
	require.True(t, v)
}

// TestNoSandboxFlag_HelpNamesTheRisk: the help text is a security control — it
// must plainly state that the flag disables container isolation and runs
// LLM-generated validation directly on the host (AC 03-01 EC2 / Security).
func TestNoSandboxFlag_HelpNamesTheRisk(t *testing.T) {
	c := &cobra.Command{Use: "review"}
	addAutoFixFlags(c)
	usage := c.Flags().Lookup("no-sandbox").Usage
	require.NotEmpty(t, usage)
	for _, want := range []string{"container isolation", "host", "LLM-generated"} {
		require.Contains(t, usage, want,
			"--no-sandbox help must name the risk (disables container isolation, runs LLM-generated code on the host)")
	}
	// The escalation framing carries the severity, not just the neutral nouns — pin
	// it so the help cannot be silently weakened to understate the risk (3.5.A LOW).
	for _, want := range []string{"DANGER", "privileges", "no isolation"} {
		require.Contains(t, usage, want,
			"--no-sandbox help must keep its severity framing (DANGER / your privileges / no isolation)")
	}
}

// TestReviewCmd_NoSandboxFlagRegisteredOnReview: guards the actual wiring —
// deleting addAutoFixFlags(cmd) from newReviewCmd would leave the real `review`
// command with no --no-sandbox flag while the synthetic-command tests still passed
// (3.5.A MEDIUM). Mirrors TestReviewCmd_AutoFixFlagExists.
func TestReviewCmd_NoSandboxFlagRegisteredOnReview(t *testing.T) {
	_, help := execCmdCapture(t, "review", "--help")
	require.Contains(t, help, "--no-sandbox")

	v, err := newReviewCmd().Flags().GetBool("no-sandbox")
	require.NoError(t, err)
	require.False(t, v, "--no-sandbox must default to false on the real review command")
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

// TestValidateAutoFixBackend_SandboxSkippedWhenTokenMissing: a missing token fails
// one of the cheap local checks (1-3), so the sandbox check — the expensive fourth
// piece that shells out to docker and spawns a throwaway container — is skipped
// entirely. The combined error names the token failure and NOT the sandbox piece.
func TestValidateAutoFixBackend_SandboxSkippedWhenTokenMissing(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "."}}
	cmd := autoFixCmd(t, "o/r", "", "") // token missing
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "a GitHub token is required", "token failure must appear in the combined error")
	require.NotContains(t, err.Error(), "[sandbox] block", "sandbox check is skipped when an earlier gate check already failed")
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

// TestValidateAutoFixBackend_SandboxUsesSuppliedContext: when the command carries a
// real context (as it always does in production via ExecuteContext), the gate
// threads THAT context into the sandbox resolver rather than falling back to the
// nil-guard's context.Background(). A cancelled context makes Preflight fail, and
// that failure surfaces through the combined gate error — proving the supplied
// context reaches ResolveAutoFixSandbox.
func TestValidateAutoFixBackend_SandboxUsesSuppliedContext(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{
		Agents:  []string{"a"},
		AutoFix: &registry.AutoFixConfig{ApplyTarget: "."},
		Sandbox: sandboxConfig(fakeDockerShim(t, true)), // would pass Preflight with a live ctx
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled: Preflight's docker subprocess must abort
	cmd := autoFixCmd(t, "o/r", "tok", "")
	cmd.SetContext(ctx)
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err, "a cancelled supplied context must make sandbox Preflight fail the gate")
	require.Equal(t, 2, exitCode(err))
	require.Contains(t, err.Error(), "sandbox", "the cancelled-context preflight failure must surface as the sandbox gate piece")
}

// TestValidateAutoFixBackend_CheapPiecesCombineInOneError: when every cheap local
// gate piece fails simultaneously (missing apply target, unresolvable validation
// command, missing GitHub token), the single returned usage error names ALL THREE
// in one combined message (exit 2). The sandbox check — the expensive fourth piece
// that shells out to docker — is gated on those cheap checks passing, so it
// contributes nothing to a gate that already refuses.
func TestValidateAutoFixBackend_CheapPiecesCombineInOneError(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir() // deliberately NO go.mod → validation command cannot resolve
	proj := &registry.ProjectConfig{
		Agents:  []string{"a"},
		AutoFix: &registry.AutoFixConfig{ApplyTarget: "does-not-exist"}, // apply target stat-fails
		Sandbox: nil,                                                    // unconfigured sandbox
	}
	cmd := autoFixCmd(t, "", "", "") // no repo, no token
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err), "an all-pieces-missing gate failure is a usage error (exit 2)")
	msg := err.Error()
	require.Contains(t, msg, "apply target", "the combined error must name the apply-target failure")
	require.Contains(t, msg, "validation command", "the combined error must name the validation-command failure")
	require.Contains(t, msg, "a GitHub token is required", "the combined error must name the GitHub-credential failure")
	require.NotContains(t, msg, "[sandbox] block", "sandbox check is skipped when the cheap checks already failed")
}

// --- 03-02: --no-sandbox bypasses the resolver/preflight gate ---------------

// noSandboxCmd builds a bare auto-fix command with --no-sandbox already set, so a
// bypass test drives validateAutoFixBackend on the opt-out path.
func noSandboxCmd(t *testing.T, repo, token string) *cobra.Command {
	t.Helper()
	c := autoFixCmd(t, repo, token, "")
	require.NoError(t, c.Flags().Set("no-sandbox", "true"))
	return c
}

// TestValidateAutoFixBackend_NoSandboxSkipsResolver: with --no-sandbox set and NO
// sandbox config and no Docker, the resolver is never called, no sandbox entry is
// appended to missing, and the gate passes with a nil sandboxBackend (AC 03-02
// Scenario 1). Proven by a zero-invocation spy, not merely "no docker error".
func TestValidateAutoFixBackend_NoSandboxSkipsResolver(t *testing.T) {
	clearGitHubEnv(t)
	calls := spyResolveSandbox(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "."}}
	cmd := noSandboxCmd(t, "o/r", "tok")
	be, err := validateAutoFixBackend(cmd, proj, root)
	require.NoError(t, err, "--no-sandbox must let the gate pass without a sandbox config")
	require.Equal(t, 0, *calls, "the sandbox resolver must be called ZERO times under --no-sandbox")
	require.Nil(t, be.sandboxBackend, "no sandbox backend is resolved on the --no-sandbox path")
	require.True(t, be.noSandbox, "the gate must record the explicit opt-out so runAutoFix authorizes the host path (epic 32.2 Task 1); a nil backend without it fails closed")
}

// TestValidateAutoFixBackend_NoSandboxSkipsEvenWithValidConfig: a working
// sandbox config does not override an explicit --no-sandbox — the operator's
// choice wins and the resolver is still never called (AC 03-02 Scenario 3).
func TestValidateAutoFixBackend_NoSandboxSkipsEvenWithValidConfig(t *testing.T) {
	clearGitHubEnv(t)
	calls := spyResolveSandbox(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{
		Agents:  []string{"a"},
		AutoFix: &registry.AutoFixConfig{ApplyTarget: "."},
		Sandbox: sandboxConfig(fakeDockerShim(t, true)),
	}
	cmd := noSandboxCmd(t, "o/r", "tok")
	be, err := validateAutoFixBackend(cmd, proj, root)
	require.NoError(t, err)
	require.Equal(t, 0, *calls, "a valid sandbox config must not override an explicit --no-sandbox")
	require.Nil(t, be.sandboxBackend)
}

// TestValidateAutoFixBackend_NoSandboxStillEnforcesOtherPieces: the bypass is
// scoped to sandbox resolution only — a missing token still fails closed, and the
// error names the token failure but NOT any sandbox failure (AC 03-02 Security).
func TestValidateAutoFixBackend_NoSandboxStillEnforcesOtherPieces(t *testing.T) {
	clearGitHubEnv(t)
	calls := spyResolveSandbox(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "."}}
	cmd := noSandboxCmd(t, "o/r", "") // token missing
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err))
	require.Contains(t, err.Error(), "a GitHub token is required", "other gate pieces stay enforced under --no-sandbox")
	require.NotContains(t, err.Error(), "[sandbox] block", "no sandbox failure may appear on the bypass path")
	require.Equal(t, 0, *calls)
}

// TestValidateAutoFixBackend_NoSandboxOtherPiecesFailClosed: the bypass touches
// sandbox resolution ONLY — a missing apply target and a missing validation
// command each still fail closed under --no-sandbox, with the resolver never
// called (AC 03-02 Security / Story-Specific, all three non-sandbox pieces).
func TestValidateAutoFixBackend_NoSandboxOtherPiecesFailClosed(t *testing.T) {
	cases := []struct {
		name    string
		goMod   bool
		target  string
		wantSub string
	}{
		{name: "missing apply target", goMod: true, target: "does-not-exist", wantSub: "apply target"},
		{name: "missing validation command", goMod: false, target: ".", wantSub: "validation command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearGitHubEnv(t)
			calls := spyResolveSandbox(t)
			root := t.TempDir()
			if tc.goMod {
				writeGoMod(t, root)
			}
			proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: tc.target}}
			cmd := noSandboxCmd(t, "o/r", "tok")
			_, err := validateAutoFixBackend(cmd, proj, root)
			require.Error(t, err)
			require.Equal(t, 2, exitCode(err))
			require.Contains(t, err.Error(), tc.wantSub, "non-sandbox gate pieces must still fail closed under --no-sandbox")
			require.NotContains(t, err.Error(), "[sandbox] block", "the sandbox piece is bypassed, so it must not appear")
			require.Equal(t, 0, *calls, "the resolver must not be called on the bypass path")
		})
	}
}

// --- 03-03: every-run (non-memoized) stderr security warning ----------------

const noSandboxWarnMarker = "WITHOUT container isolation"

// runNoSandboxGate drives validateAutoFixBackend once on the --no-sandbox path
// against a fresh stderr buffer and returns what was written to stderr. Other
// pieces are valid so the gate passes; the warning must fire regardless.
func runNoSandboxGate(t *testing.T) string {
	t.Helper()
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "."}}
	cmd := noSandboxCmd(t, "o/r", "tok")
	var errBuf, outBuf strings.Builder
	cmd.SetErr(&errBuf)
	cmd.SetOut(&outBuf)
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.NoError(t, err, "the --no-sandbox gate should pass with all other pieces valid")
	require.NotContains(t, outBuf.String(), noSandboxWarnMarker, "the warning must go to stderr, never stdout")
	return errBuf.String()
}

// TestWarnNoSandbox_PrintsOnEveryConsecutiveCall: the warning fires on the first
// AND every subsequent in-process invocation — the load-bearing non-memoization
// guard. A sync.Once/package-bool implementation would fail the 2nd/3rd call
// (AC 03-03 Scenario 2 / Error Scenario 1). Exercised through the real call site.
func TestWarnNoSandbox_PrintsOnEveryConsecutiveCall(t *testing.T) {
	for i := 1; i <= 3; i++ {
		got := runNoSandboxGate(t)
		require.Contains(t, got, noSandboxWarnMarker,
			"the --no-sandbox warning must print on consecutive call #%d (no memoization)", i)
	}
}

// TestWarnNoSandbox_PrintsExactlyOncePerCallToSharedWriter: three consecutive
// gate calls writing to ONE shared stderr buffer must yield exactly three warning
// occurrences — occurrence count, not mere presence, is what a sync.Once or
// package-level "seen" bool would fail (it would show 1). Complements the
// fresh-buffer presence test (AC 03-03 Error Scenario 1).
func TestWarnNoSandbox_PrintsExactlyOncePerCallToSharedWriter(t *testing.T) {
	clearGitHubEnv(t)
	var shared strings.Builder
	for i := 0; i < 3; i++ {
		root := t.TempDir()
		writeGoMod(t, root)
		proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "."}}
		cmd := noSandboxCmd(t, "o/r", "tok")
		cmd.SetErr(&shared)
		_, err := validateAutoFixBackend(cmd, proj, root)
		require.NoError(t, err)
	}
	require.Equal(t, 3, strings.Count(shared.String(), noSandboxWarnMarker),
		"the warning must appear exactly once per invocation (no memoization across calls)")
}

// TestWarnNoSandbox_FiresEvenWhenGateFails: the warning fires the moment the
// bypass is chosen, BEFORE the combined missing-piece early return — so a
// --no-sandbox run that ALSO fails another check (missing token) still emits the
// warning (AC 03-03 EC2). Locks the "warn before the len(missing)>0 return"
// ordering against a refactor that moves the warning into the success path.
func TestWarnNoSandbox_FiresEvenWhenGateFails(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "."}}
	cmd := noSandboxCmd(t, "o/r", "") // token missing -> gate fails
	var errBuf strings.Builder
	cmd.SetErr(&errBuf)
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err, "the gate must still fail on the missing token")
	require.Contains(t, errBuf.String(), noSandboxWarnMarker,
		"the warning must fire even when the --no-sandbox run fails another gate check")
}

// TestWarnNoSandbox_NamesTheRisk: the warning is explicit about the risk —
// container isolation is off and untrusted/LLM-generated code runs on the host
// (AC 03-03 Scenario 3).
func TestWarnNoSandbox_NamesTheRisk(t *testing.T) {
	got := runNoSandboxGate(t)
	for _, want := range []string{"WARNING", noSandboxWarnMarker, "LLM-generated", "host"} {
		require.Contains(t, got, want, "the --no-sandbox warning must name the specific risk")
	}
}

// TestWarnNoSandbox_AbsentWhenFlagUnset: the default (sandboxed) path prints NO
// --no-sandbox warning — the warning is strictly conditional on the flag being
// true (AC 03-03 EC3, regression guard against an inverted condition).
func TestWarnNoSandbox_AbsentWhenFlagUnset(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{
		Agents:  []string{"a"},
		AutoFix: &registry.AutoFixConfig{ApplyTarget: "."},
		Sandbox: sandboxConfig(fakeDockerShim(t, true)),
	}
	cmd := autoFixCmd(t, "o/r", "tok", "") // no --no-sandbox
	var errBuf strings.Builder
	cmd.SetErr(&errBuf)
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.NoError(t, err)
	require.NotContains(t, errBuf.String(), noSandboxWarnMarker,
		"no --no-sandbox warning may appear on the default sandboxed path")
}

// TestValidateAutoFixBackend_NoSandboxFalseKeepsGate: --no-sandbox=false is
// identical to the flag being absent — the real resolver/preflight gate runs and a
// missing sandbox is a hard refusal (AC 03-02 EC3 regression guard). The bypass
// must be gated strictly on true, not on "the flag was set". Uses the REAL
// resolver (no spy) so the refusal it asserts is genuine, proving the resolver was
// actually reached.
func TestValidateAutoFixBackend_NoSandboxFalseKeepsGate(t *testing.T) {
	clearGitHubEnv(t)
	root := t.TempDir()
	writeGoMod(t, root)
	proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "."}}
	cmd := autoFixCmd(t, "o/r", "tok", "")
	require.NoError(t, cmd.Flags().Set("no-sandbox", "false"))
	_, err := validateAutoFixBackend(cmd, proj, root)
	require.Error(t, err, "--no-sandbox=false must leave the sandbox gate intact")
	require.Equal(t, 2, exitCode(err))
	require.Contains(t, err.Error(), "[sandbox] block", "the resolver's unconfigured refusal must run when --no-sandbox is false")
}
