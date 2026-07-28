package verify

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jsonQuote renders s as a JSON string literal so a multi-line diff can be
// embedded in a scripted agent-mode response.
func jsonQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// chatCallCount reads the Chat counter under the mutex, so a test may read it
// after generateFixes's worker pool has drained without racing the detector.
func (f *fakeChatCompleter) chatCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.chatCalls
}

// sequencedExecutor is a scripted executorCompleter that returns a DIFFERENT
// completion per call, so a test can drive the diff-smell gate's reject-then-retry
// loop (a recordingExecutor returns the same output forever and could never model
// a successful retry). The last output repeats once the script is exhausted.
type sequencedExecutor struct {
	mu      sync.Mutex
	outs    []string
	calls   int
	prompts []string
}

func (s *sequencedExecutor) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.calls
	s.calls++
	s.prompts = append(s.prompts, inv.Prompt)
	if i >= len(s.outs) {
		i = len(s.outs) - 1
	}
	return s.outs[i], nil
}

func (s *sequencedExecutor) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *sequencedExecutor) promptAt(i int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prompts[i]
}

// flakySequencedExecutor is a sequencedExecutor that can also script an error
// at a given call index, so the gate's retry-that-FAILS branch is drivable.
type flakySequencedExecutor struct {
	mu    sync.Mutex
	outs  []string
	errAt map[int]error
	calls int
}

func (s *flakySequencedExecutor) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.calls
	s.calls++
	if err, ok := s.errAt[i]; ok {
		return "", err
	}
	if i >= len(s.outs) {
		i = len(s.outs) - 1
	}
	return s.outs[i], nil
}

func (s *flakySequencedExecutor) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// gateFinding builds one fix-eligible finding at the given path.
func gateFinding(file string) []reconcile.JSONFinding {
	return []reconcile.JSONFinding{
		{Severity: "HIGH", File: file, Line: 1, Problem: "p", Confidence: ConfidenceVerified, Evidence: "ev"},
	}
}

// A clean impl-only diff the retry can return to succeed.
const gateCleanDiff = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 func f() int {
-	return 0
+	return 1
 }
