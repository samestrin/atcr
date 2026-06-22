package verify

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingExecutor is a scripted executorCompleter that records every prompt and
// returns a fixed completion/error, so a test can assert call count and prompt
// contents without a provider.
type recordingExecutor struct {
	out     string
	err     error
	calls   int
	prompts []string
}

func (r *recordingExecutor) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	r.calls++
	r.prompts = append(r.prompts, inv.Prompt)
	return r.out, r.err
}

// testExecProvider is the provider name shared by execConfig and execRegistry.
// The executor's Provider field and the registry's Providers map key must match
// or validation rejects the registry, so both reference this single constant.
const testExecProvider = "p"

func execConfig(minSev string) *registry.ExecutorConfig {
	return &registry.ExecutorConfig{
		Name: "opus", Provider: testExecProvider, Model: "m-exec", Persona: "fixer",
		Role: registry.RoleExecutor, MinSeverity: minSev,
	}
}

func execRegistry(minSev string) *registry.Registry {
	return &registry.Registry{
		Providers: map[string]registry.Provider{testExecProvider: {BaseURL: "http://x.invalid", APIKeyEnv: "K"}},
		Executor:  execConfig(minSev),
	}
}

func TestReadFixSnippet_DoesNotShortCircuitWhitespacePath(t *testing.T) {
	// The empty/whitespace short-circuit in readFixSnippet is redundant: the
	// dispatcher's jail rejects empty paths, and any read error already maps to
	// "". Removing the trim lets the dispatcher decide, which is what the
	// downstream jail/tool handler does anyway.
	disp := okDispatcher()
	got := readFixSnippet(context.Background(), disp, "   ", 10)
	assert.Equal(t, "file contents", got, "whitespace-only path should still be dispatched")
	assert.Equal(t, 1, disp.count(), "trim short-circuit prevented the dispatcher call")
}

func TestGenerateFixes_PopulatesFixAndAttribution(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "sqli", Confidence: ConfidenceVerified,
			Evidence: "Found by bruce; confidence HIGH"},
	}
	rec := &recordingExecutor{out: "use a parameterized query"}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, okDispatcher())

	assert.Equal(t, 1, rec.calls)
	assert.Equal(t, "use a parameterized query", findings[0].Fix)
	assert.Contains(t, findings[0].Evidence, "fix by opus")
}

func TestGenerateFixes_SkipsBelowConfidence(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: reconcile.ConfMedium, Fix: "orig"},
	}
	rec := &recordingExecutor{out: "new fix"}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, okDispatcher())

	assert.Equal(t, 0, rec.calls, "MEDIUM confidence is below the HIGH floor")
	assert.Equal(t, "orig", findings[0].Fix)
}

func TestGenerateFixes_SkipsBelowSeverity(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "LOW", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified, Fix: "orig"},
	}
	rec := &recordingExecutor{out: "new fix"}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, okDispatcher())

	assert.Equal(t, 0, rec.calls, "LOW severity is below the MEDIUM fix floor")
	assert.Equal(t, "orig", findings[0].Fix)
}

func TestGenerateFixes_FailureIsolation(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified, Fix: "orig", Evidence: "ev"},
	}
	rec := &recordingExecutor{err: errors.New("provider boom")}
	require.NotPanics(t, func() {
		generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, okDispatcher())
	})
	assert.Equal(t, 1, rec.calls)
	assert.Equal(t, "orig", findings[0].Fix, "failed fix leaves the reviewer fix untouched")
	assert.Equal(t, "ev", findings[0].Evidence, "no attribution on failure")
	assert.Contains(t, findings[0].FixWarning, "fix generation failed", "failure is recorded on the finding for downstream consumers")
}

func TestGenerateFixes_EmptyCompletionLeavesFix(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified, Fix: "orig"},
	}
	rec := &recordingExecutor{out: "   "}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, okDispatcher())
	assert.Equal(t, "orig", findings[0].Fix)
	assert.Contains(t, findings[0].FixWarning, "empty completion")
}

func TestGenerateFixes_Idempotent(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified,
			Fix: "already fixed", Evidence: "Found by bruce; fix by opus"},
	}
	rec := &recordingExecutor{out: "new fix"}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, okDispatcher())
	assert.Equal(t, 0, rec.calls, "an already-attributed finding is not re-generated")
	assert.Equal(t, "already fixed", findings[0].Fix)
}

func TestGenerateFixes_AttributionGuardIsNameSpecific(t *testing.T) {
	// Evidence mentions "fix by " but not this executor (opus); generation must run.
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified,
			Evidence: "reviewer suggested a fix by hand"},
	}
	rec := &recordingExecutor{out: "real fix"}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, okDispatcher())
	assert.Equal(t, 1, rec.calls)
	assert.Equal(t, "real fix", findings[0].Fix)
	assert.Contains(t, findings[0].Evidence, "fix by opus")
}

