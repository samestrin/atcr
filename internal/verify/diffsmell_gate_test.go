package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
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

	// byFile scripts responses PER FINDING, keyed on the finding's file path. With
	// more than one finding generateFixes runs its workers concurrently, so `outs`
	// — handed out by a shared call counter — maps responses to findings by
	// goroutine scheduling order, which is nondeterministic. Any multi-finding test
	// written against `outs` is flaky by construction. When byFile is set it takes
	// precedence and `outs` is ignored.
	byFile map[string][]string
	perFn  map[string]int
}

func (s *sequencedExecutor) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.calls
	s.calls++
	s.prompts = append(s.prompts, inv.Prompt)

	if s.byFile != nil {
		path := smellPromptLocation(inv.Prompt)
		script, ok := s.byFile[path]
		if !ok || len(script) == 0 {
			return "", fmt.Errorf("sequencedExecutor: no script for finding %q", path)
		}
		if s.perFn == nil {
			s.perFn = map[string]int{}
		}
		j := s.perFn[path]
		s.perFn[path]++
		if j >= len(script) {
			j = len(script) - 1
		}
		return script[j], nil
	}

	if i >= len(s.outs) {
		i = len(s.outs) - 1
	}
	return s.outs[i], nil
}

// smellPromptLocation recovers the finding's file path from a fix prompt.
// buildFixPrompt writes it as `Location: <file>:<line>`, which is the only
// per-finding key available to a scripted executor.
func smellPromptLocation(prompt string) string {
	for _, line := range strings.Split(prompt, "\n") {
		rest, ok := strings.CutPrefix(line, "Location: ")
		if !ok {
			continue
		}
		if i := strings.LastIndex(rest, ":"); i >= 0 {
			return rest[:i]
		}
		return rest
	}
	return ""
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

// callsFor reports how many times the finding at path was served from byFile.
func (s *sequencedExecutor) callsFor(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.perFn[path]
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
	ctx, buf := ceilingCtx()
	generateFixes(ctx, findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 2, rec.callCount(), "a HARD smell must trigger exactly one retry")
	assert.Equal(t, strings.TrimSpace(gateCleanDiff), findings[0].Fix, "the clean retry fix is accepted")
	assert.Empty(t, findings[0].FixWarning, "an accepted retry leaves no warning")
	assert.Empty(t, findings[0].FixReview, "a clean retry needs no review annotation")
	assert.Contains(t, findings[0].Evidence, "fix by opus")
	assert.Contains(t, buf.String(), "class=executor_smell_reject", "the rejection must be visible in the run log — it is the only production signal that the gate fired")
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
	findings[0].FixReview = "NEEDS_REVIEW: stale from a prior run"
	rec := &sequencedExecutor{outs: []string{dsTestOnly, dsTestOnly}}
	ctx, buf := ceilingCtx()
	generateFixes(ctx, findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 2, rec.callCount(), "the gate must not retry more than once")
	assert.Empty(t, findings[0].Fix, "a twice-rejected fix must never be written")
	assert.Contains(t, findings[0].FixWarning, smellTestOnly, "the warning must name the smell that halted it")
	assert.NotContains(t, findings[0].Evidence, "fix by opus", "a withheld fix must not be attributed")
	assert.Empty(t, findings[0].FixReview, "a rejected fix is not a NEEDS_REVIEW acceptance — a stale one must be cleared")
	assert.Equal(t, 2, strings.Count(buf.String(), `msg="pipeline warning" class=executor_smell_reject`), "both rejection sites (retrying, halted) must log the class")
}

// An empty retry completion is a failure path too: it must not leave a stale
// FixReview beside the freshly stamped FixWarning.
func TestGenerateFixes_EmptyRetryClearsStaleFixReview(t *testing.T) {
	findings := gateFinding("a.go")
	findings[0].FixReview = "NEEDS_REVIEW: stale from a prior run"
	rec := &sequencedExecutor{outs: []string{dsTestOnly, "   "}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 2, rec.callCount())
	assert.Empty(t, findings[0].Fix)
	assert.Contains(t, findings[0].FixWarning, "empty completion")
	assert.Empty(t, findings[0].FixReview, "a failed retry must clear a stale review annotation")
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

// --- retry cost: snippet reuse + retry counter (TD: executor.go:395) ---

// The snippet cannot change between the first attempt and the smell retry — the
// review snapshot is immutable for the run — so it must be read ONCE, not
// re-dispatched per attempt.
func TestGenerateFixes_SmellRetryReusesSnippet(t *testing.T) {
	findings := gateFinding("a.go")
	rec := &sequencedExecutor{outs: []string{dsTestOnly, gateCleanDiff}}
	disp := okDispatcher()
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, disp, 0)

	require.Equal(t, 2, rec.callCount(), "a HARD smell must trigger exactly one retry")
	assert.Equal(t, 1, disp.count(), "the retry must reuse the first attempt's snippet, not re-read it")
}

// The smell-gate retry count rides generateFixes's return so the run Result can
// surface a systematically rejected executor's doubled model spend.
func TestGenerateFixes_SmellRetryCount(t *testing.T) {
	findings := gateFinding("a.go")
	rec := &sequencedExecutor{outs: []string{dsTestOnly, gateCleanDiff}}
	n := generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)
	assert.Equal(t, 1, n, "one HARD verdict = one retry")

	findings = gateFinding("a.go")
	rec = &sequencedExecutor{outs: []string{gateCleanDiff}}
	n = generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)
	assert.Equal(t, 0, n, "a clean first attempt costs no retry")
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
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := log.NewContext(context.Background(), logger)
	generateFixes(ctx, findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 1, rec.callCount(), "a pathologically large fix must not be scanned or retried")
	assert.NotEmpty(t, findings[0].Fix)
	assert.Contains(t, buf.String(), "executor_smell_skipped", "an unscanned fix must be announced — never a SILENT bypass")
	// Unscanned means unscanned in BOTH directions: the gate produced no rejection
	// and no NEEDS_REVIEW annotation. `huge` embeds dsTestOnly, which WOULD score
	// HARD if it were scanned, so these pin the skip rather than restating it.
	assert.Empty(t, findings[0].FixWarning, "a skipped scan must not manufacture a rejection")
	assert.Empty(t, findings[0].FixReview, "a skipped scan must not manufacture an annotation")

	// An under-cap fix must NOT emit the skip record: the log distinguishes
	// "scanned and clean" from "never scanned", so both directions are pinned.
	buf.Reset()
	small := gateFinding("a.go")
	rec2 := &sequencedExecutor{outs: []string{gateCleanDiff}}
	generateFixes(ctx, small, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec2, nil, okDispatcher(), 0)
	assert.NotContains(t, buf.String(), "executor_smell_skipped", "a scanned fix must not be reported as skipped")
}

