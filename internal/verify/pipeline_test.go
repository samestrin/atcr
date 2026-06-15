package verify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initGitRepo creates a one-commit git repo in a temp dir and returns its path,
// so buildDispatcher's snapshot path can be exercised against a real SHA.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644))
	run("add", "a.go")
	run("commit", "-q", "-m", "init")
	return dir
}

// gitHeadSHA returns the full HEAD commit SHA of repo.
func gitHeadSHA(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git rev-parse: %s", out)
	return strings.TrimSpace(string(out))
}

// pipelineReview writes a completed, reconciled review dir (reconciled/findings.json,
// reconciled/summary.json, manifest.json) under a temp dir and returns its path.
func pipelineReview(t *testing.T, findings []reconcile.JSONFinding) string {
	t.Helper()
	dir := t.TempDir()
	recon := filepath.Join(dir, reconciledSubdir)
	require.NoError(t, os.MkdirAll(recon, 0o755))
	data, err := json.MarshalIndent(findings, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(recon, reconcile.FindingsJSON), append(data, '\n'), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(recon, reconcile.SummaryJSON), []byte(`{"total_findings":0}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestFile),
		[]byte(`{"base":"a","head":"deadbeef","roster":["rev"],"partial":false,"stages":["review"]}`), 0o644))
	return dir
}

// skepticRegistry builds an in-memory registry with one reviewer (model m-rev)
// and one skeptic (model m-skep, function-calling capable), so a finding credited
// to "rev" has an eligible different-model skeptic.
func skepticRegistry() *registry.Registry {
	return &registry.Registry{
		Providers: map[string]registry.Provider{"p": {BaseURL: "http://x.invalid", APIKeyEnv: "K"}},
		Agents: map[string]registry.AgentConfig{
			"rev":  {Provider: "p", Model: "m-rev", Role: registry.RoleReviewer},
			"skep": {Provider: "p", Model: "m-skep", Role: registry.RoleSkeptic, SupportsFC: true},
		},
	}
}

// scriptedHarness returns a harnessFunc serving a scripted completer + fake
// dispatcher, so the pipeline runs end to end without a provider or git snapshot.
func scriptedHarness(content string) harnessFunc {
	return func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		return finalChat(content), okDispatcher(), nil, nil
	}
}

// alwaysChat answers every Chat/Complete turn with the same content, so a single
// shared completer serves every skeptic in a multi-vote run identically.
type alwaysChat struct{ content string }

func (a alwaysChat) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return a.content, nil
}

func (a alwaysChat) Chat(_ context.Context, _ llmclient.Invocation, _ []llmclient.Message, _ []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	c := a.content
	return &llmclient.ChatResponse{Message: llmclient.Message{Role: "assistant", Content: &c}, FinishReason: "stop"}, nil
}

// byModelChat answers each skeptic according to its Invocation.Model, so a
// multi-skeptic run can script a different verdict per skeptic (e.g. 2-confirm/
// 1-refute) — unlike alwaysChat, which answers every skeptic identically.
type byModelChat struct{ byModel map[string]string }

func (b byModelChat) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	return b.byModel[inv.Model], nil
}

func (b byModelChat) Chat(_ context.Context, inv llmclient.Invocation, _ []llmclient.Message, _ []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	c := b.byModel[inv.Model]
	return &llmclient.ChatResponse{Message: llmclient.Message{Role: "assistant", Content: &c}, FinishReason: "stop"}, nil
}

func readFindings(t *testing.T, dir string) []reconcile.JSONFinding {
	t.Helper()
	f, err := reconcile.ReadReconciledFindings(dir)
	require.NoError(t, err)
	return f
}

// TestRunVerify_ConfirmedPromotesToVerified: a skeptic that confirms a finding
// promotes it to VERIFIED and records a confirmed verdict (AC 03-01 / 04-01).
func TestRunVerify_ConfirmedPromotesToVerified(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	res, err := runVerify(context.Background(), dir, skepticRegistry(), Options{},
		scriptedHarness(`{"verdict":"confirmed","reasoning":"checked the line"}`))
	require.NoError(t, err)
	assert.Equal(t, 1, res.FindingsProcessed)
	assert.Equal(t, VerdictCounts{Confirmed: 1}, res.VerdictCounts)

	got := readFindings(t, dir)
	require.NotNil(t, got[0].Verification)
	assert.Equal(t, "confirmed", got[0].Verification.Verdict)
	assert.Equal(t, ConfidenceVerified, got[0].Confidence)
	assert.FileExists(t, filepath.Join(dir, reconciledSubdir, "verification.json"))
}

// TestRunVerify_RefutedDemotesToLow: a skeptic that refutes a finding demotes it
// to LOW but retains it (AC 03-01).
func TestRunVerify_RefutedDemotesToLow(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "HIGH", Reviewers: []string{"rev"}},
	})
	res, err := runVerify(context.Background(), dir, skepticRegistry(), Options{},
		scriptedHarness(`{"verdict":"refuted","reasoning":"unreachable"}`))
	require.NoError(t, err)
	assert.Equal(t, VerdictCounts{Refuted: 1}, res.VerdictCounts)
	got := readFindings(t, dir)
	assert.Equal(t, "refuted", got[0].Verification.Verdict)
	assert.Equal(t, reconcile.ConfLow, got[0].Confidence)
}

// TestRunVerify_BelowFloorSkipped: a finding below the min-severity floor keeps
// its v1 confidence and never reaches a skeptic (cost control, AC 02-07).
func TestRunVerify_BelowFloorSkipped(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "LOW", File: "a.go", Line: 1, Problem: "nit", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	res, err := runVerify(context.Background(), dir, skepticRegistry(), Options{MinSeverity: "MEDIUM"},
		scriptedHarness(`{"verdict":"confirmed"}`))
	require.NoError(t, err)
	assert.Equal(t, 0, res.FindingsProcessed)
	assert.Nil(t, readFindings(t, dir)[0].Verification)
}

// TestRunVerify_NoEligibleSkeptic: a finding whose only skeptic shares the
// reviewer's model is unverifiable with no_eligible_skeptic, no harness built.
func TestRunVerify_NoEligibleSkeptic(t *testing.T) {
	reg := skepticRegistry()
	reg.Agents["skep"] = registry.AgentConfig{Provider: "p", Model: "m-rev", Role: registry.RoleSkeptic, SupportsFC: true} // same model as rev
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	res, err := runVerify(context.Background(), dir, reg, Options{}, func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		t.Fatal("harness must not be built when no skeptic is eligible")
		return nil, nil, nil, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 0, res.FindingsProcessed, "no eligible skeptic: finding must not count as processed")
	assert.Equal(t, VerdictCounts{Unverifiable: 1}, res.VerdictCounts)
	got := readFindings(t, dir)
	assert.Equal(t, "unverifiable", got[0].Verification.Verdict)
	assert.Equal(t, "no_eligible_skeptic", got[0].Verification.Notes)
}

// TestRunVerify_ToolHarnessUnavailable: when the harness cannot be built, an
// eligible finding degrades to unverifiable rather than failing the run.
func TestRunVerify_ToolHarnessUnavailable(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	res, err := runVerify(context.Background(), dir, skepticRegistry(), Options{}, func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		return nil, nil, nil, errors.New("snapshot failed")
	})
	require.NoError(t, err)
	assert.Equal(t, 0, res.FindingsProcessed, "harness unavailable: no finding was sent through a live skeptic")
	assert.Equal(t, VerdictCounts{Unverifiable: 1}, res.VerdictCounts)
	assert.Equal(t, "tool_harness_unavailable", readFindings(t, dir)[0].Verification.Notes)
}

func TestRunVerify_ToolHarnessUnavailable_RedactsDetail(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w

	_, err = runVerify(context.Background(), dir, skepticRegistry(), Options{}, func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		return nil, nil, nil, errors.New("snapshot failed: /secret/repo/path")
	})
	require.NoError(t, w.Close())
	os.Stderr = oldStderr
	require.NoError(t, err)

	var buf strings.Builder
	_, _ = io.Copy(&buf, r)
	output := buf.String()
	assert.Contains(t, output, "tool harness unavailable")
	assert.NotContains(t, output, "/secret/repo/path")
}

// TestRunVerify_SkipsAlreadyVerified: without Fresh, a finding that already
// carries a verdict is skipped; with Fresh it is re-verified (AC 04-05).
func TestRunVerify_SkipsAlreadyVerified(t *testing.T) {
	mk := func() string {
		return pipelineReview(t, []reconcile.JSONFinding{{
			Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "VERIFIED",
			Reviewers: []string{"rev"}, Verification: &reconcile.Verification{Verdict: "confirmed", Skeptic: "prior"},
		}})
	}

	// Without Fresh: skipped, prior verdict preserved, harness never built.
	dir := mk()
	res, err := runVerify(context.Background(), dir, skepticRegistry(), Options{}, func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		t.Fatal("already-verified finding must not invoke the harness")
		return nil, nil, nil, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 0, res.FindingsProcessed)
	assert.Equal(t, "confirmed", readFindings(t, dir)[0].Verification.Verdict)

	// With Fresh: re-verified (here refuted), prior verdict overwritten.
	dir = mk()
	res, err = runVerify(context.Background(), dir, skepticRegistry(), Options{Fresh: true},
		scriptedHarness(`{"verdict":"refuted","reasoning":"stale"}`))
	require.NoError(t, err)
	assert.Equal(t, 1, res.FindingsProcessed)
	assert.Equal(t, "refuted", readFindings(t, dir)[0].Verification.Verdict)
}

// TestRunVerify_ThoroughUsesThreeSkeptics: --thorough raises the vote count, so a
// majority of confirms yields confirmed across three skeptics.
func TestRunVerify_ThoroughUsesThreeSkeptics(t *testing.T) {
	reg := skepticRegistry()
	// Three eligible skeptics, all different models from the reviewer.
	reg.Agents["s2"] = registry.AgentConfig{Provider: "p", Model: "m-s2", Role: registry.RoleSkeptic, SupportsFC: true}
	reg.Agents["s3"] = registry.AgentConfig{Provider: "p", Model: "m-s3", Role: registry.RoleSkeptic, SupportsFC: true}
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	harness := func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		return alwaysChat{content: `{"verdict":"confirmed","reasoning":"ok"}`}, okDispatcher(), nil, nil
	}
	res, err := runVerify(context.Background(), dir, reg, Options{Thorough: true}, harness)
	require.NoError(t, err)
	assert.Equal(t, VerdictCounts{Confirmed: 1}, res.VerdictCounts)
}

// TestRunVerify_ThoroughMultiSkepticRecordsAllModels: in a --thorough run with
// multiple eligible skeptics, the verification.json model field must name every
// participating skeptic's model, not just the lead (skeptics[0]) model.
func TestRunVerify_ThoroughMultiSkepticRecordsAllModels(t *testing.T) {
	reg := skepticRegistry()
	// skep=m-skep is already in skepticRegistry; add s2=m-s2. Alphabetically
	// SelectEligibleSkeptics returns ["s2","skep"], so skeptics[0].Config.Model
	// is "m-s2". The test asserts "m-skep" also appears — which fails today.
	reg.Agents["s2"] = registry.AgentConfig{Provider: "p", Model: "m-s2", Role: registry.RoleSkeptic, SupportsFC: true}
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	harness := func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		return alwaysChat{content: `{"verdict":"confirmed","reasoning":"ok"}`}, okDispatcher(), nil, nil
	}
	_, err := runVerify(context.Background(), dir, reg, Options{Thorough: true}, harness)
	require.NoError(t, err)

	data, readErr := os.ReadFile(filepath.Join(dir, reconciledSubdir, "verification.json"))
	require.NoError(t, readErr)
	var vf VerificationFile
	require.NoError(t, json.Unmarshal(data, &vf))
	require.Len(t, vf.Findings, 1)
	assert.Contains(t, vf.Findings[0].Model, "m-s2", "lead skeptic model must be recorded")
	assert.Contains(t, vf.Findings[0].Model, "m-skep", "all participating skeptic models must be recorded")
}

// TestVerifyFinding_EarlyReturnsRecordEmptyModel locks AC3: neither early-return
// path reaches a skeptic execution, so VerificationResult.Model is "" — a model is
// attributed only to what actually ran (epic 3.1 clarification). Candidate skeptic
// models present on the tool_harness_unavailable path are deliberately NOT recorded.
func TestVerifyFinding_EarlyReturnsRecordEmptyModel(t *testing.T) {
	t.Parallel()
	f := reconcile.JSONFinding{File: "a.go", Line: 1, Problem: "boom"}

	// no_eligible_skeptic: no skeptics at all.
	_, vr := verifyFinding(context.Background(), f, nil, finalChat("{}"), okDispatcher())
	assert.Equal(t, "unverifiable", vr.Verdict)
	assert.Equal(t, "no_eligible_skeptic", vr.Reasoning)
	assert.Empty(t, vr.Model, "no skeptic ran → no model attributed")

	// tool_harness_unavailable: skeptics eligible but the dispatcher never built.
	_, vr = verifyFinding(context.Background(), f, []Skeptic{testSkeptic()}, finalChat("{}"), nil)
	assert.Equal(t, "unverifiable", vr.Verdict)
	assert.Equal(t, "tool_harness_unavailable", vr.Reasoning)
	assert.Empty(t, vr.Model, "skeptics selected but none executed → no model attributed")
}

// TestRunVerify_WinningModelAttribution_TwoConfirmOneRefute locks AC2: in a
// 3-skeptic run where two confirm and one refutes, VerificationResult.Model must
// name only the two winning (confirming) skeptics' models — never the losing
// (refuting) skeptic's model, and never default to skeptics[0].
func TestRunVerify_WinningModelAttribution_TwoConfirmOneRefute(t *testing.T) {
	reg := skepticRegistry() // skep=m-skep
	reg.Agents["s2"] = registry.AgentConfig{Provider: "p", Model: "m-s2", Role: registry.RoleSkeptic, SupportsFC: true}
	reg.Agents["s3"] = registry.AgentConfig{Provider: "p", Model: "m-s3", Role: registry.RoleSkeptic, SupportsFC: true}
	// Alphabetical selection: s2(m-s2), s3(m-s3), skep(m-skep). s3 refutes; the
	// other two confirm → winner=confirmed, winners={m-s2, m-skep}.
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	harness := func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		return byModelChat{byModel: map[string]string{
			"m-s2":   `{"verdict":"confirmed","reasoning":"yes"}`,
			"m-skep": `{"verdict":"confirmed","reasoning":"yes"}`,
			"m-s3":   `{"verdict":"refuted","reasoning":"no"}`,
		}}, okDispatcher(), nil, nil
	}
	_, err := runVerify(context.Background(), dir, reg, Options{Thorough: true}, harness)
	require.NoError(t, err)

	data, rerr := os.ReadFile(filepath.Join(dir, reconciledSubdir, "verification.json"))
	require.NoError(t, rerr)
	var vf VerificationFile
	require.NoError(t, json.Unmarshal(data, &vf))
	require.Len(t, vf.Findings, 1)
	assert.Equal(t, "confirmed", vf.Findings[0].Verdict)
	assert.Contains(t, vf.Findings[0].Model, "m-s2", "winning skeptic model must be recorded")
	assert.Contains(t, vf.Findings[0].Model, "m-skep", "winning skeptic model must be recorded")
	assert.NotContains(t, vf.Findings[0].Model, "m-s3", "losing (refuting) skeptic model must NOT be attributed")
}

// TestRunVerify_ThreeWayTieRecordsAllParticipantModels locks the tie branch of
// winningAttribution: when three skeptics split 1 confirmed / 1 refuted / 1
// unverifiable, the aggregate is a tie → unverifiable, and Model must name every
// participant — not just the lone unverifiable voter (independent-review MED).
func TestRunVerify_ThreeWayTieRecordsAllParticipantModels(t *testing.T) {
	reg := skepticRegistry() // skep=m-skep
	reg.Agents["s2"] = registry.AgentConfig{Provider: "p", Model: "m-s2", Role: registry.RoleSkeptic, SupportsFC: true}
	reg.Agents["s3"] = registry.AgentConfig{Provider: "p", Model: "m-s3", Role: registry.RoleSkeptic, SupportsFC: true}
	// Selection s2(m-s2), s3(m-s3), skep(m-skep): confirmed / refuted / unverifiable.
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	harness := func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		return byModelChat{byModel: map[string]string{
			"m-s2":   `{"verdict":"confirmed","reasoning":"yes"}`,
			"m-s3":   `{"verdict":"refuted","reasoning":"no"}`,
			"m-skep": `{"verdict":"unverifiable","reasoning":"dunno"}`,
		}}, okDispatcher(), nil, nil
	}
	_, err := runVerify(context.Background(), dir, reg, Options{Thorough: true}, harness)
	require.NoError(t, err)

	data, rerr := os.ReadFile(filepath.Join(dir, reconciledSubdir, "verification.json"))
	require.NoError(t, rerr)
	var vf VerificationFile
	require.NoError(t, json.Unmarshal(data, &vf))
	require.Len(t, vf.Findings, 1)
	assert.Equal(t, "unverifiable", vf.Findings[0].Verdict, "a 1/1/1 split is a tie → unverifiable")
	for _, m := range []string{"m-s2", "m-s3", "m-skep"} {
		assert.Contains(t, vf.Findings[0].Model, m, "tie must record every participant's model")
	}
}

// TestRunVerify_SkipDropsMismatchedPriorMetadata locks the carry-forward guard: a
// prior verification.json whose verdict no longer matches the current findings.json
// block must NOT lend its Model/DurationMs/TrippedBudgets to the now-different
// outcome (independent-review LOW).
func TestRunVerify_SkipDropsMismatchedPriorMetadata(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "VERIFIED",
		Reviewers: []string{"rev"}, Verification: &reconcile.Verification{Verdict: "confirmed", Skeptic: "s1"},
	}})
	// Prior verdict is "refuted" — a stale/hand-edited mismatch against the current
	// "confirmed" block; its rich metadata must be dropped, not carried.
	require.NoError(t, WriteVerification(dir, []VerificationResult{{
		File: "a.go", Line: 1, Problem: "boom", Verdict: "refuted", Skeptic: "s1",
		Model: "m-stale", DurationMs: 9999, TrippedBudgets: []string{"timeout_secs"},
	}}))

	_, err := runVerify(context.Background(), dir, skepticRegistry(), Options{}, func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		t.Fatal("already-verified finding must not invoke the harness")
		return nil, nil, nil, nil
	})
	require.NoError(t, err)

	data, rerr := os.ReadFile(filepath.Join(dir, reconciledSubdir, "verification.json"))
	require.NoError(t, rerr)
	var vf VerificationFile
	require.NoError(t, json.Unmarshal(data, &vf))
	require.Len(t, vf.Findings, 1)
	assert.Equal(t, "confirmed", vf.Findings[0].Verdict)
	assert.Empty(t, vf.Findings[0].Model, "mismatched prior verdict must not carry its model")
	assert.Zero(t, vf.Findings[0].DurationMs, "mismatched prior verdict must not carry its duration")
	assert.Empty(t, vf.Findings[0].TrippedBudgets, "mismatched prior verdict must not carry its tripped budgets")
}

// TestRunVerify_SkipPreservesPriorMetadata locks AC4: a finding skipped on a
// re-run (already-verified, no --fresh) must carry Model/DurationMs/TrippedBudgets
// forward from the prior on-disk verification.json rather than re-synthesizing a
// lossy compact record from the findings.json block (which lacks those fields).
func TestRunVerify_SkipPreservesPriorMetadata(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "VERIFIED",
		Reviewers: []string{"rev"}, Verification: &reconcile.Verification{Verdict: "confirmed", Skeptic: "s1"},
	}})
	// Seed a prior verification.json carrying rich metadata for that finding.
	require.NoError(t, WriteVerification(dir, []VerificationResult{{
		File: "a.go", Line: 1, Problem: "boom", Verdict: "confirmed", Skeptic: "s1",
		Model: "m-prior", Reasoning: "checked the line", DurationMs: 4242, TrippedBudgets: []string{"max_turns"},
	}}))

	// Without --fresh the finding is skipped: the harness must never build, and the
	// rewritten verification.json must retain the prior metadata.
	res, err := runVerify(context.Background(), dir, skepticRegistry(), Options{}, func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		t.Fatal("already-verified finding must not invoke the harness")
		return nil, nil, nil, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 0, res.FindingsProcessed)

	data, rerr := os.ReadFile(filepath.Join(dir, reconciledSubdir, "verification.json"))
	require.NoError(t, rerr)
	var vf VerificationFile
	require.NoError(t, json.Unmarshal(data, &vf))
	require.Len(t, vf.Findings, 1)
	assert.Equal(t, "confirmed", vf.Findings[0].Verdict)
	assert.Equal(t, "m-prior", vf.Findings[0].Model, "skip path must carry prior model")
	assert.Equal(t, 4242, vf.Findings[0].DurationMs, "skip path must carry prior duration")
	assert.Equal(t, []string{"max_turns"}, vf.Findings[0].TrippedBudgets, "skip path must carry prior tripped budgets")
}

// TestRunVerify_MissingReconciledFindings: a review with no findings.json returns
// ErrNoReconciledFindings (the caller renders the reconcile-first guidance).
func TestRunVerify_MissingReconciledFindings(t *testing.T) {
	dir := t.TempDir()
	_, err := runVerify(context.Background(), dir, skepticRegistry(), Options{}, scriptedHarness(`{}`))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoReconciledFindings))
}

// TestVerify_BuildsProductionHarness: the exported Verify wires the production
// harness; with a finding whose snapshot head is bogus, the harness fails to
// build and the finding degrades to unverifiable — exercising the public entry.
func TestVerify_BuildsProductionHarness(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	res, err := Verify(context.Background(), t.TempDir(), dir, skepticRegistry(), Options{})
	require.NoError(t, err)
	// "deadbeef" is not a resolvable commit in the empty repoRoot, so the snapshot
	// fails and the finding degrades to unverifiable rather than erroring.
	assert.Equal(t, VerdictCounts{Unverifiable: 1}, res.VerdictCounts)
}

// TestVerify_ProductionHarnessSuccess exercises the exported Verify with a real
// git snapshot so the production harness builds a dispatcher successfully; the
// skeptic call then fails against the unreachable provider and the finding
// degrades to unverifiable — covering Verify's harness-success closure.
func TestVerify_ProductionHarnessSuccess(t *testing.T) {
	repo := initGitRepo(t)
	sha := gitHeadSHA(t, repo)
	rev := filepath.Join(repo, "review")
	recon := filepath.Join(rev, reconciledSubdir)
	require.NoError(t, os.MkdirAll(recon, 0o755))
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	}
	data, err := json.MarshalIndent(findings, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(recon, reconcile.FindingsJSON), append(data, '\n'), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(recon, reconcile.SummaryJSON), []byte(`{"total_findings":1}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(rev, manifestFile),
		[]byte(`{"base":"a","head":"`+sha+`","stages":["review"]}`), 0o644))

	res, err := Verify(context.Background(), repo, rev, skepticRegistry(), Options{})
	require.NoError(t, err)
	assert.Equal(t, VerdictCounts{Unverifiable: 1}, res.VerdictCounts)
}

