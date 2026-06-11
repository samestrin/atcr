package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitRepo creates a temp git repo with a base and head commit, returning the
// repo dir and the two full SHAs.
func gitRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	run("config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.go"), []byte("package main\n\nfunc a() {}\n"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "base")
	base = run("rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.go"), []byte("package main\n\nfunc a() { b() }\n\nfunc b() {}\n"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "head")
	head = run("rev-parse", "HEAD")
	return dir, base, head
}

// writeReviewConfig redirects HOME to a temp dir and writes a registry (one
// provider, two agents) plus a project config rostering them, so
// fanout.LoadReviewConfig resolves a valid two-agent roster hermetically.
func writeReviewConfig(t *testing.T, root string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	regYAML := `providers:
  p:
    api_key_env: ATCR_TEST_KEY
    base_url: https://example.invalid/v1
agents:
  greta:
    provider: p
    model: m-greta
  kai:
    provider: p
    model: m-kai
`
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(regYAML), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "config.yaml"),
		[]byte("agents:\n  - greta\n  - kai\npayload_mode: diff\n"), 0o644))
}

// validFindings is a fakeCompleter response in v1 model-output form (7 cols;
// the engine stamps REVIEWER), so a review produces one CRITICAL finding.
const validFindings = "CRITICAL|auth.go:3|Unchecked call to b()|Add a guard|security|15|b() unchecked"

