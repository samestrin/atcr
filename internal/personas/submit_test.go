package personas

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/gitexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSubmitter is an in-memory GitHubSubmitter recorder: it records the order of
// calls and returns canned results, so Submit's sequencing/short-circuit behavior
// (AC 02-02) can be unit-tested with zero real gh process or network calls
// (AC 02-03). Reused across the happy-path, exactly-once, and each-failure tests.
type stubSubmitter struct {
	calls []string

	preconErr error
	forkErr   error
	syncErr   error
	pushHead  string
	pushErr   error
	prURL     string
	prErr     error

	gotPR PRRequest
}

func (s *stubSubmitter) CheckPrecondition(_ context.Context) error {
	s.calls = append(s.calls, "precondition")
	return s.preconErr
}

func (s *stubSubmitter) Fork(_ context.Context, repo string) error {
	s.calls = append(s.calls, "fork:"+repo)
	return s.forkErr
}

func (s *stubSubmitter) SyncFork(_ context.Context, repo string) error {
	s.calls = append(s.calls, "sync:"+repo)
	return s.syncErr
}

func (s *stubSubmitter) PushBranch(_ context.Context, branch, _, _ string) (string, error) {
	s.calls = append(s.calls, "push:"+branch)
	return s.pushHead, s.pushErr
}

func (s *stubSubmitter) CreatePR(_ context.Context, req PRRequest) (string, error) {
	s.calls = append(s.calls, "pr")
	s.gotPR = req
	return s.prURL, s.prErr
}

// TestSubmit_HappyPath covers AC 02-02 Scenario 1: precondition → fork → push →
// pr-create run exactly once, in order, and the PR URL is returned. The PR request
// carries the source persona name for downstream attribution (Edge Case 2).
func TestSubmit_HappyPath(t *testing.T) {
	s := &stubSubmitter{pushHead: "octocat:persona-submit/sasha", prURL: "https://github.com/samestrin/atcr/pull/42"}

	url, err := Submit(context.Background(), s, personasDirWith(t, "sasha"), t.TempDir(), "sasha")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/samestrin/atcr/pull/42", url)
	assert.Equal(t, []string{"precondition", "fork:samestrin/atcr", "sync:samestrin/atcr", "push:persona-submit/sasha", "pr"}, s.calls)
	assert.Equal(t, "samestrin/atcr", s.gotPR.Repo)
	assert.Equal(t, "octocat:persona-submit/sasha", s.gotPR.Head)
	assert.Contains(t, s.gotPR.Body, "sasha", "PR body carries the source persona name for attribution")
}

// TestSubmit_ExactlyOnce asserts no step is invoked more than once on a clean run.
func TestSubmit_ExactlyOnce(t *testing.T) {
	s := &stubSubmitter{pushHead: "octocat:persona-submit/sasha", prURL: "https://x/pull/1"}
	_, err := Submit(context.Background(), s, personasDirWith(t, "sasha"), t.TempDir(), "sasha")
	require.NoError(t, err)

	counts := map[string]int{}
	for _, c := range s.calls {
		counts[c]++
	}
	assert.Equal(t, 1, counts["precondition"])
	assert.Equal(t, 1, counts["fork:samestrin/atcr"])
	assert.Equal(t, 1, counts["sync:samestrin/atcr"])
	assert.Equal(t, 1, counts["push:persona-submit/sasha"])
	assert.Equal(t, 1, counts["pr"])
}

// TestSubmit_InvalidNameShortCircuits is the defense-in-depth guard (2.2.A): even
// though the command runs SubmitGate first, the orchestrator re-validates the name
// before any GitHub interaction, so an invalid name never reaches the seam.
func TestSubmit_InvalidNameShortCircuits(t *testing.T) {
	s := &stubSubmitter{}

	_, err := Submit(context.Background(), s, t.TempDir(), t.TempDir(), "../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid persona name")
	assert.Empty(t, s.calls, "an invalid name reaches neither precondition nor fork")
}

