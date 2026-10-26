package cli

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/integrations/slack"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/webhook"
	"github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	whSigningSecretEnv = "ATCR_TEST_SLACK_SIGNING_SECRET"
	whBotTokenEnv      = "ATCR_TEST_SLACK_BOT_TOKEN"
	whGitHubTokenEnv   = "ATCR_TEST_GITHUB_TOKEN"
	whSigningSecret    = "test-signing-secret"
	whBotToken         = "xoxb-test-bot-token"
)

// lockedBuffer is a bytes.Buffer the daemon's goroutines and the test can
// share: the command writes its listen line and log lines from other
// goroutines than the one reading them.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// whPRFixture builds an "upstream" repository whose PR head lives only under
// refs/pull/7/head, as on GitHub, and a local checkout of it: the shape
// webhook.repos points at. It returns the checkout's absolute path.
func whPRFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	upstream := filepath.Join(root, "upstream")
	require.NoError(t, os.MkdirAll(upstream, 0o755))
	runFixtureGit(t, upstream, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(upstream, "a.txt"), []byte("main\n"), 0o644))
	runFixtureGit(t, upstream, "add", "a.txt")
	runFixtureGit(t, upstream, "commit", "-q", "-m", "main")
	runFixtureGit(t, upstream, "checkout", "-q", "-b", "pr")
	require.NoError(t, os.WriteFile(filepath.Join(upstream, "a.txt"), []byte("pr head\n"), 0o644))
	runFixtureGit(t, upstream, "commit", "-q", "-am", "pr")
	out, err := fixtureGit(upstream, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	runFixtureGit(t, upstream, "update-ref", "refs/pull/7/head", strings.TrimSpace(string(out)))
	runFixtureGit(t, upstream, "checkout", "-q", "main")
	runFixtureGit(t, upstream, "branch", "-q", "-D", "pr")

	checkout := filepath.Join(root, "checkout")
	require.NoError(t, os.MkdirAll(checkout, 0o755))
	runFixtureGit(t, checkout, "init", "-q", "-b", "main")
	runFixtureGit(t, checkout, "remote", "add", "origin", upstream)
	runFixtureGit(t, checkout, "fetch", "-q", "origin")
	runFixtureGit(t, checkout, "checkout", "-q", "-B", "main", "origin/main")
	return checkout
}

// whConfig is the webhook block the tests write under the project config.
func whConfig(checkout string) string {
	return "webhook:\n" +
		"  listen: 127.0.0.1:0\n" +
		"  job_timeout: 1m\n" +
		"  github_token_env: " + whGitHubTokenEnv + "\n" +
		"  repos:\n" +
		"    acme/widget: " + checkout + "\n" +
		"  slack:\n" +
		"    bot_token_env: " + whBotTokenEnv + "\n" +
		"    signing_secret_env: " + whSigningSecretEnv + "\n" +
		"    workspaces: [T1]\n" +
		"    channels: [C1]\n" +
		"    users: [U1]\n"
}

// writeWebhookConfig appends extra (a webhook block) to ./.atcr/config.yaml.
func writeWebhookConfig(t *testing.T, extra string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	path := filepath.Join(".atcr", "config.yaml")
	prev, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		require.NoError(t, err)
	}
	require.NoError(t, os.WriteFile(path, append(prev, []byte(extra)...), 0o644))
}

// slackPost is one chat.postMessage request the fake Slack server received.
type slackPost struct {
	Auth     string
	Channel  string `json:"channel"`
	ThreadTS string `json:"thread_ts"`
	Text     string `json:"text"`
}

// fakeSlack is an httptest stand-in for the Slack Web API.
func fakeSlack(t *testing.T) (*httptest.Server, <-chan slackPost) {
	t.Helper()
	posts := make(chan slackPost, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat.postMessage" {
			http.NotFound(w, r)
			return
		}
		var p slackPost
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &p)
		p.Auth = r.Header.Get("Authorization")
		posts <- p
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	return srv, posts
}

// startWebhook runs `atcr webhook` in the background and returns the base URL
// it listens on, its stderr, and a stop function that cancels it and returns
// the command's error.
func startWebhook(t *testing.T) (string, *lockedBuffer, func() error) {
	t.Helper()
	stderr := &lockedBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	root := NewRootCmd()
	root.SetArgs([]string{"webhook"})
	root.SetOut(io.Discard)
	root.SetErr(stderr)
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	listening := regexp.MustCompile(`listening on (http://127\.0\.0\.1:\d+)`)
	var base string
	require.Eventually(t, func() bool {
		select {
		case err := <-done:
			done <- err
			return true
		default:
		}
		if m := listening.FindStringSubmatch(stderr.String()); m != nil {
			base = m[1]
			return true
		}
		return false
	}, 10*time.Second, 10*time.Millisecond)
	stopped := false
	stop := func() error {
		if stopped {
			return nil
		}
		stopped = true
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(30 * time.Second):
			return fmt.Errorf("atcr webhook did not stop")
		}
	}
	t.Cleanup(func() { _ = stop() })
	require.NotEmpty(t, base, "atcr webhook exited before listening: %s", stderr.String())
	return base, stderr, stop
}

