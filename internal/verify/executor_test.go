package verify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
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

func execConfig(minSev string) *registry.ExecutorConfig {
	return &registry.ExecutorConfig{
		Name: "opus", Provider: "p", Model: "m-exec", Persona: "fixer",
		Role: registry.RoleExecutor, MinSeverity: minSev,
	}
}

func execRegistry(minSev string) *registry.Registry {
	return &registry.Registry{
		Providers: map[string]registry.Provider{"p": {BaseURL: "http://x.invalid", APIKeyEnv: "K"}},
		Executor:  execConfig(minSev),
	}
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
}

func TestGenerateFixes_EmptyCompletionLeavesFix(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified, Fix: "orig"},
	}
	rec := &recordingExecutor{out: "   "}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, okDispatcher())
	assert.Equal(t, "orig", findings[0].Fix)
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
