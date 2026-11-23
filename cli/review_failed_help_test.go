package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// switchableProvider fails every request with a 400 until ok is set, then
// replies with one LOW finding.
func switchableProvider(t *testing.T) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	ok := &atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if !ok.Load() {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		content := "LOW|a.txt:1|Nit|Tidy it|style|5|evidence"
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, ok
}

// latestReviewID returns the id .atcr/latest points at.
func latestReviewID(t *testing.T) string {
	t.Helper()
	dir, err := anchorDir("")
	require.NoError(t, err)
	return filepath.Base(dir)
}

// TestReviewCmd_FailedRangeReviewSuggestsRunnableResume (EST-8140): a review of
// an explicit range whose agents all fail still ends its --axi output with a
// help[] block, and the resume it and status suggest carries the recorded range,
// so the suggested command runs where a bare `review --resume <id>` exits 2.
func TestReviewCmd_FailedRangeReviewSuggestsRunnableResume(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	// A third commit, so the reviewed range (HEAD~2..HEAD~1) is not the one a
	// bare resume would re-derive.
	require.NoError(t, os.WriteFile("b.txt", []byte("three\n"), 0o644))
	gitRun(t, "add", "b.txt")
	gitRun(t, "commit", "-qm", "third")
	base, head := gitRevParse(t, "HEAD~2"), gitRevParse(t, "HEAD~1")
	srv, ok := switchableProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	resume := "atcr review --resume <id> --base=" + base + " --head=" + head

	code, stdout, stderr := execCmdSplit(t, "review", "--axi", "--base", "HEAD~2", "--head", "HEAD~1")
	require.Equal(t, 1, code, stderr)
	assert.True(t, strings.HasSuffix(stdout, "help[2]: atcr status <id>,"+resume+"\n"), "got:\n%s", stdout)
	id := latestReviewID(t)

	code, stdout, stderr = execCmdSplit(t, "status", id, "--axi")
	require.Equal(t, 0, code, stderr)
	assert.True(t, strings.HasSuffix(stdout, "help[1]: "+resume+"\n"), "got:\n%s", stdout)

	ok.Store(true)
	// A bare resume re-derives a different range and fails, naming the fix.
	root := NewRootCmd()
	root.SetArgs([]string{"review", "--resume", id})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.ExecuteContext(context.Background())
	require.Equal(t, 2, exitCode(err))
	assert.Contains(t, err.Error(), "--base "+base+" --head "+head)

	// The suggested resume runs.
	args := strings.Fields(strings.ReplaceAll(resume, idPlaceholder, id))[1:]
	code, _, stderr = execCmdSplit(t, args...)
	assert.Equal(t, 0, code, stderr)
}

// TestReviewCmd_FailedReviewHumanNextLines (EST-8140): the human-mode failure
// gets the same next steps as Next: lines on stderr.
func TestReviewCmd_FailedReviewHumanNextLines(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv, _ := switchableProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, _, stderr := execCmdSplit(t, "review", "--base", "HEAD^")
	require.Equal(t, 1, code, stderr)
	assert.Contains(t, stderr, "Next: atcr status <id>\nNext: atcr review --resume <id> --base=")
}