// postSignedMention sends a signed app_mention event, as Slack would.
func postSignedMention(t *testing.T, base, eventID, text string) {
	t.Helper()
	body := fmt.Sprintf(`{"type":"event_callback","team_id":"T1","event_id":%q,"event":{"type":"app_mention","user":"U1","channel":"C1","text":%q,"ts":"100.1","thread_ts":"99.9"}}`, eventID, text)
	req, err := http.NewRequest(http.MethodPost, base+webhook.SlackEventsPath, strings.NewReader(body))
	require.NoError(t, err)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(whSigningSecret))
	mac.Write([]byte("v0:" + ts + ":" + body))
	req.Header.Set(slack.HeaderTimestamp, ts)
	req.Header.Set(slack.HeaderSignature, "v0="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func setWebhookEnv(t *testing.T, slackURL string) {
	t.Helper()
	t.Setenv(whSigningSecretEnv, whSigningSecret)
	t.Setenv(whBotTokenEnv, whBotToken)
	t.Setenv(whGitHubTokenEnv, "ghp_testtokentesttokentesttoken12")
	orig := slackAPIBaseURL
	slackAPIBaseURL = slackURL
	t.Cleanup(func() { slackAPIBaseURL = orig })
}

func waitPost(t *testing.T, posts <-chan slackPost, stderr *lockedBuffer) slackPost {
	t.Helper()
	select {
	case p := <-posts:
		return p
	case <-time.After(60 * time.Second):
		t.Fatalf("no Slack reply; daemon stderr:\n%s", stderr.String())
		return slackPost{}
	}
}

func TestWebhookCmd_HelpListsCommand(t *testing.T) {
	var out bytes.Buffer
	root := NewRootCmd()
	root.SetArgs([]string{"--help"})
	root.SetOut(&out)
	require.NoError(t, root.Execute())
	assert.Regexp(t, `(?m)^\s+webhook\s+\S`, out.String(), "root help lists `atcr webhook`")

	out.Reset()
	root = NewRootCmd()
	root.SetArgs([]string{"webhook", "--help"})
	root.SetOut(&out)
	require.NoError(t, root.Execute())
	help := out.String()
	assert.Contains(t, help, "atcr webhook")
	assert.Contains(t, help, "127.0.0.1:8787", "help names the loopback default")
	assert.Contains(t, help, "docs/webhook.md")
}

// TestWebhookCmd_EndToEnd_SignedMentionGetsThreadReply is the T7 done-when: a
// signed app_mention reaches the running daemon, the real review runs against
// a mock model provider in a worktree of the PR head, and the reply lands in
// the mention's thread on the fake Slack server. Nothing is left behind.
func TestWebhookCmd_EndToEnd_SignedMentionGetsThreadReply(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	checkout := whPRFixture(t)
	provider := liveMockProvider(t)
	liveReviewConfig(t, provider.URL, "bruce")
	writeWebhookConfig(t, whConfig(checkout))
	slackSrv, posts := fakeSlack(t)
	setWebhookEnv(t, slackSrv.URL)

	base, stderr, stop := startWebhook(t)
	postSignedMention(t, base, "Ev1", "<@B1> review <https://github.com/acme/widget/pull/7>")

	p := waitPost(t, posts, stderr)
	assert.Equal(t, "Bearer "+whBotToken, p.Auth)
	assert.Equal(t, "C1", p.Channel)
	assert.Equal(t, "99.9", p.ThreadTS, "the reply goes to the mention's thread")
	assert.Contains(t, p.Text, "ATCR review of acme/widget#7")
	assert.Contains(t, p.Text, "1 CRITICAL")
	assert.Contains(t, p.Text, "Unchecked call")
	assert.NotContains(t, p.Text, "a.txt", "no path reaches Slack")
	assert.NotContains(t, p.Text, checkout)

	require.NoError(t, stop())
	worktrees, err := fixtureGit(checkout, "worktree", "list", "--porcelain").Output()
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(worktrees), "worktree "), "leftover worktree:\n%s", worktrees)
	branches, err := fixtureGit(checkout, "for-each-ref", "refs/heads/atcr-webhook/").Output()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(branches)), "leftover webhook branch")
	assert.NotContains(t, stderr.String(), whBotToken)
	assert.NotContains(t, stderr.String(), whSigningSecret)
}

