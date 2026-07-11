package personas

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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

	url, err := Submit(context.Background(), s, t.TempDir(), t.TempDir(), "sasha")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/samestrin/atcr/pull/42", url)
	assert.Equal(t, []string{"precondition", "fork:samestrin/atcr", "push:persona-submit/sasha", "pr"}, s.calls)
	assert.Equal(t, "samestrin/atcr", s.gotPR.Repo)
	assert.Equal(t, "octocat:persona-submit/sasha", s.gotPR.Head)
	assert.Contains(t, s.gotPR.Body, "sasha", "PR body carries the source persona name for attribution")
}

// TestSubmit_ExactlyOnce asserts no step is invoked more than once on a clean run.
func TestSubmit_ExactlyOnce(t *testing.T) {
	s := &stubSubmitter{pushHead: "octocat:persona-submit/sasha", prURL: "https://x/pull/1"}
	_, err := Submit(context.Background(), s, t.TempDir(), t.TempDir(), "sasha")
	require.NoError(t, err)

	counts := map[string]int{}
	for _, c := range s.calls {
		counts[c]++
	}
	assert.Equal(t, 1, counts["precondition"])
	assert.Equal(t, 1, counts["fork:samestrin/atcr"])
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

	url, err := Submit(context.Background(), s, t.TempDir(), t.TempDir(), "sasha")
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

	_, err := Submit(context.Background(), s, t.TempDir(), t.TempDir(), "sasha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fork samestrin/atcr")
	assert.Contains(t, err.Error(), "HTTP 403: forbidden")
	assert.Equal(t, []string{"precondition", "fork:samestrin/atcr"}, s.calls)
}

// TestSubmit_PushFailShortCircuits covers AC 02-02 Error Scenario 2: a push
// failure after a successful fork halts before pr-create.
func TestSubmit_PushFailShortCircuits(t *testing.T) {
	s := &stubSubmitter{pushErr: errors.New("permission denied")}

	_, err := Submit(context.Background(), s, t.TempDir(), t.TempDir(), "sasha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to push branch to fork")
	assert.Contains(t, err.Error(), "permission denied")
	assert.Equal(t, []string{"precondition", "fork:samestrin/atcr", "push:persona-submit/sasha"}, s.calls, "push failure short-circuits before pr-create")
}

// TestSubmit_PRFailIsRecoverable covers AC 02-02 Error Scenario 3: a pr-create
// failure after a successful push surfaces an actionable recovery path and does
// not roll back the pushed branch.
func TestSubmit_PRFailIsRecoverable(t *testing.T) {
	s := &stubSubmitter{pushHead: "octocat:persona-submit/sasha", prErr: errors.New("could not create pull request")}

	_, err := Submit(context.Background(), s, t.TempDir(), t.TempDir(), "sasha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PR creation failed")
	assert.Contains(t, err.Error(), "retry with 'gh pr create'")
	assert.Contains(t, err.Error(), "atcr personas submit sasha")
	assert.Equal(t, []string{"precondition", "fork:samestrin/atcr", "push:persona-submit/sasha", "pr"}, s.calls)
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
}

// TestForkAlreadyExists covers AC 02-02 Scenario 2: a fork that already exists is
// recognized as the non-fatal reuse case.
func TestForkAlreadyExists(t *testing.T) {
	assert.True(t, forkAlreadyExists("! octocat/atcr already exists"))
	assert.False(t, forkAlreadyExists("HTTP 403: forbidden"))
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