// TestSubmit_EmptyPRURLIsError covers the 2.2.A empty-response guard: a seam that
// returns exit-0 with no URL must surface an error, not a silent success that
// prints a blank stdout line.
func TestSubmit_EmptyPRURLIsError(t *testing.T) {
	s := &stubSubmitter{pushHead: "octocat:persona-submit/sasha", prURL: "   "}

	url, err := Submit(context.Background(), s, personasDirWith(t, "sasha"), t.TempDir(), "sasha")
	require.Error(t, err)
	assert.Empty(t, url)
	assert.Contains(t, err.Error(), "no URL")
}

// TestSubmit_PreconditionShortCircuits covers AC 02-01: a failed precondition
// halts before any fork/branch/PR call.
func TestSubmit_PreconditionShortCircuits(t *testing.T) {
	s := &stubSubmitter{preconErr: errors.New("gh auth check failed: not logged in")}

	_, err := Submit(context.Background(), s, t.TempDir(), t.TempDir(), "sasha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gh auth check failed")
	assert.Equal(t, []string{"precondition"}, s.calls, "no fork/push/pr after a failed precondition")
}

// TestSubmit_ForkFailShortCircuits covers AC 02-02 Error Scenario 1: a fork
// failure (not "already exists") halts before branch/push with the documented
// message, and no push/pr call is made.
func TestSubmit_ForkFailShortCircuits(t *testing.T) {
	s := &stubSubmitter{forkErr: errors.New("HTTP 403: forbidden")}

	_, err := Submit(context.Background(), s, personasDirWith(t, "sasha"), t.TempDir(), "sasha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fork samestrin/atcr")
	assert.Contains(t, err.Error(), "HTTP 403: forbidden")
	assert.Equal(t, []string{"precondition", "fork:samestrin/atcr"}, s.calls)
}

// TestSubmit_PushFailShortCircuits covers AC 02-02 Error Scenario 2: a push
// failure after a successful fork halts before pr-create.
func TestSubmit_PushFailShortCircuits(t *testing.T) {
	s := &stubSubmitter{pushErr: errors.New("permission denied")}

	_, err := Submit(context.Background(), s, personasDirWith(t, "sasha"), t.TempDir(), "sasha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to push branch to fork")
	assert.Contains(t, err.Error(), "permission denied")
	assert.Equal(t, []string{"precondition", "fork:samestrin/atcr", "sync:samestrin/atcr", "push:persona-submit/sasha"}, s.calls, "push failure short-circuits before pr-create")
}

// TestSubmit_PRFailIsRecoverable covers AC 02-02 Error Scenario 3: a pr-create
// failure after a successful push surfaces an actionable recovery path and does
// not roll back the pushed branch.
func TestSubmit_PRFailIsRecoverable(t *testing.T) {
	s := &stubSubmitter{pushHead: "octocat:persona-submit/sasha", prErr: errors.New("could not create pull request")}

	_, err := Submit(context.Background(), s, personasDirWith(t, "sasha"), t.TempDir(), "sasha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PR creation failed")
	assert.Contains(t, err.Error(), "retry with 'gh pr create'")
	assert.Contains(t, err.Error(), "atcr personas submit sasha")
	assert.Equal(t, []string{"precondition", "fork:samestrin/atcr", "sync:samestrin/atcr", "push:persona-submit/sasha", "pr"}, s.calls)
}

// TestCheckGHPrecondition_NotOnPath covers AC 02-01 Error Scenario 1: gh absent
// from PATH halts with the exact actionable install message.
func TestCheckGHPrecondition_NotOnPath(t *testing.T) {
	restore := swapPreconditionSeams(t, func() (string, error) { return "", errors.New("not found") }, nil)
	defer restore()

	err := checkGHPrecondition(context.Background())
	require.Error(t, err)
	assert.Equal(t, "gh CLI not found on PATH; install it from https://cli.github.com", err.Error())
}