// TestWebhookCmd_ReviewRunsInPRWorktreeWithOperatorConfig pins what the
// reviewer is handed: the PR head's tree, reviewed under the config of the
// directory the daemon was started in, never one the pull request carries.
func TestWebhookCmd_ReviewRunsInPRWorktreeWithOperatorConfig(t *testing.T) {
	isolate(t)
	checkout := whPRFixture(t)
	writeWebhookConfig(t, "agents: [bruce]\n"+whConfig(checkout))
	slackSrv, posts := fakeSlack(t)
	setWebhookEnv(t, slackSrv.URL)
	project, err := os.Getwd()
	require.NoError(t, err)

	var gotRoot, gotContent string
	orig := webhookReviewerFn
	webhookReviewerFn = func(_ context.Context, root string) webhook.Reviewer {
		gotRoot = root
		return func(_ context.Context, dir string) ([]reconcile.Finding, error) {
			b, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
			gotContent = string(b)
			return []reconcile.Finding{{Severity: "HIGH", Problem: "stub finding"}}, nil
		}
	}
	t.Cleanup(func() { webhookReviewerFn = orig })

	base, stderr, stop := startWebhook(t)
	postSignedMention(t, base, "Ev2", "<@B1> review https://github.com/acme/widget/pull/7")
	p := waitPost(t, posts, stderr)
	require.NoError(t, stop())

	assert.Equal(t, project, gotRoot)
	assert.Equal(t, "pr head\n", gotContent, "the review runs on the PR head")
	assert.Contains(t, p.Text, "1 HIGH")
}

func TestWebhookCmd_RefusesWithoutWebhookBlock(t *testing.T) {
	isolate(t)
	writeWebhookConfig(t, "agents: [bruce]\n")
	var stderr bytes.Buffer
	root := NewRootCmd()
	root.SetArgs([]string{"webhook"})
	root.SetOut(io.Discard)
	root.SetErr(&stderr)
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook:")
}

func TestWebhookCmd_RefusesWithoutSlackBlock(t *testing.T) {
	isolate(t)
	writeWebhookConfig(t, "agents: [bruce]\nwebhook:\n  repos:\n    acme/widget: /nonexistent/checkout\n")
	root := NewRootCmd()
	root.SetArgs([]string{"webhook"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook.slack")
}

// TestWebhookCmd_MissingCredentialNamesTheVariable checks an unset credential
// stops the daemon before it binds, naming the variable and never a value.
func TestWebhookCmd_MissingCredentialNamesTheVariable(t *testing.T) {
	for _, unset := range []string{whSigningSecretEnv, whBotTokenEnv} {
		t.Run(unset, func(t *testing.T) {
			isolate(t)
			writeWebhookConfig(t, "agents: [bruce]\n"+whConfig("/nonexistent/checkout"))
			setWebhookEnv(t, "http://127.0.0.1:1")
			t.Setenv(unset, "")
			root := NewRootCmd()
			root.SetArgs([]string{"webhook"})
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			err := root.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), unset)
			assert.NotContains(t, err.Error(), whSigningSecret)
			assert.NotContains(t, err.Error(), whBotToken)
		})
	}
}

// TestWebhookCmd_GitHubTokenIsOptional checks an unset github_token_env
// variable does not stop the daemon: fetching falls back to the checkout's own
// git credentials.
func TestWebhookCmd_GitHubTokenIsOptional(t *testing.T) {
	isolate(t)
	writeWebhookConfig(t, "agents: [bruce]\n"+whConfig("/nonexistent/checkout"))
	setWebhookEnv(t, "http://127.0.0.1:1")
	t.Setenv(whGitHubTokenEnv, "")
	_, _, stop := startWebhook(t)
	require.NoError(t, stop())
}

type whCtxKey struct{}

// TestJobContext_KeepsBaseValuesAndJobDeadline checks the review context
// carries the command's values but ends with the job: at its deadline as
// DeadlineExceeded, so the reply says "timed out", and on its cancellation.
func TestJobContext_KeepsBaseValuesAndJobDeadline(t *testing.T) {
	base, cancelBase := context.WithCancel(context.WithValue(context.Background(), whCtxKey{}, "v"))
	cancelBase() // the command context's own cancellation does not reach a job

	job, cancelJob := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelJob()
	ctx, cancel := jobContext(base, job)
	defer cancel()
	assert.Equal(t, "v", ctx.Value(whCtxKey{}))
	require.NoError(t, ctx.Err())
	<-ctx.Done()
	assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)

	job2, cancelJob2 := context.WithCancel(context.Background())
	ctx2, cancel2 := jobContext(base, job2)
	defer cancel2()
	_, hasDeadline := ctx2.Deadline()
	assert.False(t, hasDeadline)
	cancelJob2()
	select {
	case <-ctx2.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("job cancellation did not reach the review context")
	}
	assert.ErrorIs(t, ctx2.Err(), context.Canceled)
}

// TestWebhookDocStatesCredentialsAllowlistsAndLoopback is the docs half of the
// T7 done-when: docs/webhook.md names every credential config key (an
// environment-variable name, never a value), every allowlist and the loopback
// default.
func TestWebhookDocStatesCredentialsAllowlistsAndLoopback(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootDir(t), "docs", "webhook.md"))
	require.NoError(t, err)
	doc := string(b)
	for _, want := range []string{
		"bot_token_env", "signing_secret_env", "github_token_env", // credentials
		"workspaces", "channels", "users", "repos", // allowlists
		registry.DefaultWebhookListen, "loopback", // bind default
		"atcr webhook",
	} {
		assert.Contains(t, doc, want)
	}
}