// TestReadManifestHead covers the manifest head reader's success and error paths.
func TestReadManifestHead(t *testing.T) {
	dir := pipelineReview(t, nil)
	head, err := readManifestHead(dir)
	require.NoError(t, err)
	assert.Equal(t, "deadbeef", head)

	_, err = readManifestHead(t.TempDir()) // no manifest
	assert.Error(t, err)

	bad := t.TempDir() // malformed manifest
	require.NoError(t, os.WriteFile(filepath.Join(bad, manifestFile), []byte("{not json"), 0o644))
	_, err = readManifestHead(bad)
	assert.Error(t, err)
}

// TestRunVerify_EmptyVerdictReverified: a finding whose existing verdict is empty
// (a contract violation, not a trusted cache) is re-verified (AC 04-05 EC2/ES1).
func TestRunVerify_EmptyVerdictReverified(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM",
		Reviewers: []string{"rev"}, Verification: &reconcile.Verification{Verdict: ""},
	}})
	res, err := runVerify(context.Background(), dir, skepticRegistry(), Options{},
		scriptedHarness(`{"verdict":"confirmed","reasoning":"ok"}`))
	require.NoError(t, err)
	assert.Equal(t, 1, res.FindingsProcessed, "empty verdict is not trusted, finding re-verified")
	assert.Equal(t, "confirmed", readFindings(t, dir)[0].Verification.Verdict)
}