// TestCheckGHPrecondition_NotAuthed covers AC 02-01 Error Scenario 2: gh present
// but not authenticated surfaces the captured stderr.
func TestCheckGHPrecondition_NotAuthed(t *testing.T) {
	restore := swapPreconditionSeams(t,
		func() (string, error) { return "/usr/bin/gh", nil },
		func(context.Context) (string, error) {
			return "You are not logged into any GitHub hosts", errors.New("exit status 1")
		},
	)
	defer restore()

	err := checkGHPrecondition(context.Background())
	require.Error(t, err)
	assert.Equal(t, "gh auth check failed: You are not logged into any GitHub hosts", err.Error())
}

// TestCheckGHPrecondition_OK covers AC 02-01 Scenario 1: gh present and
// authenticated returns nil.
func TestCheckGHPrecondition_OK(t *testing.T) {
	restore := swapPreconditionSeams(t,
		func() (string, error) { return "/usr/bin/gh", nil },
		func(context.Context) (string, error) { return "", nil },
	)
	defer restore()

	assert.NoError(t, checkGHPrecondition(context.Background()))
}

// TestSubmissionBranch confirms slashes in a namespaced persona name are made
// ref-safe so the head branch is a single path segment under persona-submit/.
func TestSubmissionBranch(t *testing.T) {
	assert.Equal(t, "persona-submit/sasha", submissionBranch("sasha"))
	assert.Equal(t, "persona-submit/security-owasp", submissionBranch("security/owasp"))
}

// TestSubmissionBranch_CollisionFree confirms that distinct persona names such as
// "foo/bar" and "foo-bar" do not map to the same branch, which would cause the
// second submission to clobber the first (TD item submit.go:122).
func TestSubmissionBranch_CollisionFree(t *testing.T) {
	assert.Equal(t, "persona-submit/foo-bar", submissionBranch("foo/bar"))
	assert.Equal(t, "persona-submit/foo--bar", submissionBranch("foo-bar"))
	assert.NotEqual(t, submissionBranch("foo/bar"), submissionBranch("foo-bar"))
}

// TestExistingPRURL covers AC 02-02 Edge Case 1: when gh reports a pull request
// already exists for the branch, its URL is extracted from stderr; unrelated
// errors extract nothing.
func TestExistingPRURL(t *testing.T) {
	stderr := "a pull request for branch \"persona-submit/sasha\" into branch \"main\" already exists:\nhttps://github.com/samestrin/atcr/pull/7"
	assert.Equal(t, "https://github.com/samestrin/atcr/pull/7", existingPRURL(stderr))
	assert.Empty(t, existingPRURL("could not create pull request: network error"))
	// Characterization (TD-004): an "already exists" error that carries no pull URL
	// yields "" — the caller then surfaces it as a real failure rather than a reused
	// PR. Pins the no-URL branch of the lenient match.
	assert.Empty(t, existingPRURL("a pull request already exists but gh printed no url"),
		"'already exists' without a /pull/ URL returns no URL (lenient match, TD-004)")
}

// TestForkAlreadyExists covers AC 02-02 Scenario 2: a fork that already exists is
// recognized as the non-fatal reuse case.
func TestForkAlreadyExists(t *testing.T) {
	assert.True(t, forkAlreadyExists("! octocat/atcr already exists"))
	assert.False(t, forkAlreadyExists("HTTP 403: forbidden"))
	// Characterization of the accepted lenient match (TD-004): gh exposes no
	// structured "fork already exists" signal, so ANY stderr containing the phrase
	// reads as benign reuse — including a non-reuse failure that incidentally
	// contains it. This pins that intentional false-positive so a future tightening
	// is a conscious, tested change, not an accident.
	assert.True(t, forkAlreadyExists("fatal: destination path already exists and is not empty"),
		"lenient substring match treats any 'already exists' text as reuse (TD-004 accepted tradeoff)")
}