// writeFindingsFile writes a v1 per-source findings.txt at path.
func writeFindingsFile(t *testing.T, path string, rows ...string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	body := "# atcr-findings/v1\n" + strings.Join(rows, "\n") + "\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

// reviewFixture builds a review directory with a host source and a pool-agent
// source that both report the same CRITICAL finding (so reconcile merges them to
// HIGH confidence), a manifest, a pool summary, and the .atcr/latest pointer.
// Returns the review id.
func reviewFixture(t *testing.T, root string) string {
	t.Helper()
	id := "2026-06-10_fix"
	dir := filepath.Join(root, ".atcr", "reviews", id)
	writeFindingsFile(t, filepath.Join(dir, "sources", "host", "findings.txt"),
		"CRITICAL|auth.go:3|Unchecked call to b()|Guard the call|security|15|b() unchecked|host")
	writeFindingsFile(t, filepath.Join(dir, "sources", "pool", "raw", "agent", "greta", "findings.txt"),
		"CRITICAL|auth.go:3|Unchecked call to b|Add a guard|security|15|b() unchecked|greta")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"aaa","head":"bbb","roster":["greta"],"partial":false}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sources", "pool", "summary.json"),
		[]byte(`{"total":1,"succeeded":1,"failed":0,"partial":false,"total_findings":1}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte(id+"\n"), 0o644))
	return id
}

// pollStatus polls atcr_status until the review leaves the in_progress state
// (the background fan-out writes summary.json on completion).
func pollStatus(t *testing.T, cs *mcpsdk.ClientSession, id string) StatusResult {
	t.Helper()
	for i := 0; i < 400; i++ {
		st := callOK[StatusResult](t, cs, ToolStatus, map[string]any{"id_or_path": id})
		if st.Status != fanout.RunInProgress {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("review %s did not complete", id)
	return StatusResult{}
}

// --- atcr_review ---------------------------------------------------------

func TestReviewHandler_ExplicitBaseHead(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	root, base, head := gitRepo(t)
	writeReviewConfig(t, root)
	cs := connectTest(t, root, fakeCompleter{resp: validFindings})

	out := callOK[ReviewResult](t, cs, ToolReview, map[string]any{"base": base, "head": head})
	assert.Equal(t, runningStatus, out.Status)
	assert.Equal(t, 2, out.AgentCount)
	assert.NotEmpty(t, out.ReviewID)
	assert.DirExists(t, out.ReviewPath)

	// Fan-out runs in the background; poll status until it completes.
	st := pollStatus(t, cs, out.ReviewID)
	assert.Equal(t, fanout.RunCompleted, st.Status)
	assert.FileExists(t, filepath.Join(out.ReviewPath, "sources", "pool", "raw", "agent", "greta", "findings.txt"))
}

func TestReviewHandler_NoGitRepo(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	msg := callErr(t, cs, ToolReview, map[string]any{})
	assert.Contains(t, msg, "not a git repository")
}

// TestReviewHandler_EmptyRangeErrors verifies an empty range is rejected before
// any scaffolding/fan-out (it must NOT silently scaffold a no-op review).
func TestReviewHandler_EmptyRangeErrors(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	root, _, head := gitRepo(t)
	writeReviewConfig(t, root)
	cs := connectTest(t, root, fakeCompleter{resp: validFindings})
	msg := callErr(t, cs, ToolReview, map[string]any{"base": head, "head": head})
	assert.Contains(t, msg, "nothing to review")
	assert.NoDirExists(t, filepath.Join(root, ".atcr", "reviews"), "no review dir for an empty range")
}

// TestReviewHandler_MergeCommitWithBaseHeadRejected verifies the argument
// combination the CLI forbids in validateRangeFlags is also rejected at the MCP
// layer instead of silently ignoring merge_commit (explicit base/head wins in
// gitrange.Resolve), and that the error speaks json field names, not CLI flags.
func TestReviewHandler_MergeCommitWithBaseHeadRejected(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	root, base, head := gitRepo(t)
	writeReviewConfig(t, root)
	cs := connectTest(t, root, fakeCompleter{resp: validFindings})
	msg := callErr(t, cs, ToolReview, map[string]any{"base": base, "head": head, "merge_commit": head})
	assert.Contains(t, msg, "merge_commit")
	assert.NotContains(t, msg, "--base", "error must use json field names, not CLI flags")
	assert.NoDirExists(t, filepath.Join(root, ".atcr", "reviews"), "no review dir for rejected args")
}

// --- atcr_reconcile ------------------------------------------------------

func TestReconcileHandler_LatestMergesToHighConfidence(t *testing.T) {
	root := t.TempDir()
	reviewFixture(t, root)
	cs := connectTest(t, root, fakeCompleter{})

	out := callOK[ReconcileResult](t, cs, ToolReconcile, map[string]any{})
	assert.True(t, out.Pass, "no fail_on threshold means pass")
	assert.Equal(t, 1, out.TotalFindings, "the two CRITICAL findings merge into one")
}

func TestReconcileHandler_FailOnGate(t *testing.T) {
	root := t.TempDir()
	reviewFixture(t, root)
	cs := connectTest(t, root, fakeCompleter{})

	out := callOK[ReconcileResult](t, cs, ToolReconcile, map[string]any{"fail_on": "HIGH"})
	assert.False(t, out.Pass, "a surviving CRITICAL fails a HIGH gate")
	require.NotEmpty(t, out.Findings, "failing findings are returned inline")
	assert.Equal(t, "CRITICAL", out.Findings[0].Severity)
}

func TestReconcileHandler_InvalidFailOn(t *testing.T) {
	root := t.TempDir()
	reviewFixture(t, root)
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolReconcile, map[string]any{"fail_on": "SEVERE"})
	assert.Contains(t, msg, "invalid severity")
}

func TestReconcileHandler_NoResults(t *testing.T) {
	root := t.TempDir()
	id := "2026-06-10_empty"
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr", "reviews", id, "sources"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte(id+"\n"), 0o644))
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolReconcile, map[string]any{})
	assert.Contains(t, msg, "no agent results found")
}

// --- atcr_report ---------------------------------------------------------

func TestReportHandler_Formats(t *testing.T) {
	root := t.TempDir()
	id := reviewFixture(t, root)
	cs := connectTest(t, root, fakeCompleter{})
	// Reconcile first so reconciled/findings.json exists.
	callOK[ReconcileResult](t, cs, ToolReconcile, map[string]any{})

	md := callOK[ReportResult](t, cs, ToolReport, map[string]any{"id_or_path": id})
	assert.Equal(t, "md", md.Format)
	assert.Contains(t, md.Content, "atcr Review Report")

	js := callOK[ReportResult](t, cs, ToolReport, map[string]any{"format": "json"})
	assert.Equal(t, "json", js.Format)
	assert.Contains(t, js.Content, "\"severity\"")

	cl := callOK[ReportResult](t, cs, ToolReport, map[string]any{"format": "checklist"})
	assert.Contains(t, cl.Content, "- [ ]")
}

func TestReportHandler_InvalidFormatRejected(t *testing.T) {
	root := t.TempDir()
	reviewFixture(t, root)
	cs := connectTest(t, root, fakeCompleter{})
	callOK[ReconcileResult](t, cs, ToolReconcile, map[string]any{})
	// xml is not in the enum: the schema rejects it before the handler runs
	// (transport-level error); the handler's own enum check is the in-process
	// backstop. Accept either surfacing.
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: ToolReport, Arguments: map[string]any{"format": "xml"}})
	// The schema enum rejects it (tool-level error result); the message names the
	// rejected value. Either a transport error or an IsError result is acceptable.
	if err == nil {
		require.True(t, res.IsError, "invalid format must be rejected")
		assert.Contains(t, contentText(res), "xml")
	}
}

func TestReportHandler_NoReconciliation(t *testing.T) {
	root := t.TempDir()
	id := reviewFixture(t, root) // sources only, no reconciled/
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolReport, map[string]any{"id_or_path": id})
	assert.Contains(t, msg, "run atcr_reconcile first")
}

// --- atcr_range ----------------------------------------------------------

func TestRangeHandler_Explicit(t *testing.T) {
	root, base, head := gitRepo(t)
	cs := connectTest(t, root, fakeCompleter{})
	out := callOK[RangeResult](t, cs, ToolRange, map[string]any{"base": base, "head": head})
	assert.Equal(t, 1, out.CommitCount)
	assert.Equal(t, 1, out.FileCount)
}

func TestRangeHandler_EmptyDiffNoError(t *testing.T) {
	root, _, head := gitRepo(t)
	cs := connectTest(t, root, fakeCompleter{})
	out := callOK[RangeResult](t, cs, ToolRange, map[string]any{"base": head, "head": head})
	assert.Equal(t, 0, out.CommitCount)
	assert.Equal(t, 0, out.FileCount)
}

func TestRangeHandler_NoGit(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	msg := callErr(t, cs, ToolRange, map[string]any{})
	assert.Contains(t, msg, "not a git repository")
}

// TestRangeHandler_BaseWithoutHeadRejected verifies the base/head pairing rule
// the CLI enforces in validateRangeFlags is also enforced at the MCP layer, in
// json field vocabulary.
func TestRangeHandler_BaseWithoutHeadRejected(t *testing.T) {
	root, base, _ := gitRepo(t)
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolRange, map[string]any{"base": base})
	assert.Contains(t, msg, "base and head must be provided together")
	assert.NotContains(t, msg, "--base", "error must use json field names, not CLI flags")
}

// TestRangeHandler_MergeCommitWithHeadRejected verifies merge_commit combined
// with head is rejected with an error phrased in the MCP json arg vocabulary —
// not the "--base and --head must be provided together" CLI flag wording that
// gitrange surfaces when the first decision-tree branch fires.
func TestRangeHandler_MergeCommitWithHeadRejected(t *testing.T) {
	root, _, head := gitRepo(t)
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolRange, map[string]any{"head": head, "merge_commit": head})
	assert.Contains(t, msg, "merge_commit")
	assert.NotContains(t, msg, "--base", "error must use json field names, not CLI flags")
}

// --- atcr_status ---------------------------------------------------------

func TestStatusHandler_Completed(t *testing.T) {
	root := t.TempDir()
	id := reviewFixture(t, root)
	cs := connectTest(t, root, fakeCompleter{})
	out := callOK[StatusResult](t, cs, ToolStatus, map[string]any{"id_or_path": id})
	assert.Equal(t, fanout.RunCompleted, out.Status)
	assert.Equal(t, 1, out.AgentsDone)
}

func TestStatusHandler_NoReviews(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	msg := callErr(t, cs, ToolStatus, map[string]any{})
	assert.Contains(t, msg, "no reviews found")
}

func TestStatusHandler_CorruptManifest(t *testing.T) {
	root := t.TempDir()
	id := "2026-06-10_corrupt"
	dir := filepath.Join(root, ".atcr", "reviews", id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{not json"), 0o644))
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolStatus, map[string]any{"id_or_path": id})
	assert.Contains(t, msg, "corrupt")
}

func TestStatusHandler_PathContainment(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	msg := callErr(t, cs, ToolStatus, map[string]any{"id_or_path": "../../etc/passwd"})
	assert.Contains(t, msg, "invalid review id")
}