func TestGenerateFixes_SnippetEmbeddedInPrompt(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 42, Problem: "p", Confidence: ConfidenceVerified},
	}
	// okDispatcher returns Content "file contents" for read_file.
	rec := &recordingExecutor{out: "fix"}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, okDispatcher())
	require.Len(t, rec.prompts, 1)
	assert.Contains(t, rec.prompts[0], "file contents", "snippet read from the snapshot is embedded in the prompt")
}

// blockingExecutor records the peak number of Complete calls in flight at once.
// Every call announces its arrival on `arrived`, then parks on `release` so all
// concurrent calls pile up before any return — letting a test observe true peak
// concurrency rather than racing on timing.
type blockingExecutor struct {
	mu       sync.Mutex
	inFlight int
	peak     int
	arrived  chan struct{}
	release  chan struct{}
}

func (b *blockingExecutor) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	b.mu.Lock()
	b.inFlight++
	if b.inFlight > b.peak {
		b.peak = b.inFlight
	}
	b.mu.Unlock()
	b.arrived <- struct{}{}
	<-b.release
	b.mu.Lock()
	b.inFlight--
	b.mu.Unlock()
	return "fix", nil
}

// TestGenerateFixes_RunsConcurrently proves fix generation runs as a bounded
// worker pool, not serially: with N eligible findings and max_parallel=N, all N
// executor calls must be in flight at once. Serial generation parks the first
// call on `release` forever, so the N-th arrival never comes and the test times
// out — the RED state for this item.
func TestGenerateFixes_RunsConcurrently(t *testing.T) {
	const n = 4
	findings := make([]reconcile.JSONFinding, n)
	for i := range findings {
		findings[i] = reconcile.JSONFinding{
			Severity: "HIGH", File: "a.go", Line: i + 1, Problem: "p", Confidence: ConfidenceVerified,
		}
	}
	be := &blockingExecutor{arrived: make(chan struct{}, n), release: make(chan struct{})}
	reg := execRegistry("MEDIUM")
	reg.Verify.MaxParallel = n

	done := make(chan struct{})
	go func() {
		generateFixes(context.Background(), findings, execConfig("MEDIUM"), reg, be, okDispatcher())
		close(done)
	}()

	for i := 0; i < n; i++ {
		select {
		case <-be.arrived:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d/%d fix calls ran concurrently — generateFixes is serial", i, n)
		}
	}
	close(be.release)
	<-done

	be.mu.Lock()
	peak := be.peak
	be.mu.Unlock()
	assert.Equal(t, n, peak, "all %d fixes should run concurrently under the worker pool", n)
	for i := range findings {
		assert.Equal(t, "fix", findings[i].Fix)
	}
}

// TestGenerateFixes_BoundedByMaxParallel proves the worker pool honors its cap:
// with more eligible findings than max_parallel, peak concurrency never exceeds
// the cap (so the pool bounds executor round-trips rather than launching all at
// once).
func TestGenerateFixes_BoundedByMaxParallel(t *testing.T) {
	const n, cap = 6, 2
	findings := make([]reconcile.JSONFinding, n)
	for i := range findings {
		findings[i] = reconcile.JSONFinding{
			Severity: "HIGH", File: "a.go", Line: i + 1, Problem: "p", Confidence: ConfidenceVerified,
		}
	}
	// Release each call shortly after it arrives, so the pool keeps cycling
	// workers while peak in-flight stays capped.
	be := &blockingExecutor{arrived: make(chan struct{}, n), release: make(chan struct{})}
	reg := execRegistry("MEDIUM")
	reg.Verify.MaxParallel = cap

	done := make(chan struct{})
	go func() {
		generateFixes(context.Background(), findings, execConfig("MEDIUM"), reg, be, okDispatcher())
		close(done)
	}()

	// Drain arrivals and let calls return one at a time; the pool must never
	// have more than `cap` calls in flight.
	go func() {
		for i := 0; i < n; i++ {
			<-be.arrived
			be.release <- struct{}{}
		}
	}()
	<-done

	be.mu.Lock()
	peak := be.peak
	be.mu.Unlock()
	assert.LessOrEqual(t, peak, cap, "peak in-flight must not exceed max_parallel")
	assert.Greater(t, peak, 0, "fixes should have been generated")
}

// TestReadFixSnippet_LogsWhenDispatcherFails proves a swallowed dispatcher error
// is recorded (mirroring the skeptic-failure logging discipline) instead of
// silently degrading the fix to finding-text-only with no trace.
func TestReadFixSnippet_LogsWhenDispatcherFails(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := log.NewContext(context.Background(), logger)
	disp := &fakeDispatcher{err: errors.New("read boom")}
	got := readFixSnippet(ctx, disp, "a.go", 10)
	assert.Equal(t, "", got, "a dispatcher error still yields an empty snippet")
	assert.Contains(t, buf.String(), "fix_snippet_unavailable", "the swallowed dispatcher error must be logged")
}