// TestGHError confirms the leaf error helper preserves the underlying error (so a
// context timeout is detectable and never surfaces blank) and appends stderr only
// when present (2.2.A fix).
func TestGHError(t *testing.T) {
	base := context.DeadlineExceeded

	withStderr := ghError(base, "  HTTP 403  ")
	assert.Equal(t, "context deadline exceeded: HTTP 403", withStderr.Error())
	assert.ErrorIs(t, withStderr, context.DeadlineExceeded, "underlying error is unwrappable")

	blank := ghError(base, "   ")
	assert.Equal(t, "context deadline exceeded", blank.Error(), "empty stderr is elided, error still surfaces")
	assert.ErrorIs(t, blank, context.DeadlineExceeded)
}

// TestCopyPersonaUnit covers the fork-copy step's file logic without any gh/git:
// a resolved persona unit is written into personas/community/<name>.yaml under the
// work tree, byte-for-byte, and an invalid name is rejected before any write.
func TestCopyPersonaUnit(t *testing.T) {
	personasDir := t.TempDir()
	workDir := t.TempDir()
	body := []byte("provider: anthropic\nmodel: claude-sonnet-4-6\nrole: reviewer\n")
	prompt := []byte("# Sasha\nLocally-tuned custom reviewer prompt.\n")
	require.NoError(t, os.WriteFile(filepath.Join(personasDir, "sasha.yaml"), body, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(personasDir, "sasha.md"), prompt, 0o600))

	require.NoError(t, copyPersonaUnit(personasDir, "sasha", workDir))
	gotYAML, err := os.ReadFile(filepath.Join(workDir, "personas", "community", "sasha.yaml"))
	require.NoError(t, err)
	assert.Equal(t, body, gotYAML)
	// The co-located <name>.md custom prompt — where local tuning lives — must ride
	// along, or the PR diverges from the fixture-validated unit (2.5 gate HIGH).
	gotMD, err := os.ReadFile(filepath.Join(workDir, "personas", "community", "sasha.md"))
	require.NoError(t, err)
	assert.Equal(t, prompt, gotMD)

	err = copyPersonaUnit(personasDir, "../escape", workDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid persona name")
}

// TestCopyPersonaUnit_BindingOnly covers a persona with no co-located .md: the
// YAML alone is the whole unit, so the copy succeeds and writes no stray .md.
func TestCopyPersonaUnit_BindingOnly(t *testing.T) {
	personasDir := t.TempDir()
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(personasDir, "binder.yaml"), []byte("provider: anthropic\nmodel: x\n"), 0o600))

	require.NoError(t, copyPersonaUnit(personasDir, "binder", workDir))
	_, statErr := os.Stat(filepath.Join(workDir, "personas", "community", "binder.md"))
	assert.True(t, os.IsNotExist(statErr), "no .md is written for a binding-only persona")
}

// TestNewGitHubSubmitter confirms the production constructor returns a usable,
// non-nil seam implementation.
func TestNewGitHubSubmitter(t *testing.T) {
	assert.NotNil(t, NewGitHubSubmitter())
}

// TestGHSubmitterCheckPrecondition confirms the default seam's precondition
// delegates to checkGHPrecondition, exercised via the Path/AuthStatus sub-seams so
// no real gh binary is touched (AC 02-03).
func TestGHSubmitterCheckPrecondition(t *testing.T) {
	restore := swapPreconditionSeams(t,
		func() (string, error) { return "/usr/bin/gh", nil },
		func(context.Context) (string, error) { return "", nil },
	)
	defer restore()

	assert.NoError(t, NewGitHubSubmitter().CheckPrecondition(context.Background()))
}

// swapPreconditionSeams overrides the low-level ghPath/ghAuthStatus seams for a
// test and returns a restore func, so the precondition check's exact error
// strings are exercised without a real gh binary (AC 02-01 / AC 02-03).
func swapPreconditionSeams(t *testing.T, path func() (string, error), auth func(context.Context) (string, error)) func() {
	t.Helper()
	oldPath, oldAuth := ghPath, ghAuthStatus
	if path != nil {
		ghPath = path
	}
	if auth != nil {
		ghAuthStatus = auth
	}
	return func() { ghPath, ghAuthStatus = oldPath, oldAuth }
}

