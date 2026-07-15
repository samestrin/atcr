package mcp

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/report"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewHandler_AttachesReviewIDToFanoutContext verifies the MCP review path
// seeds the detached fan-out context with the server logger tagged by review_id,
// so AC9 (every review log line carries review_id) holds under `atcr serve`,
// matching the CLI path. Phase 4 fan-out reads this logger via log.FromContext.
func TestReviewHandler_AttachesReviewIDToFanoutContext(t *testing.T) {
	var buf bytes.Buffer
	logger, err := log.New("info", "text", &buf)
	require.NoError(t, err)
	e := &engine{log: logger}

	ctx := e.reviewContext(context.Background(), "2026-06-17_rid")
	log.FromContext(ctx).Info("fan-out line")

	assert.Contains(t, buf.String(), "review_id=2026-06-17_rid",
		"the detached fan-out context must carry the server logger tagged with review_id")
}

// TestReviewContext_RedactsSecretsAndPaths verifies the serve-mode fan-out
// context enforces sink-level redaction (AC5 secret scrub, AC6 path
// relativization) even with the default root ".", which reviewContext resolves
// to an absolute path so paths under it relativize.
func TestReviewContext_RedactsSecretsAndPaths(t *testing.T) {
	var buf bytes.Buffer
	logger, err := log.New("info", "text", &buf)
	require.NoError(t, err)
	e := &engine{log: logger, root: "."} // serve-mode default root

	ctx := e.reviewContext(context.Background(), "rid-1")

	cwd, err := filepath.Abs(".")
	require.NoError(t, err)
	abs := filepath.Join(cwd, "internal", "secret.go")
	log.FromContext(ctx).Info("loaded "+abs, "key", "sk-leakedvalue123")

	out := buf.String()
	assert.Contains(t, out, "review_id=rid-1")
	assert.NotContains(t, out, "sk-leakedvalue123", "secret must be redacted at the sink (AC5)")
	assert.NotContains(t, out, cwd+string(filepath.Separator), "absolute root must be relativized (AC6)")
	assert.Contains(t, out, filepath.Join("internal", "secret.go"), "path must render relative to root")
}

// TestReviewContext_ScrubsConfiguredNonSkKey is the serve-mode half of epic 4.9
// AC2: a non-sk-/non-Bearer-shaped provider key (Google AIzaSy…) resolved from
// the prepared review's slots and threaded into reviewContext is scrubbed at the
// sink. Before 4.9 reviewContext passed no secrets to NewRedactor, so this key —
// lacking the sk-/Bearer token shapes — would leak verbatim in serve-mode logs.
func TestReviewContext_ScrubsConfiguredNonSkKey(t *testing.T) {
	const key = "AIzaSyServeModeNonSkKeyValue1234567890"
	t.Setenv("ATCR_MCP_REDACT_KEY", key)

	var buf bytes.Buffer
	logger, err := log.New("info", "json", &buf)
	require.NoError(t, err)
	e := &engine{log: logger, root: "."}

	prep := &fanout.PreparedReview{
		ID:    "2026-06-20_serve",
		Slots: []fanout.Slot{{Primary: fanout.Agent{Invocation: llmclient.Invocation{APIKeyEnv: "ATCR_MCP_REDACT_KEY"}}}},
	}
	secrets, _ := prep.SecretValues()
	ctx := e.reviewContext(context.Background(), prep.ID, secrets...)
	log.FromContext(ctx).Info("provider rejected request", "header", "x-goog-api-key: "+key)

	out := buf.String()
	assert.Contains(t, out, "2026-06-20_serve", "review_id must stay greppable (AC9)")
	assert.NotContains(t, out, key, "AC2: a configured non-sk provider key must be scrubbed in serve-mode logs")
	assert.Contains(t, out, "[redacted]", "AC2: the scrubbed key must be replaced with the redaction marker")
}

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

