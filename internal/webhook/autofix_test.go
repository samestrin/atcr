package webhook

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// testPushToken is the push token's value; it must reach the child only
	// through its environment, never an argument or a log line.
	testPushToken = "ghp_PushTokenValue0123456789abcdefABCDEF"
	// testPushTokenEnv is the daemon variable that holds it.
	testPushTokenEnv = "ATCR_TEST_AUTOFIX_PUSH_TOKEN"
)

// autoFixStub is a stand-in `atcr` binary: a shell script that records its
// arguments, working directory, GITHUB_TOKEN and the reviewed tree's own
// .atcr/config.yaml under rec, then runs body.
type autoFixStub struct {
	path string
	rec  string
}

func newAutoFixStub(t *testing.T, body string) autoFixStub {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub atcr binary is a POSIX shell script")
	}
	dir := t.TempDir()
	rec := filepath.Join(dir, "rec")
	require.NoError(t, os.MkdirAll(rec, 0o755))
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do printf '%%s\n' "$a"; done > %[1]q/args
pwd -P > %[1]q/pwd
printf '%%s' "$GITHUB_TOKEN" > %[1]q/token
cat .atcr/config.yaml > %[1]q/tree-config 2>/dev/null
%[2]s
`, rec, body)
	path := filepath.Join(dir, "atcr")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return autoFixStub{path: path, rec: rec}
}

func (s autoFixStub) read(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(s.rec, name))
	require.NoError(t, err)
	return string(body)
}

func (s autoFixStub) ran() bool {
	_, err := os.Stat(filepath.Join(s.rec, "args"))
	return err == nil
}

// logLines collects Logf lines; the child's output arrives on another goroutine.
type logLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *logLines) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logLines) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

const (
	prHeadConfig   = "auto_fix:\n  validate_command: \"pr-head-command\"\n"
	operatorConfig = "auto_fix:\n  validate_command: \"operator-command\"\n"
)

// newAutoFixFixture is a PR whose head carries its own .atcr/config.yaml, and
// an operator checkout whose .atcr/config.yaml sets a different command.
func newAutoFixFixture(t *testing.T) prFixture {
	t.Helper()
	f := newPRFixtureWith(t, map[string]string{".atcr/config.yaml": prHeadConfig})
	require.NoError(t, os.MkdirAll(filepath.Join(f.checkout, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, ".atcr", "config.yaml"), []byte(operatorConfig), 0o644))
	return f
}

func newAutoFixRunner(f prFixture, stub autoFixStub, logs *logLines) *AutoFixRunner {
	return &AutoFixRunner{
		Repos:        f.repos(),
		Scratch:      f.scratch,
		Binary:       stub.path,
		PushTokenEnv: testPushTokenEnv,
		Logf:         logs.logf,
	}
}

func assertNoToken(t *testing.T, logs *logLines, err error) {
	t.Helper()
	for _, line := range logs.all() {
		assert.NotContains(t, line, testPushToken, "push token in log line")
	}
	if err != nil {
		assert.NotContains(t, err.Error(), testPushToken, "push token in returned error")
	}
}

func TestAutoFixRunner_RunsAtcrWithOperatorConfigDir(t *testing.T) {
	f := newAutoFixFixture(t)
	t.Setenv(testPushTokenEnv, testPushToken)
	// The daemon's own GITHUB_TOKEN (a review token) must not reach the child.
	t.Setenv("GITHUB_TOKEN", "ghp_ReviewTokenNotForPushing0123456789")
	stub := newAutoFixStub(t, `echo "pushing with $GITHUB_TOKEN"; echo "stderr $GITHUB_TOKEN" >&2; exit 0`)
	logs := &logLines{}

	err := newAutoFixRunner(f, stub, logs).Run(context.Background(), AutoFixTarget{Repo: "acme/widget", Number: 7, Base: "feature/login"})
	require.NoError(t, err)

	args := strings.Split(strings.TrimSuffix(stub.read(t, "args"), "\n"), "\n")
	assert.Equal(t, []string{
		"review",
		"--auto-fix",
		"--auto-fix-base=feature/login",
		"--config-dir=" + f.checkout,
		"--repo=acme/widget",
	}, args)
	for _, a := range args {
		assert.NotContains(t, a, testPushToken, "push token in an argument")
	}

	// The child ran in the PR-head worktree, whose own config differs from the
	// operator's: only --config-dir points it at the operator's.
	scratch, err := filepath.EvalSymlinks(f.scratch)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(strings.TrimSpace(stub.read(t, "pwd")), scratch+string(filepath.Separator)), "child ran outside the worktree")
	assert.Equal(t, prHeadConfig, stub.read(t, "tree-config"))
	assert.NotEqual(t, prHeadConfig, operatorConfig)

	assert.Equal(t, testPushToken, stub.read(t, "token"), "the push token reaches the child as GITHUB_TOKEN")

	all := strings.Join(logs.all(), "\n")
	assert.Contains(t, all, "pushing with [redacted]")
	assert.Contains(t, all, "stderr [redacted]")
	assertNoToken(t, logs, nil)
	assertNothingLeft(t, f)
}

func TestAutoFixRunner_CancelledContextRemovesWorktreeAndBranch(t *testing.T) {
	f := newAutoFixFixture(t)
	t.Setenv(testPushTokenEnv, testPushToken)
	stub := newAutoFixStub(t, `echo "token $GITHUB_TOKEN"; touch `+fmt.Sprintf("%q", "started")+`; exec sleep 30`)
	logs := &logLines{}
	r := newAutoFixRunner(f, stub, logs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer cancel()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if stub.ran() && hasStarted(f) {
				return
			}
		}
	}()
	start := time.Now()
	err := r.Run(ctx, AutoFixTarget{Repo: "acme/widget", Number: 7, Base: "feature/login"})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), 20*time.Second, "the child outlived the cancellation")
	assertNoToken(t, logs, err)
	assertNothingLeft(t, f)
}

// hasStarted reports whether a stub has touched "started" in its worktree.
func hasStarted(f prFixture) bool {
	matches, _ := filepath.Glob(filepath.Join(f.scratch, "*", "tree", "started"))
	return len(matches) > 0
}

func TestAutoFixRunner_TimeoutKillsChildIgnoringSIGTERM(t *testing.T) {
	f := newAutoFixFixture(t)
	t.Setenv(testPushTokenEnv, testPushToken)
	old := autoFixKillGrace
	autoFixKillGrace = 200 * time.Millisecond
	t.Cleanup(func() { autoFixKillGrace = old })
	stub := newAutoFixStub(t, `trap '' TERM; exec sleep 30`)
	logs := &logLines{}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := newAutoFixRunner(f, stub, logs).Run(ctx, AutoFixTarget{Repo: "acme/widget", Number: 7, Base: "feature/login"})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 20*time.Second, "a child ignoring SIGTERM was not killed")
	assertNothingLeft(t, f)
}

func TestAutoFixRunner_FailedRunReturnsErrorWithoutToken(t *testing.T) {
	f := newAutoFixFixture(t)
	t.Setenv(testPushTokenEnv, testPushToken)
	stub := newAutoFixStub(t, `echo "auto-fix: refused, token $GITHUB_TOKEN" >&2; exit 3`)
	logs := &logLines{}

	err := newAutoFixRunner(f, stub, logs).Run(context.Background(), AutoFixTarget{Repo: "acme/widget", Number: 7, Base: "feature/login"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "acme/widget#7")
	assert.Contains(t, strings.Join(logs.all(), "\n"), "auto-fix: refused, token [redacted]")
	assertNoToken(t, logs, err)
	assertNothingLeft(t, f)
}

func TestAutoFixRunner_RefusesBeforeCheckout(t *testing.T) {
	f := newAutoFixFixture(t)
	stub := newAutoFixStub(t, `exit 0`)
	target := AutoFixTarget{Repo: "acme/widget", Number: 7, Base: "feature/login"}

	cases := []struct {
		name   string
		env    string
		mutate func(*AutoFixRunner, *AutoFixTarget)
		want   error
	}{
		{name: "push token unset", want: ErrNoPushToken},
		{name: "push token blank", env: "  ", want: ErrNoPushToken},
		{name: "no push token env name", env: testPushToken, mutate: func(r *AutoFixRunner, _ *AutoFixTarget) { r.PushTokenEnv = "" }, want: ErrNoPushToken},
		{name: "no base", env: testPushToken, mutate: func(_ *AutoFixRunner, tg *AutoFixTarget) { tg.Base = " " }, want: ErrNoAutoFixBase},
		{name: "repo not allowlisted", env: testPushToken, mutate: func(_ *AutoFixRunner, tg *AutoFixTarget) { tg.Repo = "acme/other" }, want: ErrRepoNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env == "" {
				t.Setenv(testPushTokenEnv, "")
				require.NoError(t, os.Unsetenv(testPushTokenEnv))
			} else {
				t.Setenv(testPushTokenEnv, tc.env)
			}
			logs := &logLines{}
			r := newAutoFixRunner(f, stub, logs)
			tg := target
			if tc.mutate != nil {
				tc.mutate(r, &tg)
			}
			err := r.Run(context.Background(), tg)
			assert.True(t, errors.Is(err, tc.want), "got %v, want %v", err, tc.want)
			assert.False(t, stub.ran(), "atcr ran despite the refusal")
			assertNoToken(t, logs, err)
			assertNothingLeft(t, f)
		})
	}
}

func TestAutoFixLogWriter_SplitsRedactsAndCapsLines(t *testing.T) {
	logs := &logLines{}
	w := newAutoFixLogWriter("acme/widget#7", []string{testPushToken}, logs.logf)
	_, _ = w.Write([]byte("one " + testPushToken[:10]))
	_, _ = w.Write([]byte(testPushToken[10:] + " end\ntwo\n\nthree\n"))
	huge := strings.Repeat("x", autoFixLogChunk+10) + testPushToken + "\nafter\n"
	_, _ = w.Write([]byte(huge))
	w.Flush()

	lines := logs.all()
	require.Len(t, lines, 5, "%q", lines)
	assert.Equal(t, "webhook: auto-fix acme/widget#7: one [redacted] end", lines[0])
	assert.Equal(t, "webhook: auto-fix acme/widget#7: two", lines[1])
	assert.Equal(t, "webhook: auto-fix acme/widget#7: three", lines[2])
	assert.LessOrEqual(t, len([]rune(lines[3])), len("webhook: auto-fix acme/widget#7: ")+autoFixLogLineRunes)
	assert.True(t, strings.HasSuffix(lines[3], "…"), "a long line is marked truncated")
	assert.Equal(t, "webhook: auto-fix acme/widget#7: after", lines[4])
	assertNoToken(t, logs, nil)
}