// TestGitHasStagedChanges exercises the empty-diff guard used by PushBranch.
// TestCommitPersona_PassesExplicitIdentity proves the persona commit supplies the
// committer identity via -c flags rather than depending on the global git config that
// gitexec nulls out (GIT_CONFIG_GLOBAL=/dev/null) — the "Author identity unknown" /
// fabricated-author regression the gitexec migration would otherwise introduce on the
// fresh fork clone.
func TestCommitPersona_PassesExplicitIdentity(t *testing.T) {
	orig := gitexec.CommandContextFn
	defer func() { gitexec.CommandContextFn = orig }()
	var got []string
	gitexec.CommandContextFn = func(_ context.Context, arg ...string) *exec.Cmd {
		got = arg
		return exec.Command("true")
	}
	if err := commitPersona(context.Background(), "", "octocat", "web/react"); err != nil {
		t.Fatalf("commitPersona: %v", err)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-c user.name=octocat") {
		t.Errorf("commit missing explicit user.name: %v", got)
	}
	if !strings.Contains(joined, "-c user.email=octocat@users.noreply.github.com") {
		t.Errorf("commit missing explicit user.email: %v", got)
	}
	if !slices.Contains(got, "commit") {
		t.Errorf("expected a commit invocation, got: %v", got)
	}
}

func TestGitHasStagedChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		require.NoError(t, c.Run())
	}
	run("init", "-q")

	has, err := gitHasStagedChanges(context.Background(), dir)
	require.NoError(t, err)
	assert.False(t, has, "a fresh repo has no staged changes")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o600))
	has, err = gitHasStagedChanges(context.Background(), dir)
	require.NoError(t, err)
	assert.False(t, has, "unstaged file is not a staged change")

	run("add", "a.txt")
	has, err = gitHasStagedChanges(context.Background(), dir)
	require.NoError(t, err)
	assert.True(t, has, "staged change is detected")
}

// TestSubmit_WritesMarkerAfterPR_MalformedHead covers the malformed push-head
// edge case: when the stub returns a head without an "owner:" prefix, Submit
// records the whole head as the submitter rather than silently corrupting the
// marker (TD item submit_test.go:310).
func TestSubmit_WritesMarkerAfterPR_MalformedHead(t *testing.T) {
	personasDir := t.TempDir()
	subDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(personasDir, "sasha.yaml"), []byte(validPersonaYAML), 0o600))
	s := &stubSubmitter{pushHead: "persona-submit/sasha", prURL: "https://github.com/samestrin/atcr/pull/42"}

	_, err := Submit(context.Background(), s, personasDir, subDir, "sasha")
	require.NoError(t, err)

	got, ok, err := ReadSubmission(subDir, "sasha")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "persona-submit/sasha", got.Submitter, "submitter falls back to the whole head when owner is missing")
}

// personasDirWith returns a personas dir containing a minimal <name>.yaml unit so
// Submit's pre-fork "nothing local to submit" existence check passes and the
// gh-sequencing assertions reach the seam.
func personasDirWith(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(validPersonaYAML), 0o600))
	return dir
}

// swapGhExec overrides the ghExec seam for a test and restores it on cleanup, so
// ghSubmitter.Fork/SyncFork/CreatePR can be driven with canned stdout/stderr and
// zero real gh process or network calls (AC 02-03).
func swapGhExec(t *testing.T, fn func(context.Context, ...string) (bytes.Buffer, bytes.Buffer, error)) {
	t.Helper()
	old := ghExec
	ghExec = fn
	t.Cleanup(func() { ghExec = old })
}

// cannedGhExec returns a ghExec stub yielding the given stdout/stderr and error.
func cannedGhExec(stdout, stderr string, err error) func(context.Context, ...string) (bytes.Buffer, bytes.Buffer, error) {
	return func(context.Context, ...string) (bytes.Buffer, bytes.Buffer, error) {
		var out, errb bytes.Buffer
		out.WriteString(stdout)
		errb.WriteString(stderr)
		return out, errb, err
	}
}