`

// --- HARD verdict: reject, retry once ---

// A HARD smell (test_only) must withhold the first fix and trigger exactly one
// retry; a clean retry is accepted normally.
func TestGenerateFixes_HardSmellRetriesAndAcceptsCleanRetry(t *testing.T) {
	findings := gateFinding("a.go")
	rec := &sequencedExecutor{outs: []string{dsTestOnly, gateCleanDiff}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 2, rec.callCount(), "a HARD smell must trigger exactly one retry")
	assert.Equal(t, strings.TrimSpace(gateCleanDiff), findings[0].Fix, "the clean retry fix is accepted")
	assert.Empty(t, findings[0].FixWarning, "an accepted retry leaves no warning")
	assert.Empty(t, findings[0].FixReview, "a clean retry needs no review annotation")
	assert.Contains(t, findings[0].Evidence, "fix by opus")
}

// The retry prompt must carry the diff-smell evidence so the executor knows what
// was rejected — otherwise the retry is just a re-roll of the same dice.
func TestGenerateFixes_RetryPromptCarriesSmellFeedback(t *testing.T) {
	findings := gateFinding("a.go")
	rec := &sequencedExecutor{outs: []string{dsTestOnly, gateCleanDiff}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	require.Equal(t, 2, rec.callCount())
	first, retry := rec.promptAt(0), rec.promptAt(1)
	assert.NotContains(t, first, smellTestOnly, "the FIRST attempt must not be pre-loaded with feedback")
	assert.Contains(t, retry, smellTestOnly, "the retry prompt must name the rejected smell")
	assert.Contains(t, retry, "rejected", "the retry prompt must say the prior attempt was rejected")
	// The retry instruction belongs in the instruction section, ahead of the
	// untrusted finding-data delimiter buildFixPrompt already establishes.
	assert.Less(t, strings.Index(retry, "rejected"), strings.Index(retry, "\n---\n"),
		"retry instructions must precede the untrusted-data delimiter")
}

// The retry-that-FAILS branch (postCheck over the retry's output returning
// ok=false) must fail exactly as a first attempt would: no Fix, the classified
// FixWarning, and no further retry.
func TestGenerateFixes_HardSmellRetryFailureModes(t *testing.T) {
	cases := []struct {
		name       string
		retryOut   string
		retryErr   error
		wantFix    string
		wantWarn   string
		wantReview string
	}{
		{"provider error", "", errors.New("provider boom"), "", "fix generation failed:", ""},
		{"empty completion", "   ", nil, "", "fix generation returned an empty completion", ""},
		{"decline sentinel", "ATCR_DECLINE: too complex", nil, "", "executor declined:", ""},
		// The non-diff escape hatch is DELIBERATE (clarification Q1): a prose
		// retry is not gateable, so it is accepted as-is. This case pins that so
		// a future tightening cannot break it silently.
		{"free-form prose", "just change the return value to 1", nil, "just change the return value to 1", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := gateFinding("a.go")
			rec := &flakySequencedExecutor{outs: []string{dsTestOnly, tc.retryOut}, errAt: map[int]error{}}
			if tc.retryErr != nil {
				rec.errAt[1] = tc.retryErr
			}
			generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

			assert.Equal(t, 2, rec.callCount(), "exactly one retry, no more")
			assert.Equal(t, tc.wantFix, findings[0].Fix)
			if tc.wantWarn == "" {
				assert.Empty(t, findings[0].FixWarning)
			} else {
				assert.Contains(t, findings[0].FixWarning, tc.wantWarn)
			}
			assert.Equal(t, tc.wantReview, findings[0].FixReview)
		})
	}
}

// Two consecutive HARD verdicts halt the automation: no fix is written, and the
// escalation rides the existing FixWarning contract.
func TestGenerateFixes_SecondHardSmellWithholdsFix(t *testing.T) {
	findings := gateFinding("a.go")
	rec := &sequencedExecutor{outs: []string{dsTestOnly, dsTestOnly}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 2, rec.callCount(), "the gate must not retry more than once")
	assert.Empty(t, findings[0].Fix, "a twice-rejected fix must never be written")
	assert.Contains(t, findings[0].FixWarning, smellTestOnly, "the warning must name the smell that halted it")
	assert.NotContains(t, findings[0].Evidence, "fix by opus", "a withheld fix must not be attributed")
	assert.Empty(t, findings[0].FixReview, "a rejected fix is not a NEEDS_REVIEW acceptance")
}

// A prior tier's successful fix must not be clobbered by a later tier's rejection
// — the same guard the pre-dispatch ceiling skips apply.
func TestGenerateFixes_RejectionPreservesPriorTierFix(t *testing.T) {
	findings := gateFinding("a.go")
	findings[0].Fix = "an earlier tier's good fix"
	rec := &sequencedExecutor{outs: []string{dsTestOnly, dsTestOnly}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, "an earlier tier's good fix", findings[0].Fix, "a prior tier's fix must survive")
	assert.Empty(t, findings[0].FixWarning, "never stamp a warning over a finding that already has a fix")
}

// --- SOFT verdict: accept, annotate ---

func TestGenerateFixes_SoftSmellAcceptedWithFixReview(t *testing.T) {
	findings := gateFinding("a.go")
	rec := &sequencedExecutor{outs: []string{dsSuppression}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 1, rec.callCount(), "a SOFT smell is accepted without a retry")
	assert.Equal(t, strings.TrimSpace(dsSuppression), findings[0].Fix, "a SOFT-smell fix is still written")
	assert.Contains(t, findings[0].FixReview, fixReviewPrefix)
	assert.Contains(t, findings[0].FixReview, smellSuppression)
	assert.Empty(t, findings[0].FixWarning, "an accepted fix must never carry a warning")
	assert.Contains(t, findings[0].Evidence, "fix by opus")
}

// A later clean fix must clear a stale review annotation, mirroring how a valid
// fix clears a stale FixWarning.
func TestGenerateFixes_CleanFixClearsStaleFixReview(t *testing.T) {
	findings := gateFinding("a.go")
	findings[0].FixReview = "NEEDS_REVIEW: stale from a prior run"
	rec := &sequencedExecutor{outs: []string{gateCleanDiff}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Empty(t, findings[0].FixReview, "a clean fix must clear a stale review annotation")
}

// --- non-diff content bypasses the gate entirely (clarification Q1) ---

func TestGenerateFixes_NonDiffFixBypassesGate(t *testing.T) {
	// Prose that would trip stub_body if it were ever scanned as a diff.
	const prose = "Add a TODO-free nil check before the deref, then panic-free early return."
	findings := gateFinding("a.go")
	rec := &sequencedExecutor{outs: []string{prose}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 1, rec.callCount())
	assert.Equal(t, prose, findings[0].Fix, "free-form fix content passes through untouched")
	assert.Empty(t, findings[0].FixReview, "non-diff content is never annotated")
	assert.Empty(t, findings[0].FixWarning)
}

// --- test_only suppression for test-file findings (clarification Q2) ---

// A finding that legitimately lives in a test file produces a test-only fix by
// construction; test_only must be suppressed so it is not a guaranteed rejection.
func TestGenerateFixes_TestOnlySuppressedForTestFileFinding(t *testing.T) {
	findings := gateFinding("internal/verify/select_test.go")
	rec := &sequencedExecutor{outs: []string{dsTestOnlyClean}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 1, rec.callCount(), "a test-file finding must not trigger a retry")
	assert.Equal(t, strings.TrimSpace(dsTestOnlyClean), findings[0].Fix, "the fix is accepted")
	assert.Empty(t, findings[0].FixWarning)
	assert.Empty(t, findings[0].FixReview, "suppression is not a SOFT smell — no annotation")
}

// weakened_assertion is independent of the suppression above: deleting an assert
// is still HARD even when the finding itself lives in a test file.
func TestGenerateFixes_WeakenedAssertionHardEvenForTestFileFinding(t *testing.T) {
	findings := gateFinding("internal/verify/select_test.go")
	rec := &sequencedExecutor{outs: []string{dsWeakenedAssertion, dsWeakenedAssertion}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 2, rec.callCount(), "a weakened assertion must still trigger the retry")
	assert.Empty(t, findings[0].Fix)
	assert.Contains(t, findings[0].FixWarning, smellWeakenedAssertion)
}

// --- agent mode must route its retry through the agent path (clarification Q4) ---

func agentGateConfig() *registry.ExecutorConfig {
	ex := execConfig("MEDIUM")
	ex.AgentMode = true
	return ex
}

func agentGateRegistry() *registry.Registry {
	reg := execRegistry("MEDIUM")
	reg.Executor = agentGateConfig()
	return reg
}

// The gate must fire on agent-mode fixes too, and the retry must re-enter the
// AGENT path — never silently downgrade to the single-shot snippet path.
func TestGenerateFixes_AgentModeHardSmellRetriesViaAgentPath(t *testing.T) {
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: `{"fix":` + jsonQuote(dsTestOnly) + `,"explanation":"e"}`},
		{content: `{"fix":` + jsonQuote(gateCleanDiff) + `,"explanation":"e"}`},
	}}
	rec := &sequencedExecutor{outs: []string{"snippet path must not be used"}}
	findings := gateFinding("a.go")
	generateFixes(context.Background(), findings, agentGateConfig(), agentGateRegistry(), rec, cc, okDispatcher(), 0)

	assert.Equal(t, 0, rec.callCount(), "the agent-mode retry must NOT fall back to the snippet path")
	assert.Equal(t, 2, cc.chatCallCount(), "the retry must re-enter the agent path")
	assert.Equal(t, strings.TrimSpace(gateCleanDiff), findings[0].Fix)
	assert.Empty(t, findings[0].FixWarning)
}

// The agent-mode double-HARD halt must mirror the snippet path: no fix is
// written, the warning names the smell, and the snippet path is never consulted.
func TestGenerateFixes_AgentModeSecondHardSmellWithholdsFix(t *testing.T) {
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: `{"fix":` + jsonQuote(dsTestOnly) + `,"explanation":"e"}`},
		{content: `{"fix":` + jsonQuote(dsTestOnly) + `,"explanation":"e"}`},
	}}
	rec := &sequencedExecutor{outs: []string{"snippet path must not be used"}}
	findings := gateFinding("a.go")
	generateFixes(context.Background(), findings, agentGateConfig(), agentGateRegistry(), rec, cc, okDispatcher(), 0)

	assert.Equal(t, 0, rec.callCount(), "the snippet path must not be consulted")
	assert.Equal(t, 2, cc.chatCallCount(), "the gate must not retry more than once")
	assert.Empty(t, findings[0].Fix, "a twice-rejected fix must never be written")
	assert.Contains(t, findings[0].FixWarning, smellTestOnly, "the warning must name the smell that halted it")
	assert.Empty(t, findings[0].FixReview, "a rejected fix is not a NEEDS_REVIEW acceptance")
}

// The agent-mode SOFT path must mirror the snippet path: the fix is accepted
// and annotated NEEDS_REVIEW via FixReview, with no retry and no warning.
func TestGenerateFixes_AgentModeSoftSmellAcceptedWithFixReview(t *testing.T) {
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: `{"fix":` + jsonQuote(dsSuppression) + `,"explanation":"e"}`},
	}}
	rec := &sequencedExecutor{outs: []string{"snippet path must not be used"}}
	findings := gateFinding("a.go")
	generateFixes(context.Background(), findings, agentGateConfig(), agentGateRegistry(), rec, cc, okDispatcher(), 0)

	assert.Equal(t, 0, rec.callCount(), "the snippet path must not be consulted")
	assert.Equal(t, 1, cc.chatCallCount(), "a SOFT smell is accepted without a retry")
	assert.Equal(t, strings.TrimSpace(dsSuppression), findings[0].Fix)
	assert.Contains(t, findings[0].FixReview, fixReviewPrefix)
	assert.Contains(t, findings[0].FixReview, smellSuppression)
	assert.Empty(t, findings[0].FixWarning, "an accepted fix must never carry a warning")
}

// --- oversized fixes are not scanned (cost guard) ---

func TestGenerateFixes_OversizedFixSkipsGate(t *testing.T) {
	huge := dsTestOnly + strings.Repeat("+// padding\n", (maxFixBytes/12)+1)
	require.Greater(t, len(huge), maxFixBytes)
	findings := gateFinding("a.go")
	rec := &sequencedExecutor{outs: []string{huge}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 1, rec.callCount(), "a pathologically large fix must not be scanned or retried")
	assert.NotEmpty(t, findings[0].Fix)
}

// An UNFENCED unified diff is not Go source. The syntax guard used to parse it
// and stamp a guaranteed-bogus "expected declaration, found diff" — on exactly
// the fix shape --auto-fix consumes. It must be exempt on content, not just on a
// ```diff fence label.
func TestValidateGoFixSyntax_UnifiedDiffExempt(t *testing.T) {
	assert.NoError(t, validateGoFixSyntax(gateCleanDiff), "an unfenced unified diff must not be parsed as Go")
	assert.NoError(t, validateGoFixSyntax("```diff\n"+gateCleanDiff+"```"), "a fenced diff stays exempt")
	// The guard must still catch genuinely broken Go.
	assert.Error(t, validateGoFixSyntax("func add(a, b int) int {\n\treturn a + b\n"))
}

