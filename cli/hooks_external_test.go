// Package cli_test proves the hook seam from outside the cli package: every cli
// identifier used here is exported, so a separate module such as the private
// atcr-enterprise wrapper can register a ReviewComplete observer the same way
// (Epic 41.0). The only internal import is gitexec, for the fixture repository;
// the repo-wide test gate requires every test git command to go through it, and
// a separate module would build its fixture with its own git.
package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/samestrin/atcr/cli"
	"github.com/samestrin/atcr/internal/gitexec"
	"github.com/stretchr/testify/require"
)

const externalKeyEnv = "ATCR_TEST_EXTERNAL_KEY"

// recordingObserver is what an embedding binary would register: it implements
// only the exported cli.ReviewCompleteObserver.
type recordingObserver struct {
	mu  sync.Mutex
	got []cli.ReviewReport
}

func (o *recordingObserver) OnReviewComplete(_ context.Context, rep cli.ReviewReport) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.got = append(o.got, rep)
}

func (o *recordingObserver) reports() []cli.ReviewReport {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]cli.ReviewReport(nil), o.got...)
}

// reviewFixture builds a hermetic workspace in a fresh temp dir: HOME points at
// its own temp dir, the working dir is a two-commit git repo, and the registry
// sends one agent to a mock OpenAI-shaped provider that returns one finding.
// It returns the base and head SHAs.
func reviewFixture(t *testing.T) (base, head string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(externalKeyEnv, "secret")
	t.Setenv("GITHUB_REF", "")
	wd := t.TempDir()
	t.Chdir(wd)

	git := func(args ...string) string {
		cmd := gitexec.FixtureCommandContext(context.Background(), wd, args...)
		cmd.Env = append(cmd.Env,
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	require.NoError(t, os.WriteFile("a.txt", []byte("one\n"), 0o644))
	git("add", "a.txt")
	git("commit", "-q", "-m", "init")
	require.NoError(t, os.WriteFile("a.txt", []byte("one\ntwo\n"), 0o644))
	git("add", "a.txt")
	git("commit", "-q", "-m", "second")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		content := "CRITICAL|a.txt:1|Unchecked call|Guard it|security|15|evidence"
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	reg := "providers:\n  p:\n    api_key_env: " + externalKeyEnv + "\n    base_url: " + srv.URL + "\n" +
		"agents:\n  bruce:\n    provider: p\n    model: m-bruce\n    persona: bruce\n"
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(reg), 0o644))
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"), []byte("agents: [bruce]\n"), 0o644))

	return git("rev-parse", "HEAD^"), git("rev-parse", "HEAD")
}

// setArgs points os.Args at an atcr invocation, since Main and MainWithHooks
// read their arguments from the process the way the stock binary does.
func setArgs(t *testing.T, args ...string) {
	t.Helper()
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	os.Args = append([]string{"atcr"}, args...)
}

func TestExternal_MainWithHooksFiresReviewComplete(t *testing.T) {
	base, head := reviewFixture(t)
	setArgs(t, "review", "--base", "HEAD^", "--pr", "42")

	obs := &recordingObserver{}
	var stdout, stderr bytes.Buffer
	code := cli.MainWithHooks(context.Background(), &stdout, &stderr, cli.Hooks{ReviewComplete: obs})
	require.Equal(t, 0, code, "stdout: %s\nstderr: %s", stdout.String(), stderr.String())

	got := obs.reports()
	require.Len(t, got, 1, "one call per successful review")
	rep := got[0]
	require.Equal(t, base, rep.Base)
	require.Equal(t, head, rep.Head)
	require.Equal(t, 42, rep.PRNumber)
	require.NotEmpty(t, rep.RunID)
	require.DirExists(t, rep.ReviewDir)
	require.Equal(t, 1, rep.AgentsSucceeded)
}

// elapsedLine is the one stdout line that varies between identical runs.
var elapsedLine = regexp.MustCompile(`Total elapsed: \S+`)

// A zero Hooks is the stock binary: the same review through Main and through
// MainWithHooks(Hooks{}), each in an identical fresh fixture, prints the same
// stdout (wall-clock time aside) and exits with the same code.
func TestExternal_ZeroHooksMatchesMain(t *testing.T) {
	run := func(t *testing.T, entry func(stdout, stderr io.Writer) int) (int, string) {
		reviewFixture(t)
		setArgs(t, "review", "--base", "HEAD^", "--id", "external-proof")
		var stdout, stderr bytes.Buffer
		code := entry(&stdout, &stderr)
		require.Equal(t, 0, code, "stderr: %s", stderr.String())
		return code, elapsedLine.ReplaceAllString(stdout.String(), "Total elapsed: <t>")
	}

	var mainCode, hooksCode int
	var mainOut, hooksOut string
	t.Run("Main", func(t *testing.T) {
		mainCode, mainOut = run(t, func(stdout, stderr io.Writer) int {
			return cli.Main(context.Background(), stdout, stderr)
		})
	})
	t.Run("MainWithHooks", func(t *testing.T) {
		hooksCode, hooksOut = run(t, func(stdout, stderr io.Writer) int {
			return cli.MainWithHooks(context.Background(), stdout, stderr, cli.Hooks{})
		})
	})

	require.NotEmpty(t, mainOut, "the review must print something for the comparison to mean anything")
	require.Equal(t, mainCode, hooksCode)
	require.Equal(t, mainOut, hooksOut)
}