// TestRunVerify_EmitErrorPropagates: a missing summary.json surfaces as an error
// from the summary-update emitter rather than a silent partial success.
func TestRunVerify_EmitErrorPropagates(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	require.NoError(t, os.Remove(filepath.Join(dir, reconciledSubdir, reconcile.SummaryJSON)))
	_, err := runVerify(context.Background(), dir, skepticRegistry(), Options{},
		scriptedHarness(`{"verdict":"confirmed"}`))
	assert.Error(t, err)
}

// TestRunVerify_ManifestUpdateErrorPropagates: a missing manifest.json surfaces
// as an error from the manifest-stage emitter (no skeptic runs, so the manifest
// is untouched until the stage update).
func TestRunVerify_ManifestUpdateErrorPropagates(t *testing.T) {
	reg := skepticRegistry()
	reg.Agents["skep"] = registry.AgentConfig{Provider: "p", Model: "m-rev", Role: registry.RoleSkeptic} // same model -> no eligible skeptic
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	require.NoError(t, os.Remove(filepath.Join(dir, manifestFile)))
	_, err := runVerify(context.Background(), dir, reg, Options{}, scriptedHarness(`{}`))
	assert.Error(t, err)
}

// TestBuildDispatcher covers the snapshot/jail success path (a real git repo) and
// the missing-head and missing-manifest error paths.
// TestRunVerify_WorkerPoolPreservesOrder verifies that a MaxParallel>1 run
// produces the same per-finding verdict ordering as a serial run.
func TestRunVerify_WorkerPoolPreservesOrder(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
		{Severity: "HIGH", File: "b.go", Line: 2, Problem: "leak", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
		{Severity: "HIGH", File: "c.go", Line: 3, Problem: "race", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	}
	reg := skepticRegistry()
	reg.Verify.MaxParallel = 2 // exercise the bounded pool path
	dir := pipelineReview(t, findings)
	handle := func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		return alwaysChat{`{"verdict":"confirmed","reasoning":"ok"}`}, okDispatcher(), nil, nil
	}
	res, err := runVerify(context.Background(), dir, reg, Options{}, handle)
	require.NoError(t, err)
	assert.Equal(t, 3, res.FindingsProcessed)
	updated := readFindings(t, dir)
	require.Len(t, updated, 3)
	for _, f := range updated {
		assert.Equal(t, "VERIFIED", f.Confidence, "each finding confirmed → VERIFIED")
		assert.Equal(t, "confirmed", f.Verification.Verdict)
	}
}

func TestBuildDispatcher(t *testing.T) {
	repo := initGitRepo(t)
	sha := gitHeadSHA(t, repo)

	// Success: manifest head = a real commit SHA → snapshot + jail + dispatcher.
	rev := filepath.Join(repo, "review")
	require.NoError(t, os.MkdirAll(rev, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rev, manifestFile),
		[]byte(`{"base":"a","head":"`+sha+`","stages":["review"]}`), 0o644))
	disp, cleanup, err := buildDispatcher(repo, rev)
	require.NoError(t, err)
	require.NotNil(t, disp)
	require.NotNil(t, cleanup)
	cleanup()

	// Error: manifest with an empty head.
	rev2 := filepath.Join(repo, "review2")
	require.NoError(t, os.MkdirAll(rev2, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rev2, manifestFile),
		[]byte(`{"base":"a","head":"","stages":["review"]}`), 0o644))
	_, _, err = buildDispatcher(repo, rev2)
	assert.Error(t, err)

	// Error: no manifest at all.
	_, _, err = buildDispatcher(repo, t.TempDir())
	assert.Error(t, err)
}