// inProgressFixture builds a review whose fan-out is still running: manifest
// and a host source exist, but the pool summary.json (the completion signal)
// has not been written. Returns the review id.
func inProgressFixture(t *testing.T, root string) string {
	t.Helper()
	id := "2026-06-10_running"
	dir := filepath.Join(root, ".atcr", "reviews", id)
	writeFindingsFile(t, filepath.Join(dir, "sources", "host", "findings.txt"),
		"CRITICAL|auth.go:3|Unchecked call to b()|Guard the call|security|15|b() unchecked|host")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"aaa","head":"bbb","roster":["greta"],"partial":false}`), 0o644))
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

// TestReviewHandler_ExplicitIDCollisionUsesFlagNeutralMessage verifies that when
// an MCP client re-sends atcr_review with an explicit id whose review directory
// already exists, the collision error does NOT name the CLI-only --resume/--force
// flags (MCP clients have none) but still names the id so the client can recover.
func TestReviewHandler_ExplicitIDCollisionUsesFlagNeutralMessage(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	root, base, head := gitRepo(t)
	writeReviewConfig(t, root)
	_, eng, err := buildServer(root, fakeCompleter{resp: validFindings}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { eng.drain(2 * time.Second) })

	const id = "fixed-id"
	_, res, err := eng.handleReview(context.Background(), nil, ReviewArgs{Base: base, Head: head, ID: id})
	require.NoError(t, err)
	require.Equal(t, runningStatus, res.Status)

	// Second call with the same explicit id collides at ScaffoldReviewDir.
	_, _, err = eng.handleReview(context.Background(), nil, ReviewArgs{Base: base, Head: head, ID: id})
	require.Error(t, err)
	assert.Contains(t, err.Error(), id, "collision error must name the id so the client can recover")
	assert.NotContains(t, err.Error(), "--resume", "MCP error must not name CLI-only flags")
	assert.NotContains(t, err.Error(), "--force", "MCP error must not name CLI-only flags")
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

// isolateUserConfig redirects HOME/XDG so file-tier config probes (the
// user-global registry) never read the developer's real ~/.config/atcr.
func isolateUserConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

// --- atcr_reconcile ------------------------------------------------------

func TestReconcileHandler_LatestMergesToHighConfidence(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	reviewFixture(t, root)
	cs := connectTest(t, root, fakeCompleter{})

	out := callOK[ReconcileResult](t, cs, ToolReconcile, map[string]any{})
	assert.True(t, out.Pass, "no fail_on threshold means pass")
	assert.Equal(t, 1, out.TotalFindings, "the two CRITICAL findings merge into one")
}

// TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag locks Epic 3.4
// AC4: handleReconcile must pass the engine's diagnostics sink (engine.diag) into
// EmitOpts.Diag, not the process-global os.Stderr. Forcing a scorecard
// write-failure — the resolved store path is a regular file, so Append's
// MkdirAll(dir) fails — makes Emit write its "scorecard: write
// failed" diagnostic; with the writer wired, that diagnostic lands in the
// injected buffer. A regression passing os.Stderr (or nil) routes it to the real
// stderr and fails this test. The scorecard failure is best-effort, so it must
// NOT fail the reconcile. The write-failure is induced cross-platform: Go's
// os.MkdirAll returns ENOTDIR on any OS when a regular file occupies the target
// path, and the test asserts only the diagnostic + exit 0, never the errno (cf.
// store.go TD-004 for the genuinely POSIX-specific O_APPEND append caveat).
func TestReconcileHandler_ScorecardDiagnosticRoutesToInjectedDiag(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	reviewFixture(t, root)

	storeDir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(storeDir), 0o755))
	require.NoError(t, os.WriteFile(storeDir, []byte("x"), 0o600))

	var buf bytes.Buffer
	e := &engine{root: root, diag: &buf}
	_, _, err = e.handleReconcile(context.Background(), nil, ReconcileArgs{})
	require.NoError(t, err, "a best-effort scorecard failure must not fail the reconcile")
	require.Contains(t, buf.String(), scorecard.MsgWriteFailed,
		"handleReconcile must wire engine.diag into EmitOpts.Diag")
}

func TestReconcileHandler_FailOnGate(t *testing.T) {
	isolateUserConfig(t) // a successful reconcile now emits a scorecard; keep it out of real ~/.config
	root := t.TempDir()
	reviewFixture(t, root)
	cs := connectTest(t, root, fakeCompleter{})

	out := callOK[ReconcileResult](t, cs, ToolReconcile, map[string]any{"fail_on": "HIGH"})
	assert.False(t, out.Pass, "a surviving CRITICAL fails a HIGH gate")
	require.NotEmpty(t, out.Findings, "failing findings are returned inline")
	assert.Equal(t, "CRITICAL", out.Findings[0].Severity)
}

// TestReconcileHandler_ProjectConfigFailOnGatesByDefault verifies the MCP
// layer honors the same file-tier gate precedence as the CLI: with no explicit
// fail_on argument, a project config fail_on gates the reconcile instead of
// silently reporting pass=true.
func TestReconcileHandler_ProjectConfigFailOnGatesByDefault(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	reviewFixture(t, root) // one CRITICAL finding
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "config.yaml"),
		[]byte("agents:\n  - greta\nfail_on: HIGH\n"), 0o644))
	cs := connectTest(t, root, fakeCompleter{})

	out := callOK[ReconcileResult](t, cs, ToolReconcile, map[string]any{})
	assert.False(t, out.Pass, "project-config fail_on must gate through MCP like the CLI")
	assert.Equal(t, "HIGH", out.FailOn)
	require.NotEmpty(t, out.Findings, "failing findings are returned inline")
}

func TestReconcileHandler_InvalidFailOn(t *testing.T) {
	root := t.TempDir()
	reviewFixture(t, root)
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolReconcile, map[string]any{"fail_on": "SEVERE"})
	assert.Contains(t, msg, "invalid severity")
}

// TestReconcileHandler_InProgressRejected verifies reconciling a review whose
// fan-out has not finished (summary.json absent) is rejected before any
// reconcile work, instead of silently emitting a verdict from a partial agent
// set or the misleading "no agent results found" error.
func TestReconcileHandler_InProgressRejected(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	id := inProgressFixture(t, root)
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolReconcile, map[string]any{})
	assert.Contains(t, msg, "still in_progress")
	assert.NoDirExists(t, filepath.Join(root, ".atcr", "reviews", id, "reconciled"),
		"an in-progress review must not get reconciled artifacts")
}

// TestReconcileHandler_PreCancelledContext verifies the handler passes its ctx
// through to RunReconcile instead of discarding it: a pre-cancelled context
// aborts the reconcile pipeline (risk profile: handlers honor ctx cancellation).
func TestReconcileHandler_PreCancelledContext(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	reviewFixture(t, root)
	e := &engine{root: root}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := e.handleReconcile(ctx, nil, ReconcileArgs{})
	require.ErrorIs(t, err, context.Canceled)
}

func TestReconcileHandler_NoResults(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	id := "2026-06-10_empty"
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr", "reviews", id, "sources"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte(id+"\n"), 0o644))
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolReconcile, map[string]any{})
	assert.Contains(t, msg, "no agent results found")
	assert.NoDirExists(t, filepath.Join(root, ".atcr", "reviews", id, "reconciled"),
		"fail-before-emit: an empty review must not get reconciled artifacts as a side effect")
}

// --- atcr_report ---------------------------------------------------------

func TestReportHandler_Formats(t *testing.T) {
	isolateUserConfig(t)
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
	isolateUserConfig(t)
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

// TestReportHandler_InProgressRejected verifies the missing-findings path
// distinguishes "fan-out still running" from "reconcile not run yet".
func TestReportHandler_InProgressRejected(t *testing.T) {
	root := t.TempDir()
	inProgressFixture(t, root)
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolReport, map[string]any{})
	assert.Contains(t, msg, "still in_progress")
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

// TestRangeHandler_BaseOnlyDefaultsHeadToHEAD verifies base-only mirrors the
// CLI contract: head defaults to HEAD (the natural CI-gate invocation).
func TestRangeHandler_BaseOnlyDefaultsHeadToHEAD(t *testing.T) {
	root, base, _ := gitRepo(t)
	cs := connectTest(t, root, fakeCompleter{})
	out := callOK[RangeResult](t, cs, ToolRange, map[string]any{"base": base})
	assert.Equal(t, 1, out.CommitCount)
	assert.Equal(t, 1, out.FileCount)
}

// TestRangeHandler_HeadWithoutBaseRejected verifies the pairing rule the CLI
// enforces in validateRangeFlags is also enforced at the MCP layer, in json
// field vocabulary.
func TestRangeHandler_HeadWithoutBaseRejected(t *testing.T) {
	root, _, head := gitRepo(t)
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolRange, map[string]any{"head": head})
	assert.Contains(t, msg, "head requires base")
	assert.NotContains(t, msg, "--head", "error must use json field names, not CLI flags")
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

// TestStatusHandler_Stale verifies the inferred `stale` state passes through
// atcr_status unchanged: StatusResult is a type alias of fanout.ReviewStatus, so
// the new enum value reaches MCP clients with no schema or shape change (Epic 1.5).
func TestStatusHandler_Stale(t *testing.T) {
	root := t.TempDir()
	id := "2026-06-10_dead"
	dir := filepath.Join(root, ".atcr", "reviews", id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"b","roster":["greta"],"started_at":"2020-01-01T00:00:00Z","timeout_secs":600,"partial":false}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte(id+"\n"), 0o644))
	cs := connectTest(t, root, fakeCompleter{})
	out := callOK[StatusResult](t, cs, ToolStatus, map[string]any{"id_or_path": id})
	assert.Equal(t, fanout.RunStale, out.Status)
}

func TestStatusHandler_NoReviews(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	msg := callErr(t, cs, ToolStatus, map[string]any{})
	assert.Contains(t, msg, "no reviews found")
}

// TestStatusHandler_CorruptLatestPointer verifies a corrupt/tampered
// .atcr/latest pointer is not misreported as "no reviews found": ReadLatest
// distinguishes a missing file from an invalid pointer, and resolveReviewDir
// must wrap that cause rather than discard it.
func TestStatusHandler_CorruptLatestPointer(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte("../escape\n"), 0o644))
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolStatus, map[string]any{})
	assert.Contains(t, msg, "invalid review id")
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

// TestLoadVerifyRegistry_CanonicalContainment checks that balanced traversal
// paths are rejected. "a/.." resolves to root and slips through the current
// string-matching check (Contains("a/..", "../") is false); canonical
// path containment must catch all three cases.
func TestLoadVerifyRegistry_CanonicalContainment(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	e := &engine{root: root}
	cases := []string{
		"a/..",          // resolves to root — slips through string-matching check
		"../x",          // resolves above root
		"sub/../../etc", // balanced traversal escaping root
	}
	for _, p := range cases {
		p := p
		t.Run(filepath.ToSlash(p), func(t *testing.T) {
			t.Parallel()
			_, err := e.loadVerifyRegistry(p)
			require.Error(t, err, "path %q must be rejected", p)
			assert.Contains(t, err.Error(), "invalid registryPath", "path %q must return invalid registryPath error", p)
		})
	}
}

// TestEngine_LoggerNilSafe verifies logger() returns a usable logger when
// engine.log is nil, matching the nil-guard buildServer provides at construction.
func TestEngine_LoggerNilSafe(t *testing.T) {
	e := &engine{}
	require.NotPanics(t, func() { e.logger().Info("nil-log probe") })
}

// TestReportHandler_PreCancelledContext verifies handleReport honors ctx
// cancellation up front: a pre-cancelled context aborts before any report work.
func TestReportHandler_PreCancelledContext(t *testing.T) {
	e := &engine{root: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := e.handleReport(ctx, nil, ReportArgs{})
	require.ErrorIs(t, err, context.Canceled)
}

// TestReportHandler_InvalidFormatInProcess verifies the handler's own enum check
// (defense in depth for in-process/programmatic callers that bypass the JSON
// Schema enum the transport enforces): an unknown format is a structured error.
func TestReportHandler_InvalidFormatInProcess(t *testing.T) {
	e := &engine{root: t.TempDir()}
	_, _, err := e.handleReport(context.Background(), nil, ReportArgs{Format: "xml"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid format")
}

// TestReportHandler_InvalidReviewID verifies a path-traversal id_or_path is
// rejected by resolveReviewDir before any report work (path-containment).
func TestReportHandler_InvalidReviewID(t *testing.T) {
	root := t.TempDir()
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolReport, map[string]any{"id_or_path": "../../etc/passwd"})
	assert.Contains(t, msg, "invalid review id")
}

// TestStatusHandler_NotFound verifies a well-formed review id that resolves to no
// on-disk review is the "review not found" error (the os.ErrNotExist branch),
// distinct from the corrupt-pointer and corrupt-manifest cases.
func TestStatusHandler_NotFound(t *testing.T) {
	root := t.TempDir()
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolStatus, map[string]any{"id_or_path": "2026-06-10_ghost"})
	assert.Contains(t, msg, "review not found")
}

// TestStatusHandler_PreCancelledContext verifies handleStatus honors ctx
// cancellation before resolving any review directory.
func TestStatusHandler_PreCancelledContext(t *testing.T) {
	e := &engine{root: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := e.handleStatus(ctx, nil, StatusArgs{IDOrPath: "2026-06-10_x"})
	require.ErrorIs(t, err, context.Canceled)
}

// TestRangeHandler_InvalidBaseRef verifies a resolution failure that is NOT
// "not a git repository" (here: an unresolvable base ref in a real repo) maps to
// the generic "failed to resolve range" client error via rangeError.
func TestRangeHandler_InvalidBaseRef(t *testing.T) {
	root, _, _ := gitRepo(t)
	cs := connectTest(t, root, fakeCompleter{})
	msg := callErr(t, cs, ToolRange, map[string]any{"base": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"})
	assert.Contains(t, msg, "failed to resolve range")
}

// TestReportHandler_SarifParity verifies the in-process handleReport renders SARIF
// identically to a direct report.Render(..., FormatSarif) — SARIF reaches MCP
// callers via the shared Render() path with no MCP-specific code (AC 01-04
// Scenario 3). Note: the report tool's transport-level format enum
// (reportInputSchema) is md|json|checklist, so an over-the-wire `sarif` request is
// rejected before the handler; this parity is exercised in-process, matching the
// AC's Scenario 3 which invokes handleReport directly.
func TestReportHandler_SarifParity(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	id := reviewFixture(t, root)
	cs := connectTest(t, root, fakeCompleter{})
	callOK[ReconcileResult](t, cs, ToolReconcile, map[string]any{})

	e := &engine{root: root}
	_, res, err := e.handleReport(context.Background(), nil, ReportArgs{IDOrPath: id, Format: report.FormatSarif})
	require.NoError(t, err)
	assert.Equal(t, report.FormatSarif, res.Format)
	assert.Contains(t, res.Content, `"version": "2.1.0"`)

	// Parity: same loader, same renderer the handler used — proves the handler
	// adds no divergence for sarif, exactly as it doesn't for json/checklist.
	dir, _, err := e.resolveReviewDir(id)
	require.NoError(t, err)
	findings, err := reconcile.ReadReconciledFindings(dir)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, report.Render(&buf, findings, report.FormatSarif))
	assert.Equal(t, buf.String(), res.Content)
}