// The gate has TWO nil-return shapes and both are bypasses, so both must leave a
// trace. Oversize was announced from the start; !looksLikeUnifiedDiff was silent,
// so a genuine diff that failed the heuristic (a context diff, a mangled header)
// slipped past with nothing in the log to say the scan never ran — exactly the
// asymmetry the oversize announcement exists to prevent.
func TestGenerateFixes_NonDiffFixLogsGateSkip(t *testing.T) {
	findings := gateFinding("a.go")
	rec := &sequencedExecutor{outs: []string{"change meetsSeverityFloor to use >= instead of >"}}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := log.NewContext(context.Background(), logger)
	generateFixes(ctx, findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.NotEmpty(t, findings[0].Fix, "a free-form fix is still accepted, only unscanned")
	assert.Contains(t, buf.String(), "executor_smell_skipped",
		"a fix the gate cannot scan must leave a trace, diff-shaped or not")
	assert.Empty(t, findings[0].FixWarning, "an unscanned fix must not manufacture a rejection")
	assert.Empty(t, findings[0].FixReview, "an unscanned fix must not manufacture an annotation")
}

// smellScanSkipReason is the single source of truth for "the gate cannot scan
// this". Pinning it directly is what keeps the threshold from drifting back
// apart between evaluateFixSmell and the executor's scanFix.
func TestSmellScanSkipReason(t *testing.T) {
	assert.Equal(t, "", smellScanSkipReason(gateCleanDiff), "a scannable diff has no skip reason")

	huge := gateCleanDiff + strings.Repeat("+// padding\n", (maxFixBytes/12)+1)
	require.Greater(t, len(huge), maxFixBytes)
	assert.Contains(t, smellScanSkipReason(huge), "scan cap", "an oversized fix reports the cap")

	assert.Contains(t, smellScanSkipReason("just change the return value"), "unified diff",
		"a non-diff fix reports the shape mismatch")

	// Oversize wins over shape so the reason names the more suspicious cause.
	assert.Contains(t, smellScanSkipReason(strings.Repeat("x", maxFixBytes+1)), "scan cap")
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

// --- multi-finding gate behaviour (per-finding scripting) ---

// gateFindings builds one fix-eligible finding per path, so a test can drive
// generateFixes' worker pool with more than one finding — the shape every gate
// test above avoids, and the one the gate itself changed by adding a SECOND
// executor call inside each worker goroutine.
func gateFindings(files ...string) []reconcile.JSONFinding {
	out := make([]reconcile.JSONFinding, 0, len(files))
	for _, f := range files {
		out = append(out, reconcile.JSONFinding{
			Severity: "HIGH", File: f, Line: 1, Problem: "p",
			Confidence: ConfidenceVerified, Evidence: "ev",
		})
	}
	return out
}

// With N > 1 findings the workers run concurrently, so a shared call counter maps
// responses to findings by scheduling order — nondeterministic by construction.
// Scripting per finding (byFile) makes the mapping total and stable, which is what
// lets this test assert each finding's OWN Fix/FixWarning.
func TestGenerateFixes_MultiFindingGateIsPerFinding(t *testing.T) {
	for i := 0; i < 25; i++ {
		findings := gateFindings("a.go", "b.go")
		rec := &sequencedExecutor{byFile: map[string][]string{
			// a.go returns the reward hack twice: rejected, retried, halted.
			"a.go": {acAssertionDeleted, acAssertionDeleted},
			// b.go returns an honest fix first time: accepted, no retry.
			"b.go": {gateCleanDiff},
		}}
		generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

		assert.Empty(t, findings[0].Fix, "a.go: a twice-rejected patch must never reach Fix")
		assert.Contains(t, findings[0].FixWarning, smellWeakenedAssertion, "a.go: the halt must name its smell")

		assert.Equal(t, strings.TrimSpace(gateCleanDiff), findings[1].Fix, "b.go: the honest fix must be accepted")
		assert.Empty(t, findings[1].FixWarning, "b.go: an accepted fix must not inherit a.go's rejection")

		assert.Equal(t, 2, rec.callsFor("a.go"), "a.go: one attempt plus exactly one retry")
		assert.Equal(t, 1, rec.callsFor("b.go"), "b.go: accepted first time, no retry")
	}
}

// The "never a SILENT bypass" oversize check guards only the FIRST attempt. The
// retry feeds straight into evaluateFixSmell, which returns nil for anything over
// maxFixBytes — so an oversized RETRY is accepted as clean with no warning, no log
// record and no FixReview. That is exactly the escape a model just told "your
// previous attempt was rejected" would find, and it fails open on the MORE
// suspicious of the two attempts.
func TestGenerateFixes_OversizedRetryIsAnnouncedNotSilentlyAccepted(t *testing.T) {
	// A reward hack (test_only + weakened_assertion) padded past the scan cap.
	huge := dsTestOnly + strings.Repeat("+// padding\n", (maxFixBytes/12)+1)
	require.Greater(t, len(huge), maxFixBytes)

	findings := gateFinding("a.go")
	rec := &sequencedExecutor{outs: []string{dsTestOnly, huge}}
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	generateFixes(ctx, findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	require.Equal(t, 2, rec.callCount(), "one attempt plus exactly one retry")
	assert.Contains(t, buf.String(), "executor_smell_skipped",
		"an UNSCANNED retry must be announced on the same terms as an unscanned first attempt")
}