// TestSubmit_NoLocalUnitShortCircuits covers the pre-fork existence check: a name
// with no installed local persona unit is rejected before any fork/sync/push, so
// only submittable local units reach the gh flow (clarification 2026-07-10).
func TestSubmit_NoLocalUnitShortCircuits(t *testing.T) {
	s := &stubSubmitter{}

	_, err := Submit(context.Background(), s, t.TempDir(), t.TempDir(), "ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nothing local to submit")
	assert.Equal(t, []string{"precondition"}, s.calls, "a missing local unit halts before fork/sync/push")
}

// TestSubmit_SyncFailShortCircuits covers AC 02-02 Scenario 2: a fork-sync failure
// after a successful fork halts before branch/push so a stale fork never produces a
// garbage PR.
func TestSubmit_SyncFailShortCircuits(t *testing.T) {
	s := &stubSubmitter{syncErr: errors.New("merge conflict")}

	_, err := Submit(context.Background(), s, personasDirWith(t, "sasha"), t.TempDir(), "sasha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to sync fork with upstream samestrin/atcr")
	assert.Contains(t, err.Error(), "merge conflict")
	assert.Equal(t, []string{"precondition", "fork:samestrin/atcr", "sync:samestrin/atcr"}, s.calls, "sync failure short-circuits before push")
}

// TestGitInvocation routes git through gh's credential helper so a gh-authed user
// who never ran `gh auth setup-git` does not hang on a credential prompt.
func TestGitInvocation(t *testing.T) {
	got := gitInvocation("clone", "--depth", "1", "https://github.com/octocat/atcr.git", "/tmp/x")
	assert.Equal(t, []string{
		"-c", "credential.helper=!gh auth git-credential",
		"clone", "--depth", "1", "https://github.com/octocat/atcr.git", "/tmp/x",
	}, got, "clone/push authenticate via gh's token, not git's own github.com helper")
}

// TestGhSubmitterFork_ReuseAndError drives the production adapter's fork reuse and
// real-error branches (submit.go) through the ghExec seam with canned stderr — the
// wiring that consumes forkAlreadyExists, previously untested.
func TestGhSubmitterFork_ReuseAndError(t *testing.T) {
	swapGhExec(t, cannedGhExec("", "! octocat/atcr already exists", errors.New("exit status 1")))
	assert.NoError(t, ghSubmitter{}.Fork(context.Background(), "samestrin/atcr"), "an already-existing fork is non-fatal reuse")

	swapGhExec(t, cannedGhExec("", "HTTP 403: forbidden", errors.New("exit status 1")))
	err := ghSubmitter{}.Fork(context.Background(), "samestrin/atcr")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 403: forbidden", "a non-reuse fork error surfaces")
}

// TestGhSubmitterCreatePR_ReuseAndError drives the production adapter's existing-PR
// reuse and real-error branches through the ghExec seam — the wiring that consumes
// existingPRURL, previously untested.
func TestGhSubmitterCreatePR_ReuseAndError(t *testing.T) {
	req := PRRequest{Repo: "samestrin/atcr", Head: "octocat:persona-submit/sasha", Title: "t", Body: "b"}

	swapGhExec(t, cannedGhExec("https://github.com/samestrin/atcr/pull/99\n", "", nil))
	url, err := ghSubmitter{}.CreatePR(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/samestrin/atcr/pull/99", url, "a created PR returns its trimmed URL")

	swapGhExec(t, cannedGhExec("", "a pull request for branch \"persona-submit/sasha\" into branch \"main\" already exists:\nhttps://github.com/samestrin/atcr/pull/7", errors.New("exit status 1")))
	url, err = ghSubmitter{}.CreatePR(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/samestrin/atcr/pull/7", url, "an existing PR yields that PR's URL, not an error")

	swapGhExec(t, cannedGhExec("", "could not create pull request: network error", errors.New("exit status 1")))
	_, err = ghSubmitter{}.CreatePR(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network error", "a non-reuse pr-create error surfaces")
}