// TestGenerateFixes_ClearsStaleFixWarningOnSuccess proves a successful fix clears
// any FixWarning left by a prior failed/empty run, so a finding never carries both
// a valid Fix and a stale warning claiming the fix is absent.
func TestGenerateFixes_ClearsStaleFixWarningOnSuccess(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified,
			FixWarning: "fix generation failed: provider boom"},
	}
	rec := &recordingExecutor{out: "use a parameterized query"}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, okDispatcher())
	assert.Equal(t, "use a parameterized query", findings[0].Fix)
	assert.Equal(t, "", findings[0].FixWarning, "a successful re-run must clear the stale fix warning")
}

// TestGenerateFixes_StopsOnCanceledContext proves the dispatch loop bails as soon
// as the context is canceled instead of grinding through every remaining finding.
// With a pre-canceled context no executor round-trip should be entered at all.
func TestGenerateFixes_StopsOnCanceledContext(t *testing.T) {
	findings := make([]reconcile.JSONFinding, 4)
	for i := range findings {
		findings[i] = reconcile.JSONFinding{
			Severity: "HIGH", File: "a.go", Line: i + 1, Problem: "p", Confidence: ConfidenceVerified,
		}
	}
	rec := &recordingExecutor{out: "fix"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before generation starts
	generateFixes(ctx, findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, okDispatcher())
	assert.Equal(t, 0, rec.calls, "a canceled context must stop fix generation before any executor call")
}

// swapExecutorClient overrides the package executor-client seam and returns a
// restore func, so an integration test can inject a scripted completer.
func swapExecutorClient(fn func() executorCompleter) func() {
	prev := newExecutorClient
	newExecutorClient = fn
	return func() { newExecutorClient = prev }
}

func TestGenerateFixes_NilExecutorOrCompleterNoop(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified, Fix: "orig"},
	}
	require.NotPanics(t, func() {
		generateFixes(context.Background(), findings, nil, execRegistry("MEDIUM"), &recordingExecutor{}, okDispatcher())
		generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), nil, okDispatcher())
	})
	assert.Equal(t, "orig", findings[0].Fix)
}

// Integration: an executor configured on the registry generates a fix during
// runVerify and the fix lands in findings.json with executor attribution.
func TestRunVerify_ExecutorGeneratesFixIntoArtifact(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "sqli", Confidence: "HIGH", Reviewers: []string{"rev"},
			Evidence: "Found by rev; confidence HIGH"},
	})
	reg := execRegistry("MEDIUM") // no skeptic agents → finding stays HIGH (no eligible skeptic)

	rec := &recordingExecutor{out: "use a parameterized query: db.Query(q, id)"}
	restore := swapExecutorClient(func() executorCompleter { return rec })
	defer restore()

	_, err := runVerify(context.Background(), dir, reg, Options{}, scriptedHarness(`{}`))
	require.NoError(t, err)

	got := readFindings(t, dir)
	assert.Equal(t, "use a parameterized query: db.Query(q, id)", got[0].Fix)
	assert.Contains(t, got[0].Evidence, "fix by opus")
	assert.GreaterOrEqual(t, rec.calls, 1)
}

// TestRunVerify_ExecutorOutputSchema locks the resolved output-format contract
// (Epic 7.0): the generated fix populates the existing `fix` key (column 4) and the
// executor attribution rides in the existing `evidence` key (column 7) — NO new
// per-finding executor column is added, so the 9-column schema is preserved.
func TestRunVerify_ExecutorOutputSchema(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "sqli", Confidence: "HIGH", Reviewers: []string{"rev"},
			Evidence: "Found by rev; confidence HIGH"},
	})
	rec := &recordingExecutor{out: "parameterize the query"}
	restore := swapExecutorClient(func() executorCompleter { return rec })
	defer restore()

	_, err := runVerify(context.Background(), dir, execRegistry("MEDIUM"), Options{}, scriptedHarness(`{}`))
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, reconciledSubdir, reconcile.FindingsJSON))
	require.NoError(t, err)
	s := string(raw)
	assert.Contains(t, s, `"fix": "parameterize the query"`)
	assert.Contains(t, s, "fix by opus")
	assert.NotContains(t, s, `"executor"`, "executor attribution rides in evidence, not a new column/key")
}

// Integration: with no executor configured, runVerify behaves exactly as before —
// the fix column is left as the reviewer supplied it.
func TestRunVerify_NoExecutorLeavesFixUnchanged(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: "HIGH", Reviewers: []string{"rev"}, Fix: "reviewer fix"},
	})
	_, err := runVerify(context.Background(), dir, skepticRegistry(), Options{}, scriptedHarness(`{"verdict":"confirmed"}`))
	require.NoError(t, err)
	got := readFindings(t, dir)
	assert.Equal(t, "reviewer fix", got[0].Fix)
	assert.NotContains(t, got[0].Evidence, "fix by")
	assert.FileExists(t, filepath.Join(dir, reconciledSubdir, "verification.json"))
}
