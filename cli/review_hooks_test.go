package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// lockedReviewObserver records ReviewComplete calls under a lock, since the
// observer contract allows concurrent calls.
type lockedReviewObserver struct {
	mu  sync.Mutex
	got []ReviewReport
}

func (o *lockedReviewObserver) OnReviewComplete(_ context.Context, rep ReviewReport) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.got = append(o.got, rep)
}

func (o *lockedReviewObserver) reports() []ReviewReport {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]ReviewReport(nil), o.got...)
}

// execWithReviewHook runs the command tree with obs attached the way
// MainWithHooks attaches it, returning the exit code and combined output.
func execWithReviewHook(t *testing.T, obs ReviewCompleteObserver, args ...string) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(&buf)
	root.SetErr(&buf)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := root.ExecuteContext(withHooks(ctx, Hooks{ReviewComplete: obs}, &buf))
	out := buf.String()
	if err != nil {
		out += err.Error()
	}
	return exitCode(err), out
}

func TestReviewHook_FiresOnceOnSuccessfulReview(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	t.Setenv("GITHUB_REF", "")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")
	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")

	obs := &lockedReviewObserver{}
	code, out := execWithReviewHook(t, obs, "review", "--base", "HEAD^", "--pr", "42")
	require.Equal(t, 0, code, out)

	got := obs.reports()
	require.Len(t, got, 1, "one call per successful review")
	rep := got[0]
	require.Equal(t, base, rep.Base)
	require.Equal(t, head, rep.Head)
	require.Equal(t, 42, rep.PRNumber)
	require.False(t, rep.Partial)
	require.NotEmpty(t, rep.RunID)
	require.Equal(t, filepath.Base(rep.ReviewDir), rep.RunID)
	require.Equal(t, 1, rep.AgentsTotal)
	require.Equal(t, 1, rep.AgentsSucceeded)
	require.Equal(t, 0, rep.AgentsFailed)
	require.False(t, rep.Reconciled, "no in-process reconcile ran, so findings are not counted")
	require.Nil(t, rep.FindingsBySeverity)
}

// The hook fires after the ledgers: by the time the observer is called the
// audit record for the run is already on disk.
func TestReviewHook_FiresAfterLedgerWrites(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	auditPath := filepath.Join(".", ".atcr", "audit.log.jsonl")
	obs := &ledgerCheckingObserver{t: t, path: auditPath}
	code, out := execWithReviewHook(t, obs, "review", "--base", "HEAD^")
	require.Equal(t, 0, code, out)
	require.Equal(t, 1, obs.calls)
	require.True(t, obs.sawAudit, "the audit record must be written before the hook fires")
}

type ledgerCheckingObserver struct {
	t        *testing.T
	path     string
	calls    int
	sawAudit bool
}

func (o *ledgerCheckingObserver) OnReviewComplete(context.Context, ReviewReport) {
	o.calls++
	info, err := os.Stat(o.path)
	o.sawAudit = err == nil && info.Size() > 0
}

// With --fail-on the review reconciles in-process, so the report carries the
// reconciled counts. The CRITICAL finding trips the gate (exit 1), but the
// review itself completed and the hook has already fired.
func TestReviewHook_InlineReconcileFillsCounts(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	obs := &lockedReviewObserver{}
	code, out := execWithReviewHook(t, obs, "review", "--base", "HEAD^", "--fail-on", "critical")
	require.Equal(t, 1, code, "the CRITICAL finding fails the gate: %s", out)

	got := obs.reports()
	require.Len(t, got, 1)
	rep := got[0]
	require.True(t, rep.Reconciled)
	require.Equal(t, 1, rep.Findings)
	require.Equal(t, map[string]int{"CRITICAL": 1}, rep.FindingsBySeverity)
}

// One agent answers and one cannot be reached: the review is partial but
// successful, and the report says so.
func TestReviewHook_PartialReviewReportsPartial(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	reg := "providers:\n" +
		"  p:\n    api_key_env: " + testReviewKeyEnv + "\n    base_url: " + srv.URL + "\n" +
		"  dead:\n    api_key_env: " + testReviewKeyEnv + "\n    base_url: http://127.0.0.1:1/v1\n" +
		"agents:\n" +
		"  bruce:\n    provider: p\n    model: m-bruce\n    persona: bruce\n" +
		"  robin:\n    provider: dead\n    model: m-robin\n    persona: robin\n"
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(reg), 0o644))
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"), []byte("agents: [bruce, robin]\n"), 0o644))

	obs := &lockedReviewObserver{}
	code, out := execWithReviewHook(t, obs, "review", "--base", "HEAD^")
	require.Equal(t, 0, code, out)

	got := obs.reports()
	require.Len(t, got, 1)
	require.True(t, got[0].Partial)
	require.Equal(t, 2, got[0].AgentsTotal)
	require.Equal(t, 1, got[0].AgentsSucceeded)
	require.Equal(t, 1, got[0].AgentsFailed)
}

func TestReviewHook_NoCallWhenEveryAgentFails(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	writeReviewFixtureConfig(t) // bruce -> unreachable URL

	obs := &lockedReviewObserver{}
	code, _ := execWithReviewHook(t, obs, "review", "--base", "HEAD^")
	require.Equal(t, 1, code)
	require.Empty(t, obs.reports(), "a failed review must not fire the hook")
}

func TestReviewHook_FiresOnceOnResumeWithPendingAgent(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	t.Setenv("GITHUB_REF", "")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "robin")
	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})

	obs := &lockedReviewObserver{}
	code, out := execWithReviewHook(t, obs, "review", "--resume", "latest", "--base", "HEAD^", "--pr", "7")
	require.Equal(t, 0, code, out)

	got := obs.reports()
	require.Len(t, got, 1, "one call per successful resume")
	rep := got[0]
	require.Equal(t, "2026-06-18_demo", rep.RunID)
	require.Equal(t, base, rep.Base)
	require.Equal(t, head, rep.Head)
	require.Equal(t, 7, rep.PRNumber)
	require.False(t, rep.Partial)
	require.Equal(t, 2, rep.AgentsTotal, "the union of completed and resumed agents")
	require.True(t, rep.Reconciled, "a resume always reconciles")
	require.Equal(t, rep.Findings, sumCounts(rep.FindingsBySeverity))
}

// The AllComplete resume re-reconciles without new review work and writes no
// ledger, so it does not fire the hook either.
func TestReviewHook_NoCallOnAllCompleteResume(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")
	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^"))

	obs := &lockedReviewObserver{}
	code, out := execWithReviewHook(t, obs, "review", "--resume", "latest", "--base", "HEAD^")
	require.Equal(t, 0, code, out)
	require.Empty(t, obs.reports())
}

func TestReviewHook_NoCallFromReconcile(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")
	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^"))

	obs := &lockedReviewObserver{}
	code, out := execWithReviewHook(t, obs, "reconcile")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "reconciled", "the reconcile must actually have run")
	require.Empty(t, obs.reports(), "a standalone reconcile is not a review")
}

func sumCounts(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}
