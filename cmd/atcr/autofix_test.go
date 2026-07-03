package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "."}}
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
	proj := &registry.ProjectConfig{Agents: []string{"a"}, AutoFix: &registry.AutoFixConfig{ApplyTarget: "."}}
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

// --- runAutoFix sequencing (the Story-4/5 gate: no GitHub before validation) -

// fakeGitHub records calls and lets a test assert the sequencing guarantee.
type fakeGitHub struct {
	branchCalls int
	commitCalls int
	findCalls   int
	createPR    int
	updatePR    int
}

func (f *fakeGitHub) CreateBranch(_ context.Context, _, _, _, _ string) error {
	f.branchCalls++
	return nil
}
func (f *fakeGitHub) CreateCommit(_ context.Context, _, _ string, _ ghaction.CommitRequest) (string, error) {
	f.commitCalls++
	return "deadbeef", nil
}
func (f *fakeGitHub) FindOpenPullRequest(_ context.Context, _, _, _ string) (int, bool, error) {
	f.findCalls++
	return 0, false, nil
}
func (f *fakeGitHub) CreatePullRequest(_ context.Context, _, _ string, _ ghaction.PullRequestRequest) (int, error) {
	f.createPR++
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

// --- thin finding->entry selection ----------------------------------------

// TestSelectAutoFixEntries_FiltersByThresholdAndFix: only findings carrying a Fix
// at/above the threshold are turned into entries.
func TestSelectAutoFixEntries_FiltersByThresholdAndFix(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.txt", Line: 1, Fix: diffFor("a.txt")},
		{Severity: "LOW", File: "b.txt", Line: 1, Fix: diffFor("b.txt")}, // below threshold
		{Severity: "CRITICAL", File: "c.txt", Line: 1, Fix: ""},          // no fix
	}
	entries, err := selectAutoFixEntries(findings, "HIGH")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "a.txt", entries[0].Path)

	// No threshold -> every finding carrying a Fix is included.
	all, err := selectAutoFixEntries(findings, "")
	require.NoError(t, err)
	require.Len(t, all, 2)
}
