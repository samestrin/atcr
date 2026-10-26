package fanout

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSASTRunner stands in for a real scanner (Epic 50.0): it parses the
// committed fixture SARIF against the scan root, the same path a real runner
// takes, so no Semgrep is needed.
type fakeSASTRunner struct {
	calls atomic.Int32
	files []string
	sarif string
	err   error
	block chan struct{} // when non-nil, Run ignores ctx and waits on it
	panic bool
}

func (f *fakeSASTRunner) Name() string { return "fake" }

func (f *fakeSASTRunner) Run(_ context.Context, root string, files []string) ([]sast.Finding, error) {
	f.calls.Add(1)
	f.files = files
	if f.panic {
		panic("scanner exploded")
	}
	if f.block != nil {
		<-f.block
	}
	if f.err != nil {
		return nil, f.err
	}
	findings, _, err := sast.ParseSARIF(strings.NewReader(f.sarif), root)
	return findings, err
}

// sastFixture is a SARIF log with one finding on auth.go (the file initRepo
// changes) and one whose path escapes the root, which must never surface.
const sastFixture = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"fake"}},"results":[
 {"ruleId":"go.lang.security.unchecked-call","message":{"text":"call result is never checked"},
  "locations":[{"physicalLocation":{"artifactLocation":{"uri":"auth.go"},"region":{"startLine":3}}}]},
 {"ruleId":"escape","message":{"text":"outside the root"},
  "locations":[{"physicalLocation":{"artifactLocation":{"uri":"../etc/passwd"},"region":{"startLine":1}}}]}
]}]}`

func withSASTRunner(t *testing.T, r sast.Runner, err error) {
	t.Helper()
	old := newSASTRunner
	newSASTRunner = func() (sast.Runner, error) { return r, err }
	t.Cleanup(func() { newSASTRunner = old })
}

func sastEnabledConfig(srvURL string) *ReviewConfig {
	cfg := twoAgentConfig(srvURL)
	cfg.Project.SAST = &registry.SASTConfig{Enabled: true}
	return cfg
}

var sastFenceRe = regexp.MustCompile(`(?s)<(sast-[0-9a-f]{16})>(.*?)</(sast-[0-9a-f]{16})>`)

func TestPrepareReview_SASTFindingsReachEveryReviewerPromptFenced(t *testing.T) {
	repo, base, head := initRepo(t)
	fake := &fakeSASTRunner{sarif: sastFixture}
	withSASTRunner(t, fake, nil)

	prep, err := PrepareReview(context.Background(), sastEnabledConfig("http://unused"), reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	require.Equal(t, int32(1), fake.calls.Load(), "the scanner runs once per review, not once per agent")
	assert.Equal(t, []string{"auth.go"}, fake.files, "the scan is limited to the changed files")

	require.NotEmpty(t, prep.Slots)
	var sentinel string
	for _, s := range prep.Slots {
		m := sastFenceRe.FindStringSubmatch(s.Primary.Prompt)
		require.NotNil(t, m, "agent %q: the rendered prompt carries the fenced SAST block", s.Primary.Name)
		assert.Equal(t, m[1], m[3], "open and close tags share the sentinel")
		assert.Contains(t, m[2], "auth.go:3")
		assert.Contains(t, m[2], "go.lang.security.unchecked-call")
		assert.Contains(t, m[2], "call result is never checked")
		assert.NotContains(t, s.Primary.Prompt, "../etc/passwd", "an escaping path never reaches a prompt")
		assert.Equal(t, s.Primary.Prompt, s.Primary.Invocation.Prompt, "the block is in the text actually sent")
		if sentinel == "" {
			sentinel = m[1]
		}
		assert.Equal(t, sentinel, m[1], "one sentinel per review")
		// The block lands ahead of the diff, so it never sits inside a file body.
		assert.Less(t, strings.Index(s.Primary.Prompt, m[0]), strings.Index(s.Primary.Prompt, "func b()"))
	}
}

func TestPrepareReview_SASTIsOffByDefault(t *testing.T) {
	repo, base, head := initRepo(t)
	fake := &fakeSASTRunner{sarif: sastFixture}
	withSASTRunner(t, fake, nil)

	prep, err := PrepareReview(context.Background(), twoAgentConfig("http://unused"), reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	assert.Zero(t, fake.calls.Load(), "no sast block in the config means the scanner never runs")
	for _, s := range prep.Slots {
		assert.NotRegexp(t, sastFenceRe, s.Primary.Prompt)
	}
}

// AC: a missing, crashing or timed-out scanner does not fail the review: it
// fails open with a visible notice.
func TestRunReview_SASTUnavailableFailsOpenWithANotice(t *testing.T) {
	cases := map[string]func(t *testing.T){
		"no runner": func(t *testing.T) {
			withSASTRunner(t, nil, errors.New("semgrep not found on PATH"))
		},
		"runner error": func(t *testing.T) {
			withSASTRunner(t, &fakeSASTRunner{err: errors.New("exit status 2")}, nil)
		},
		"runner panics": func(t *testing.T) {
			withSASTRunner(t, &fakeSASTRunner{panic: true}, nil)
		},
		"runner hangs past the timeout": func(t *testing.T) {
			release := make(chan struct{})
			t.Cleanup(func() { close(release) })
			withSASTRunner(t, &fakeSASTRunner{block: release, sarif: sastFixture}, nil)
			old := sastScanTimeout
			sastScanTimeout = 50 * time.Millisecond
			t.Cleanup(func() { sastScanTimeout = old })
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ATCR_TEST_KEY", "secret")
			repo, base, head := initRepo(t)
			srv := mockProvider(t)
			setup(t)

			var (
				res *ReviewResult
				err error
			)
			start := time.Now()
			stderr := captureStderr(t, func() {
				res, err = RunReview(context.Background(), llmclient.New(), sastEnabledConfig(srv.URL), reviewReq(repo, repo, base, head))
			})
			require.NoError(t, err, "an unavailable scanner must not fail the review")
			require.NotNil(t, res)
			assert.Equal(t, 2, res.Summary.Succeeded)
			assert.Contains(t, stderr, "atcr: notice: SAST pre-filter skipped")
			assert.Contains(t, stderr, "the review continues without deterministic findings")
			assert.Less(t, time.Since(start), 30*time.Second)
		})
	}
}

// AC: SAST snippets pass the secret guard before reaching any LLM. A scanner
// message is repo-derived text, so a credential it quotes is redacted like one
// in the diff.
func TestPrepareReview_SASTBlockPassesTheSecretGuard(t *testing.T) {
	repo, base, head := initRepo(t)
	// Hide gitleaks so the built-in ruleset decides deterministically, but keep
	// git, which PrepareReview needs to build the payload.
	gitBin, err := exec.LookPath("git")
	require.NoError(t, err)
	binDir := t.TempDir()
	require.NoError(t, os.Symlink(gitBin, filepath.Join(binDir, "git")))
	t.Setenv("PATH", binDir)
	leaky := strings.Replace(sastFixture, "call result is never checked", "hardcoded key "+guardAWSKey, 1)
	withSASTRunner(t, &fakeSASTRunner{sarif: leaky}, nil)

	cfg := sastEnabledConfig("http://unused")
	cfg.Settings.SecretGuard = "redact"
	var prep *PreparedReview
	stderr := captureStderr(t, func() {
		prep, err = PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
	})
	require.NoError(t, err)
	for _, s := range prep.Slots {
		m := sastFenceRe.FindStringSubmatch(s.Primary.Prompt)
		require.NotNil(t, m)
		assert.NotContains(t, s.Primary.Invocation.Prompt, guardAWSKey, "the key must not reach the provider")
		assert.Contains(t, m[2], payload.SecretPlaceholder)
	}
	assert.Contains(t, stderr, "SAST findings", "the warning names the SAST block as where the secret was")
	assert.NotContains(t, stderr, guardAWSKey)

	cfg.Settings.SecretGuard = "block"
	captureStderr(t, func() {
		_, err = PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSecretGuardBlocked))
}

// A deleted file is still a changed path, but there is nothing on disk to
// scan; handing it to a scanner would fail the whole scan.
func TestSASTTargets_SkipsPathsNotOnDisk(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "kept.go"), []byte("package p\n"), 0o644))
	payloads := map[string]modePayload{
		"blocks": {Entries: []payload.FileEntry{{Path: "kept.go"}, {Path: "gone.go"}}},
		"diff":   {Entries: []payload.FileEntry{{Path: "kept.go"}}},
	}
	assert.Equal(t, []string{"kept.go"}, sastTargets(root, payloads))
}

func TestPrepareReview_SASTNoFindingsAddsNoBlock(t *testing.T) {
	repo, base, head := initRepo(t)
	withSASTRunner(t, &fakeSASTRunner{sarif: `{"version":"2.1.0","runs":[]}`}, nil)

	prep, err := PrepareReview(context.Background(), sastEnabledConfig("http://unused"), reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	for _, s := range prep.Slots {
		assert.NotRegexp(t, sastFenceRe, s.Primary.Prompt)
	}
}

// The default runner is T2's Semgrep wrapper; with no semgrep on PATH an
// enabled review still completes and the notice names the missing binary.
func TestPrepareReview_DefaultRunnerIsSemgrepAndFailsOpenWhenAbsent(t *testing.T) {
	r, err := newSASTRunner()
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "semgrep", r.Name())

	repo, base, head := initRepo(t)
	gitBin, err := exec.LookPath("git")
	require.NoError(t, err)
	binDir := t.TempDir()
	require.NoError(t, os.Symlink(gitBin, filepath.Join(binDir, "git")))
	t.Setenv("PATH", binDir)

	var prep *PreparedReview
	stderr := captureStderr(t, func() {
		prep, err = PrepareReview(context.Background(), sastEnabledConfig("http://unused"), reviewReq(repo, repo, base, head))
	})
	require.NoError(t, err)
	require.NotEmpty(t, prep.Slots)
	assert.Contains(t, stderr, "atcr: notice: SAST pre-filter skipped")
	assert.Contains(t, stderr, "binary not found on PATH")
}