// --- unit-level gate semantics ---

func TestEvaluateFixSmell(t *testing.T) {
	// Non-diff content yields no result at all.
	assert.Nil(t, evaluateFixSmell("just change the return value", "a.go"))
	assert.Nil(t, evaluateFixSmell("", "a.go"))

	// test_only survives for an impl-file finding...
	res := evaluateFixSmell(dsTestOnlyClean, "a.go")
	require.NotNil(t, res)
	assert.Equal(t, smellVerdictHard, res.Summary.Verdict)

	// ...and is suppressed for a test-file finding, taking the verdict with it.
	res = evaluateFixSmell(dsTestOnlyClean, "internal/verify/select_test.go")
	require.NotNil(t, res)
	assert.Equal(t, smellVerdictClean, res.Summary.Verdict)
	assert.NotContains(t, res.Summary.ByType, smellTestOnly)
	assert.Equal(t, 0, res.Summary.Hard)

	// Suppressing test_only must not mask a co-occurring SOFT smell: the verdict
	// drops to soft_only, not clean.
	withNolint := strings.Replace(dsTestOnlyClean, "+	require.NotZero(t, pick())",
		"+	require.NotZero(t, pick()) //nolint:all", 1)
	res = evaluateFixSmell(withNolint, "internal/verify/select_test.go")
	require.NotNil(t, res)
	assert.Equal(t, smellVerdictSoftOnly, res.Summary.Verdict)
	assert.Contains(t, res.Summary.ByType, smellSuppression)

	// Suppressing test_only must NOT mask a co-occurring HARD one: a test-file
	// finding whose fix also drops an assertion stays hard.
	res = evaluateFixSmell(dsTestOnly, "internal/verify/select_test.go")
	require.NotNil(t, res)
	assert.Equal(t, smellVerdictHard, res.Summary.Verdict)
	assert.NotContains(t, res.Summary.ByType, smellTestOnly)
	assert.Contains(t, res.Summary.ByType, smellWeakenedAssertion)
}
