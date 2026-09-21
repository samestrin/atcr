package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repoStateMiniPath is the in-repo repo-state fixture suite (1 case, 2 expected
// findings), relative to cli/. Its numbers are known exactly, so these tests do
// not move when the SHIPPED suite gains a case.
const repoStateMiniPath = "../internal/benchmark/testdata/repo-state-mini"

// stubLocatedCompleter raises findings at the fixture's two settling lines, so a
// run scores 1.0 on both halves of the positional metric. app/calc.py:12 is a
// context line (the outside_diff:true expectation) and app/calc.py:8 is an added
// line (the outside_diff:false one).
type stubLocatedCompleter struct{}

func (stubLocatedCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "HIGH|app/calc.py:12|average divides by len(items) with no empty guard|add a guard|correctness|15|return total(items) / len(items)\n" +
		"MEDIUM|app/calc.py:8|safe_total is not None-safe as claimed|handle None explicitly|correctness|15|def safe_total(items):", nil
}

// stubOnlyUntouchedFileCompleter raises ONLY the defect in the file the diff never
// touches, so the Epic 14.1 gate drops every finding it emits (isGrounded returns
// false on the file lookup itself — internal/fanout/grounding.go:70-73 — before the
// line and evidence arms are consulted).
//
// A TOTAL wipe is what OutcomeUngrounded requires, and it is why the sibling
// stubUntouchedFileCompleter cannot produce one: that stub also cites the in-patch
// calc.py defect, which survives, so its reviewer scores `findings` instead.
type stubOnlyUntouchedFileCompleter struct{}

func (stubOnlyUntouchedFileCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "HIGH|app/helper.py:2|average divides by len(items) with no empty guard|add a guard|correctness|15|return total(items) / len(items)", nil
}

// stubInDiffOnlyCompleter raises ONLY the in-diff finding. It is the reviewer this
// whole tier exists to identify: competent on the diff, blind to unchanged code.
type stubInDiffOnlyCompleter struct{}

func (stubInDiffOnlyCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "MEDIUM|app/calc.py:8|safe_total is not None-safe as claimed|handle None explicitly|correctness|15|def safe_total(items):", nil
}

// T7's headline: a repo-state-v1 invocation runs end to end and reports both
// metrics — the category recall standard-v1 already produced, and the new
// positional recall split by outside_diff.
// writeTwoCaseSuite materializes a two-case temp suite shaped exactly like the
// mini fixture (same diff, base tree and message; only the ids differ), so tests
// can drive a multi-case run and fault one case. Case 2 is returned intact;
// tests overwrite its files to plant the fault.
func writeTwoCaseSuite(t *testing.T) string {
	t.Helper()
	return writeCaseSuite(t, "first-case", "second-case")
}

// writeCaseSuite is writeTwoCaseSuite for an arbitrary case count, so a test can
// fault a MIDDLE case and assert the ones on either side of it still scored — the
// shape a two-case suite cannot express, since faulting either of its cases leaves
// nothing after the fault.
func writeCaseSuite(t *testing.T, ids ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, id := range ids {
		dir := filepath.Join(root, id)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "base", "app"), 0o755))
		raw, err := os.ReadFile(filepath.Join(repoStateMiniPath, "mini-case", "case.json"))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "case.json"),
			[]byte(strings.ReplaceAll(string(raw), "mini-case", id)), 0o600))
		for _, f := range []string{"change.diff", "commit-message.txt"} {
			raw, rerr := os.ReadFile(filepath.Join(repoStateMiniPath, "mini-case", f))
			require.NoError(t, rerr)
			require.NoError(t, os.WriteFile(filepath.Join(dir, f), raw, 0o600))
		}
		raw, rerr := os.ReadFile(filepath.Join(repoStateMiniPath, "mini-case", "base", "app", "calc.py"))
		require.NoError(t, rerr)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "base", "app", "calc.py"), raw, 0o600))
	}
	entries := make([]string, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, fmt.Sprintf("{%q:%q,%q:%q}", "id", id, "dir", id))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "suite.json"), []byte(fmt.Sprintf(
		"{\"suite\":\"repo-state-v1\",\"suite_version\":\"1.0.0\",\"cases\":[%s]}", strings.Join(entries, ","))), 0o600))
	return root
}

// faultMaterialization makes one case's diff well-formed to the loader (which
// parses every diff at load) but unappliable by git: pkg/absent.py is not in the
// base tree, so MaterializeCase fails MID-LOOP, after the work dir exists and any
// earlier case has already been paid for.
func faultMaterialization(t *testing.T, suite, caseID string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(suite, caseID, "change.diff"),
		[]byte("diff --git a/pkg/absent.py b/pkg/absent.py\n--- a/pkg/absent.py\n+++ b/pkg/absent.py\n@@ -1,1 +1,1 @@\n-gone\n+here\n"), 0o600))
}

// faultUnwinnableExpectation pushes one case's expectation past the end of the
// head file it cites, so benchmark.ValidateAgainstHead rejects it. That is a
// suite-AUTHORING defect: deterministic, unchanged by a re-run, and detected
// before this case's first paid completer call — which is why it keeps aborting
// the run rather than joining the failure channel.
func faultUnwinnableExpectation(t *testing.T, suite, caseID string) {
	t.Helper()
	path := filepath.Join(suite, caseID, "case.json")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	faulted := strings.Replace(string(raw), `"line_end": 13`, `"line_end": 9001`, 1)
	require.NotEqual(t, string(raw), faulted, "the fixture's expectation bounds must still be the string this helper rewrites")
	require.NoError(t, os.WriteFile(path, []byte(faulted), 0o600))
}

// failingCompleter fails every call, so every slot in the panel fails and
// fanout.ExecuteReview returns ErrAllAgentsFailed.
type failingCompleter struct{}

func (failingCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "", errors.New("provider unreachable")
}

// countingLocatedCompleter drives the real panel while counting paid completer
// calls, so a pre-flight guarantee ("the error precedes any completer call") is
// assertable rather than assumed.
type countingLocatedCompleter struct {
	calls int
}

func (c *countingLocatedCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	c.calls++
	return stubLocatedCompleter{}.Complete(ctx, inv)
}

// loadCaseDiffLineMap runs INSIDE the paid per-case loop, so case N's diff is
// first read and parsed only after cases 1..N-1 have driven the whole reviewer
// panel — a mid-panel parse error forfeits every case already paid for, the
// exact fail-late shape LoadRepoState's eager-load contract names one level up.
// The parse must be pre-flight: every case's diff parsed before the FIRST
// completer call, where the remedy is free.
func TestExecuteRepoStateBenchmarkRun_ParsesEveryCaseDiffBeforeAnyCompleterCall(t *testing.T) {
	suite := writeTwoCaseSuite(t)
	// Case 2's diff is malformed: a hunk header the parser rejects outright.
	require.NoError(t, os.WriteFile(filepath.Join(suite, "second-case", "change.diff"),
		[]byte("diff --git a/app/calc.py b/app/calc.py\n--- a/app/calc.py\n+++ b/app/calc.py\n@@ -1 +x @@\n-old\n+new\n"), 0o600))
	cc := &countingLocatedCompleter{}

	_, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), cc, suite, time.Unix(0, 0).UTC(), 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "second-case", "the error names the defective case")
	assert.Zero(t, cc.calls, "a malformed case-2 diff must be rejected before ANY completer call, not after case 1's panel was paid for")
}

// Nothing frees the materialized repositories between cases: every repo-i (a
// full copied base tree plus a two-commit .git) and every review-i accumulate
// under one temp dir for the life of the run — doubling the footprint the
// standard tier leaves with real source trees, on a $TMPDIR volume a large
// base-tree suite can exhaust mid-panel. The repo has no consumer once the
// findings are read, so each case's repo must be released when its case
// completes, not at run end.
func TestExecuteRepoStateBenchmarkRun_ReleasesEachCaseRepoAfterItsCase(t *testing.T) {
	// Scoped to dirs created after the test began: failed runs now RETAIN their
	// work dirs (see the retention test), and those linger in $TMPDIR — counting
	// them would credit this run with repos it did not create.
	testStart := time.Now()
	suite := writeTwoCaseSuite(t)
	cc := &repoCountingCompleter{since: testStart}

	_, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), cc, suite, time.Unix(0, 0).UTC(), 0)
	require.NoError(t, err)

	require.NotEmpty(t, cc.reposSeenPerCall, "the stub completer must have been called")
	assert.Equal(t, 1, cc.reposSeenPerCall[len(cc.reposSeenPerCall)-1],
		"case 2's completer call must see only case 2's repo; case 1's must already be released")
}

// repoCountingCompleter counts the repo-N directories visible under the run's
// temp prefix at every completer call — the only observation point a test has
// inside the paid loop.
type repoCountingCompleter struct {
	since            time.Time
	reposSeenPerCall []int
}

func (c *repoCountingCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "atcr-repo-state-*", "repo-*"))
	n := 0
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.ModTime().After(c.since) {
			n++
		}
	}
	c.reposSeenPerCall = append(c.reposSeenPerCall, n)
	return stubLocatedCompleter{}.Complete(ctx, inv)
}

// A base tree carrying its own .atcrignore that excludes the changed paths
// makes PrepareReview resolve the range to zero reviewable files and abort the
// run mid-panel — after earlier cases were paid for. A repo-state case's
// reviewable set is its DIFF (the planted change), not the ignore policy of the
// tree it ships, so the runner opts the review out of ignore filtering.
func TestExecuteRepoStateBenchmarkRun_ReviewsACaseWhoseTreeSelfIgnoresTheChange(t *testing.T) {
	suite := writeTwoCaseSuite(t)
	require.NoError(t, os.WriteFile(filepath.Join(suite, "second-case", "base", ".atcrignore"),
		[]byte("app/\n"), 0o600))
	cc := &countingLocatedCompleter{}

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), cc, suite, time.Unix(0, 0).UTC(), 0)
	require.NoError(t, err, "a case whose tree ignores its own changed path must still be reviewed: the diff defines the reviewable set")
	assert.Equal(t, 2, cc.calls, "both cases' panels must have run")
	assert.Contains(t, rr.SuiteCaseIDs, "second-case")
}

// Two lanes resolving to ONE realized (model, persona) both append a CaseScore
// for the same case, silently doubling Runs and re-weighting CorroborationRate.
// The runner must fail closed, and the diagnostic must name BOTH colliding lanes
// so the operator can repartition the roster.
func TestExecuteRepoStateBenchmarkRun_RefusesTwoLanesSharingOneIdentity(t *testing.T) {
	// Two DISTINCT agents whose configured persona AND model are identical realize
	// one (model, persona) identity. An explicit persona ref distinct from the
	// agent name must resolve to a file, so the fixture writes one shared persona
	// and points both lanes at it.
	personaDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(personaDir, "shared.md"), []byte("shared persona prompt\n"), 0o600))
	cfg := &fanout.ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: "http://unused"}},
			Agents: map[string]registry.AgentConfig{
				"lane-a": {Provider: "p", Model: "m-shared", Persona: "shared", Temperature: ptrF(0.7)},
				"lane-b": {Provider: "p", Model: "m-shared", Persona: "shared", Temperature: ptrF(0.7)},
			},
		},
		Project:     &registry.ProjectConfig{Agents: []string{"lane-a", "lane-b"}},
		Settings:    registry.Settings{PayloadMode: "diff", TimeoutSecs: 600},
		PersonaDirs: registry.PersonaDirs{Project: personaDir},
	}

	_, _, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDirFromError(t, err)

	require.Error(t, err, "two lanes sharing one realized identity must fail closed, not double the score")
	assert.Contains(t, err.Error(), "scored twice")
	assert.Contains(t, err.Error(), "lane-a", "the diagnostic names both colliding agents")
	assert.Contains(t, err.Error(), "lane-b")
}

// usageLocatedCompleter raises the standard located findings while REPORTING
// token usage and burning a real per-call delay — the only way to drive the
// usage-gated cost and latency arms of the score fold, which every plain stub
// (zero usage) leaves uncovered.
type usageLocatedCompleter struct {
	delay []time.Duration
	calls int
}

func (c *usageLocatedCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	content, _, _, err := c.CompleteWithUsage(ctx, inv)
	return content, err
}

func (c *usageLocatedCompleter) CompleteWithUsage(_ context.Context, inv llmclient.Invocation) (string, llmclient.UsageData, []llmclient.CallRecord, error) {
	i := c.calls
	if i < len(c.delay) {
		time.Sleep(c.delay[i])
	}
	c.calls++
	content, err := stubLocatedCompleter{}.Complete(context.Background(), llmclient.Invocation{})
	return content, llmclient.UsageData{PromptTokens: 100, CompletionTokens: 50}, nil, err
}

// The cost and latency fold is usage-gated, and the latency arm's claimed shape
// is load-bearing: latencies are COLLECTED into a slice and MEDIANED
// (LatencyP50MS is a median on the frozen public row), while cost ACCUMULATES.
// No test ever reported non-zero usage, so a regression back to assignment —
// publishing the LAST case's wall clock under a median column — shipped
// silently. Two cases with differing real durations discriminate: assignment
// publishes the second duration; a median lands between the two.
func TestExecuteRepoStateBenchmarkRun_MedianLatencyAndAccumulatedCost(t *testing.T) {
	// 30ms + 300ms: the median sits near 165ms even under generous CI jitter, and
	// an assignment regression publishes the second duration (~300ms+) — the two
	// stay separated by more than the jitter either way.
	cc := &usageLocatedCompleter{delay: []time.Duration{30 * time.Millisecond, 300 * time.Millisecond}}

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "gpt-4o", "greta"}), cc, writeTwoCaseSuite(t), time.Unix(0, 0).UTC(), 0)
	require.NoError(t, err)

	require.Len(t, rr.Reviewers, 1)
	t.Logf("DEBUG cost_per=%v latency=%v model=%q", rr.Reviewers[0].CostPerCorroboratedFindingUSD, rr.Reviewers[0].LatencyP50MS, rr.Reviewers[0].Model)
	p50 := rr.Reviewers[0].LatencyP50MS
	assert.Greater(t, p50, int64(20), "the median must reflect both cases' durations, not zero")
	assert.Less(t, p50, int64(250),
		"LatencyP50MS is a MEDIAN over collected case durations; assignment would publish the last case's ~300ms+")

	// rr.Reviewers carries the SCRUBBED public rows, so the accumulated cost
	// surfaces as cost-per-corroborated (CostUSD / matched findings). Each case
	// costs ComputeCostUSD("gpt-4o", 100, 50), the fold must ACCUMULATE both, and
	// the stub's two findings corroborate both expected findings of both cases —
	// a denominator of 4. The per-finding quotient is therefore discriminative:
	// a fold that kept only the last case's usage would halve it.
	wantCost := llmclient.ComputeCostUSD("gpt-4o", 100, 50) + llmclient.ComputeCostUSD("gpt-4o", 100, 50)
	require.NotNil(t, rr.Reviewers[0].CostPerCorroboratedFindingUSD,
		"the matched findings must carry the priced cost, not nil (unmeasured)")
	assert.InDelta(t, wantCost/4, *rr.Reviewers[0].CostPerCorroboratedFindingUSD, 1e-9,
		"cost ACCUMULATES over every case's reported usage (denominator: 2 cases x 2 corroborated findings)")
}

// The coverage array was emitted in first-sighting slot order while Reviewers,
// Vocabulary and PositionalRecall each come back re-sorted on the scrubbed
// identity — so on any panel with more than one identity, the documented
// positional join (coverage[i] describes reviewers[i]) was false for every
// repo-state run-result. All four arrays must share one order.
func TestExecuteRepoStateBenchmarkRun_CoverageJoinsReviewersByPosition(t *testing.T) {
	// Roster order (zeta, alpha) is first-sighting order; the scrubbed sort the
	// reviewer rows use puts alpha first. The join must hold despite that.
	cfg := benchCfg(
		[3]string{"zeta", "m-zeta", "zeta"},
		[3]string{"alpha", "m-alpha", "alpha"},
	)

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC(), 0)
	require.NoError(t, err)
	require.Len(t, rr.Reviewers, 2)
	require.Len(t, rr.Coverage, 2)

	for i := range rr.Reviewers {
		assert.Equal(t, rr.Reviewers[i].Model, rr.Coverage[i].Model,
			"coverage[%d] must describe reviewers[%d]: the positional join is documented", i, i)
		assert.Equal(t, rr.Reviewers[i].Persona, rr.Coverage[i].Persona)
	}
}

// scorecard's scrub is NOT injective: it deletes path-, home- and credential-
// shaped tokens, so two DISTINCT raw identities can fold into one public one.
// This runner folds per RAW key, so both identities emit their own Reviewers row
// carrying the same public identity — which checkCoverage then rejects as a
// hand-assembled file AFTER the whole panel was paid for, with a diagnostic that
// cannot see the raw strings. The producer must name both pre-scrub identities.
func TestExecuteRepoStateBenchmarkRun_RefusesDistinctIdentitiesScrubbingToOne(t *testing.T) {
	// Both models are credential-shaped: the scrub deletes the whole token, so two
	// DISTINCT raw models fold into the same (empty) public model beside the
	// shared persona.
	personaDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(personaDir, "shared.md"), []byte("shared persona prompt\n"), 0o600))
	cfg := &fanout.ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: "http://unused"}},
			Agents: map[string]registry.AgentConfig{
				"lane-a": {Provider: "p", Model: "bedrock@us-east-1/claude", Persona: "shared", Temperature: ptrF(0.7)},
				"lane-b": {Provider: "p", Model: "x@corp/claude", Persona: "shared", Temperature: ptrF(0.7)},
			},
		},
		Project:     &registry.ProjectConfig{Agents: []string{"lane-a", "lane-b"}},
		Settings:    registry.Settings{PayloadMode: "diff", TimeoutSecs: 600},
		PersonaDirs: registry.PersonaDirs{Project: personaDir},
	}

	_, _, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDirFromError(t, err)

	require.Error(t, err, "two identities scrubbing to one public identity must fail at the producer, not at export after the panel was paid")
	assert.Contains(t, err.Error(), "scrub to the same public identity")
	assert.Contains(t, err.Error(), "bedrock@us-east-1/claude", "the error names BOTH pre-scrub identities; the post-scrub value identifies nothing the operator can edit")
	assert.Contains(t, err.Error(), "x@corp/claude", "the second pre-scrub identity is named too")
}

// The coverage rows are built from the RAW accumulator key, while Reviewers,
// Vocabulary and PositionalRecall all emit the SCRUBBED identity. A credential-
// or path-shaped model id therefore ships verbatim inside reviewer_coverage[] —
// the exact identity leak the sibling array refuses — and the export join only
// survives because coverageKey re-scrubs on read. Scrub once at the producer;
// the arrays must carry the same identity by construction.
func TestExecuteRepoStateBenchmarkRun_CoverageCarriesTheScrubbedIdentity(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "bedrock@us-east-1/claude", "greta"})

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC(), 0)
	require.NoError(t, err)
	require.Len(t, rr.Reviewers, 1)
	require.Len(t, rr.Coverage, 1)

	assert.NotEqual(t, "bedrock@us-east-1/claude", rr.Coverage[0].Model,
		"reviewer_coverage must not publish a credential-shaped model id verbatim")
	assert.Equal(t, rr.Reviewers[0].Model, rr.Coverage[0].Model,
		"the coverage row and the reviewer row carry the SAME scrubbed identity")
	assert.Equal(t, rr.Reviewers[0].Persona, rr.Coverage[0].Persona)
}

// The Expected projection deduped on the RAW category string while every other
// consumer of the same field normalizes first (Score's normalizeDistinct,
// validateCategoryEquivalence's normalize) — a case carrying 'Correctness'
// beside 'correctness' yielded two Expected entries here and one everywhere
// else. The projection must dedupe by the same rule.
func TestExecuteRepoStateBenchmarkRun_DedupesExpectedCategoriesCaseInsensitively(t *testing.T) {
	suite := writeTwoCaseSuite(t)
	raw, err := os.ReadFile(filepath.Join(suite, "first-case", "case.json"))
	require.NoError(t, err)
	patched := strings.Replace(string(raw),
		`"category": "correctness",`+"\n"+`      "summary": "The commit message claims`,
		`"category": "Correctness",`+"\n"+`      "summary": "The commit message claims`, 1)
	require.NotEqual(t, string(raw), patched, "the fixture patch must have matched the second finding's category")
	require.NoError(t, os.WriteFile(filepath.Join(suite, "first-case", "case.json"), []byte(patched), 0o600))

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	require.NoError(t, err)
	require.NotEmpty(t, rr.Reviewers)
	// The raw Expected slice is internal; the observable is the recall math. The
	// stub raises correctness twice per case, so case 1 recalls 1/1 with a
	// normalized dedupe and 1/2 with a raw-string dedupe (the invented second
	// entry matches nothing). Across two otherwise-perfect cases that is the
	// difference between CorroborationRate 1.0 and 0.75.
	assert.InDelta(t, 1.0, rr.Reviewers[0].CorroborationRate, 1e-9,
		"two categories differing only by case are ONE expected category everywhere else; a raw-string dedupe invents a second entry that caps recall")
}

// loadCaseDiffLineMap reads the case's diff with NO size cap: a third-party
// --suite-path can carry a multi-gigabyte change.diff that OOMs the process at
// read/parse time. The standard tier caps the same class of input at
// MaxDiffBytes (ReproHash rejects an oversized diff); this read must be bounded
// identically.
func TestExecuteRepoStateBenchmarkRun_RejectsAnOversizedCaseDiff(t *testing.T) {
	suite := writeTwoCaseSuite(t)
	big := "diff --git a/big.txt b/big.txt\n" +
		"--- a/big.txt\n" +
		"+++ b/big.txt\n" +
		"@@ -0,0 +1,2 @@\n" +
		"+" + strings.Repeat("x", 10*1024*1024+1) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(suite, "second-case", "change.diff"), []byte(big), 0o600))

	_, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)

	require.Error(t, err, "a diff beyond the documented byte cap must be rejected before the read, not parsed")
	assert.Contains(t, err.Error(), "exceeding", "the error names the cap")
}

// The repo-state validator's doc claims the two tiers "cannot drift" on what a
// publishable suite is. That holds only while the arm TABLE is one function:
// both validators must produce byte-identical messages for the same bad name.
func TestValidatePublishableCaseIDs_BothTiersShareTheIdentityArms(t *testing.T) {
	std := &benchmark.Manifest{Suite: "bedrock@us-east-1/claude", SuiteVersion: "1.0.0"}
	repo := &benchmark.RepoStateManifest{Suite: "bedrock@us-east-1/claude", SuiteVersion: "1.0.0"}

	errStd := validateSuitePublishableCaseIDs(std, "/suite")
	errRepo := validateRepoStatePublishableCaseIDs(repo, "/suite")

	require.Error(t, errStd)
	require.Error(t, errRepo)
	assert.Equal(t, errStd.Error(), errRepo.Error(),
		"same bad suite name, same message: the arm table is shared, not copied")
}

// The skipped-row fold's unattributed tally: a skipped row whose recovered
// trailing field is NOT a known agent name keys a categorical entry nobody reads
// — effectively dropped from every denominator. The tally counts it so the drop
// is measured, and the fold still happens (attribution, not deletion, is the
// fix — there is no agent to attribute it to).
func TestReadCaseFindingsLocated_CountsUnattributedSkippedRows(t *testing.T) {
	dir := t.TempDir()
	pool := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(pool, 0o755))
	// One well-formed row for greta, one over-column row whose recovered reviewer
	// is "nobody" — not a key in the agent set.
	content := "# atcr-findings/v1\n" +
		"HIGH|app/calc.py:12|p|f|correctness|15|sol|greta\n" +
		"HIGH|app/calc.py:13|p|f|correctness|15|sol|extra|nobody\n"
	require.NoError(t, os.WriteFile(filepath.Join(pool, "findings.txt"), []byte(content), 0o600))

	located, categorical, unattributed, _, err := readCaseFindingsLocated(dir, map[string]bool{"greta": true})
	require.NoError(t, err)
	assert.Equal(t, 1, unattributed, "the skipped row naming an unknown reviewer is counted, not silently dropped")
	assert.Len(t, located["greta"], 1)
	assert.Len(t, categorical["greta"], 1)

	// With nobody ON the panel the same row is attributed normally.
	_, _, unattributed, _, err = readCaseFindingsLocated(dir, map[string]bool{"greta": true, "nobody": true})
	require.NoError(t, err)
	assert.Zero(t, unattributed)
}

// The skipped-row fold is what keeps a malformed row in the out-of-vocabulary
// DENOMINATOR: dropping it would flatter the worst-formed reviewer with the best
// drift rate. Driven at the unit level deliberately — ParseModelOutput folds
// model-output overflow back into EVIDENCE before findings.txt is written, so a
// skipped row reaches this reader only via a hand-assembled or legacy pool,
// which is exactly the input this fold defends against. A regression that
// dropped skipped rows publishes 1/0 — a flawless rate for garbage output —
// and this test is what would catch it.
func TestReadCaseFindingsLocated_SkippedRowStaysInTheVocabularyDenominator(t *testing.T) {
	dir := t.TempDir()
	pool := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(pool, 0o755))
	content := "# atcr-findings/v1\n" +
		"HIGH|app/calc.py:12|p|f|correctness|15|sol|greta\n" +
		"HIGH|app/calc.py:13|p|f|correctness|15|sol|overflow|greta\n"
	require.NoError(t, os.WriteFile(filepath.Join(pool, "findings.txt"), []byte(content), 0o600))

	_, categorical, _, _, err := readCaseFindingsLocated(dir, map[string]bool{"greta": true})
	require.NoError(t, err)

	rs := benchmark.ReviewerScore{Model: "m-greta", Persona: "greta",
		Cases: []benchmark.CaseScore{{Expected: []string{"correctness"}, Raised: categorical["greta"]}}}
	vocab := benchmark.PerReviewerVocabulary([]benchmark.ReviewerScore{rs})
	require.Len(t, vocab, 1)
	require.NotNil(t, vocab[0].Rate)
	assert.Equal(t, 2, vocab[0].Findings, "the skipped row counts in the out-of-vocabulary denominator")
	assert.Equal(t, 1, vocab[0].Drifted, "the skipped row's folded-in empty category counts as drift")
	assert.InDelta(t, 0.5, *vocab[0].Rate, 1e-9)
}

// AC3b's integration seam: the runner must wire the case's parsed line map into
// MatchFindings. The mini fixture's added head lines are 6-9 and its only
// outside_diff:true expectation settles at 12-13 (window [9,16]) — a stub
// citing app/calc.py:9 cites an ADDED line inside the window, which clause 3
// must reject: out-of-diff recall 0.0 with ExpectedOutsideDiff 1. This test
// FAILS if the line map is replaced with an empty one — the clause-3 lookup has
// nothing to reject and the citation earns credit.
func TestExecuteRepoStateBenchmarkRun_AddedLineDoesNotEarnOutsideDiffCredit(t *testing.T) {
	cc := &capturingLocatedCompleter{content: "HIGH|app/calc.py:9|average() divides by len(items) with no empty guard|add a guard|correctness|15|return total(items) / len(items)\n" +
		"MEDIUM|app/calc.py:8|safe_total is not None-safe as claimed|handle None explicitly|correctness|15|def safe_total(items):"}

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), cc, repoStateMiniPath, time.Unix(0, 0).UTC(), 0)
	require.NoError(t, err)

	require.Len(t, rr.PositionalRecall, 1)
	pr := rr.PositionalRecall[0]
	assert.Equal(t, 1, pr.ExpectedOutsideDiff)
	assert.Equal(t, 0, pr.MatchedOutsideDiff,
		"citing an ADDED line inside the outside_diff window must earn no out-of-diff credit (clause 3)")
	require.NotNil(t, pr.OutsideDiffRecall)
	assert.InDelta(t, 0.0, *pr.OutsideDiffRecall, 1e-9)
	// The in-diff half is unaffected: :8 is an added line and the expectation is
	// outside_diff:false, so it still matches.
	assert.Equal(t, 1, pr.MatchedWithinDiff)
}

// capturingLocatedCompleter emits caller-supplied content as the reviewer's
// findings, so a test can cite an exact line.
type capturingLocatedCompleter struct{ content string }

func (c capturingLocatedCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return c.content, nil
}

// A failed run must not destroy the paid artifacts: today the deferred
// RemoveAll wipes the work dir (every completed case's review tree) on the way
// out, and the error names no path — a transient failure on the last case
// forfeits the whole panel with zero recoverable evidence.
func TestExecuteRepoStateBenchmarkRun_RetainsTheWorkDirOnFailure(t *testing.T) {
	suite := writeTwoCaseSuite(t)
	// Faulted at a site that still ABORTS. This test used to fault materialization,
	// which is now a record-and-continue site — the run would return no error at all
	// and the assertions below would be testing nothing. An unwinnable expectation is
	// the nearest still-fatal neighbour: it fails mid-loop, after the work dir exists
	// and case 1 was already paid for, which is the shape this test is about.
	faultUnwinnableExpectation(t, suite, "second-case")

	_, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDirFromError(t, err)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "retained at", "the error names the retained work dir")
	start := strings.Index(err.Error(), "retained at ") + len("retained at ")
	path := err.Error()[start:]
	if end := strings.Index(path, ")"); end >= 0 {
		path = path[:end]
	}
	require.NotEmpty(t, path, "the error carries a usable path")
	_, statErr := os.Stat(path)
	assert.NoError(t, statErr, "the paid artifacts' work dir must survive a failed run")
}

func TestExecuteRepoStateBenchmarkRun_ReportsBothMetrics(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{}, repoStateMiniPath, gen, 0)
	require.NoError(t, err)

	assert.Equal(t, benchmark.FormatRepoStateV1, rr.Suite)
	assert.Equal(t, "1.0.0", rr.SuiteVersion)
	assert.Equal(t, "2026-09-16T12:00:00Z", rr.GeneratedAt, "GeneratedAt is injected, not time.Now")
	assert.Equal(t, []string{"mini-case"}, rr.SuiteCaseIDs)

	require.Len(t, rr.Reviewers, 1, "the category-recall metric is still produced")
	assert.Equal(t, "m-greta", rr.Reviewers[0].Model)
	assert.Equal(t, 1, rr.Reviewers[0].Runs)

	require.Len(t, rr.PositionalRecall, 1, "the new metric is produced alongside it")
	p := rr.PositionalRecall[0]
	assert.Equal(t, "m-greta", p.Model)
	assert.Equal(t, 2, p.ExpectedTotal)
	assert.Equal(t, 2, p.MatchedTotal)
	require.NotNil(t, p.OutsideDiffRecall)
	assert.InDelta(t, 1.0, *p.OutsideDiffRecall, 1e-9)
	require.NotNil(t, p.WithinDiffRecall)
	assert.InDelta(t, 1.0, *p.WithinDiffRecall, 1e-9)
}

// AC4, end to end: a reviewer that reads only the diff scores 1.0 within-diff and
// 0.0 outside it. A blended number would report this reviewer at 0.5 and hide the
// only thing the run measured.
func TestExecuteRepoStateBenchmarkRun_SeparatesTheTwoHalves(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubInDiffOnlyCompleter{}, repoStateMiniPath, gen, 0)
	require.NoError(t, err)

	require.Len(t, rr.PositionalRecall, 1)
	p := rr.PositionalRecall[0]
	require.NotNil(t, p.WithinDiffRecall)
	assert.InDelta(t, 1.0, *p.WithinDiffRecall, 1e-9, "the in-diff finding was caught")
	require.NotNil(t, p.OutsideDiffRecall)
	assert.InDelta(t, 0.0, *p.OutsideDiffRecall, 1e-9, "the out-of-diff finding was missed, and says so")
	require.NotNil(t, p.Recall)
	assert.InDelta(t, 0.5, *p.Recall, 1e-9)
}

// AC5 on the produced artifact, not just the type: a real repo-state run-result
// must not carry the new metric on any reviewers[] row.
func TestExecuteRepoStateBenchmarkRun_LeavesPublicRecordAlone(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	rr, _, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC(), 0)
	require.NoError(t, err)

	require.Len(t, rr.Reviewers, 1)
	assert.Equal(t, scorecard.RaisedDenominatorBenchmarkSuite, rr.Reviewers[0].RaisedDenominator,
		"a repo-state row still declares the benchmark denominator, not a production era")
	sub := benchmark.BuildSubmission(*rr, time.Unix(0, 0).UTC())
	assert.Equal(t, benchmark.SourceBenchmarkSuite, sub.Source)
}

// A suite path that is not repo-state-v1 must not reach this runner. Routing is
// the CLI's job; this is the backstop that keeps a mis-route loud.
func TestExecuteRepoStateBenchmarkRun_RefusesAStandardV1Suite(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	_, _, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, time.Unix(0, 0).UTC(), 0)
	require.Error(t, err)
	// A missing directory, a JSON syntax error, or a git failure would also
	// produce SOME error; the assertion must discriminate the MIS-ROUTE backstop
	// specifically — the repo-state loader rejecting a standard-v1 discriminator.
	assert.Contains(t, err.Error(), "declares suite",
		"the error must be the repo-state loader's discriminator rejection, not an unrelated failure")
	assert.Contains(t, err.Error(), `"fixture-mini"`,
		"the error names the discriminator the suite actually declares")
}

// The router is what makes `atcr benchmark run --suite-path <repo-state dir>` work
// at all, and what keeps an existing standard-v1 invocation on its own path.
func TestBenchmarkRunRouting_DetectsTheSuiteFormat(t *testing.T) {
	got, err := benchmark.DetectSuiteFormat(repoStateMiniPath)
	require.NoError(t, err)
	assert.Equal(t, benchmark.FormatRepoStateV1, got)

	got, err = benchmark.DetectSuiteFormat(suiteValidPath)
	require.NoError(t, err)
	assert.NotEqual(t, benchmark.FormatRepoStateV1, got)
}

// runBenchmarkRun silently switches between the two tier runners on the suite
// discriminator — an operator who passed --suite-path could not tell from stderr
// which path actually ran. This drives the real command through both arms with
// the config and completer seams swapped and asserts the routing line names the
// discriminator and the chosen runner in each direction.
func TestRunBenchmarkRun_LogsTheChosenTierRunner(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	restoreCfg := benchmarkLoadConfig
	restoreCompleter := benchmarkNewCompleter
	t.Cleanup(func() {
		benchmarkLoadConfig = restoreCfg
		benchmarkNewCompleter = restoreCompleter
	})
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }
	benchmarkNewCompleter = func(context.Context) fanout.Completer { return stubLocatedCompleter{} }

	// repo-state arm routes to executeRepoStateBenchmarkRun.
	_, _, stderr := execCmdSplit(t, "benchmark", "run", "--suite-path", repoStateMiniPath)
	assert.Contains(t, stderr, benchmark.FormatRepoStateV1,
		"stderr must name the suite discriminator the routing decision was made on")
	assert.Contains(t, stderr, "executeRepoStateBenchmarkRun",
		"stderr must name the chosen runner so the operator can tell which tier ran")

	// standard-v1 arm routes to executeBenchmarkRun.
	_, _, stderr = execCmdSplit(t, "benchmark", "run", "--suite-path", suiteValidPath)
	assert.Contains(t, stderr, "executeBenchmarkRun",
		"the standard-v1 arm must name its runner too — the silence is the defect")
}

// Moving discriminator reading ahead of Load (runBenchmarkRun, cli/benchmark.go)
// changed the operator-visible error for a suite.json whose suite name is absent
// or blank: it used to fail in Manifest.Validate with "suite name is required";
// it now fails in DetectSuiteFormat with "suite manifest <path> declares no suite
// name", and the old arm is unreachable through `benchmark run`. No test pinned
// either message on this path, so the change was invisible to the suite. This
// pins the new wording — and asserts the old one is gone — so the next reorder
// of these calls surfaces here instead of in front of an operator.
func TestRunBenchmarkRun_NoSuiteNameErrorIsPinned(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"),
		[]byte(`{"suite":"","suite_version":"1.0.0","cases":[]}`), 0o600))

	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	restoreCfg := benchmarkLoadConfig
	t.Cleanup(func() { benchmarkLoadConfig = restoreCfg })
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }

	// Drives the real cobra RunE like execCmdSplit, but keeps the returned error:
	// the root sets SilenceErrors, so the message reaches the operator through
	// main()'s print of the returned error, not through the command's stderr.
	var outBuf, errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs([]string{"benchmark", "run", "--suite-path", dir})
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	runErr := root.ExecuteContext(context.Background())
	require.Error(t, runErr, "a suite manifest with no suite name must fail run")
	combined := outBuf.String() + errBuf.String() + runErr.Error()
	assert.Contains(t, combined, "declares no suite name",
		"DetectSuiteFormat's message is the operator-visible one now that discriminator reading precedes Load")
	assert.NotContains(t, combined, "suite name is required",
		"the old Manifest.Validate wording is unreachable through benchmark run")
}

// --checkpoint is a standard-v1 feature. Silently ignoring it on a repo-state run
// would let an operator believe a long run was resumable when it was not, which is
// the worst moment to find out.
func TestRunBenchmarkRun_RejectsCheckpointForARepoStateSuite(t *testing.T) {
	err := checkRepoStateFlags(benchmark.FormatRepoStateV1, "some/checkpoint.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checkpoint")

	require.NoError(t, checkRepoStateFlags(benchmark.FormatRepoStateV1, ""),
		"no checkpoint requested is fine")
	require.NoError(t, checkRepoStateFlags("standard-v1", "some/checkpoint.json"),
		"standard-v1 checkpointing is untouched")
}

// Neither the new suite-format routing arm nor the --checkpoint rejection had a
// command-level test: the suite tested DetectSuiteFormat and checkRepoStateFlags
// directly, and the only test driving the real cobra RunE used the standard-v1
// arm — so deleting the routing branch (and the checkpoint refusal) left
// `go test ./cli/...` green while `benchmark run --suite-path <repo-state dir>`
// silently fell back to the standard loader and died. These drive the actual
// command through both arms, per the documented benchmarkLoadConfig /
// benchmarkNewCompleter seams.
func TestRunBenchmarkRun_RepoStateArmReachesStdout(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	restoreCfg := benchmarkLoadConfig
	restoreCompleter := benchmarkNewCompleter
	t.Cleanup(func() {
		benchmarkLoadConfig = restoreCfg
		benchmarkNewCompleter = restoreCompleter
	})
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }
	benchmarkNewCompleter = func(context.Context) fanout.Completer { return stubLocatedCompleter{} }

	// Deleting the repo-state routing branch makes this fall through to
	// executeBenchmarkRun, whose loader hard-rejects the discriminator — so a
	// silent mis-route fails here instead of in front of an operator.
	code, stdout, _ := execCmdSplit(t, "benchmark", "run", "--suite-path", repoStateMiniPath)
	require.Equal(t, 0, code, stdout)
	assert.Contains(t, stdout, "reviewer_positional_recall",
		"the cobra wiring must actually reach the repo-state runner — its headline metric must reach stdout")
	assert.NotContains(t, stdout, "outside the offered vocabulary",
		"sanity: a clean-vocabulary stub run emits no drift diagnostic on the repo-state arm")
}

// The command-level counterpart to TestWarnCaseFailures: that test proves the
// MESSAGE is right, this one proves the function is ever CALLED — deleting the
// warnCaseFailures call from runBenchmarkRun left the full cli suite green, and
// with it the entire operator-facing justification of the warning. It also pins
// the ordering contract: warnCaseFailures runs BEFORE warnPositionalRecallSummary
// because it qualifies the summary (a partial run's recall covers only the scored
// cases), so the UNMEASURED warning must appear EARLIER on the same stream.
func TestRunBenchmarkRun_WarnCaseFailuresFiresBeforeTheRecallSummary(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	restoreCfg := benchmarkLoadConfig
	restoreCompleter := benchmarkNewCompleter
	t.Cleanup(func() {
		benchmarkLoadConfig = restoreCfg
		benchmarkNewCompleter = restoreCompleter
	})
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }
	benchmarkNewCompleter = func(context.Context) fanout.Completer { return stubLocatedCompleter{} }

	suite := writeCaseSuite(t, "first-case", "second-case")
	faultMaterialization(t, suite, "second-case")

	code, stdout, stderr := execCmdSplit(t, "benchmark", "run", "--suite-path", suite)
	require.Equal(t, 0, code, stdout)
	assert.Contains(t, stderr, "UNMEASURED",
		"the unmeasured-case warning must reach the operator through the COMMAND, "+
			"not only through the direct unit test of the message")
	warnAt := strings.Index(stderr, "UNMEASURED")
	summaryAt := strings.Index(stderr, "repo-state positional recall:")
	require.GreaterOrEqual(t, summaryAt, 0, "the recall summary must also reach stderr; stderr was:\n%s", stderr)
	assert.Less(t, warnAt, summaryAt,
		"the warning must precede the recall summary it qualifies — a reader who sees "+
			"the number first has already taken it for a full-suite measurement")
	assert.Contains(t, stdout, "reviewer_positional_recall",
		"sanity: the run published a run-result at all")
}

func TestRunBenchmarkRun_RejectsCheckpointForARepoStateSuiteThroughTheCommand(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	restoreCfg := benchmarkLoadConfig
	t.Cleanup(func() { benchmarkLoadConfig = restoreCfg })
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }

	// Drives the real cobra RunE (the helper-level test only calls
	// checkRepoStateFlags): the refusal must fire at the command boundary,
	// before any loader runs.
	var outBuf, errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs([]string{"benchmark", "run", "--suite-path", repoStateMiniPath, "--checkpoint", "cp.json"})
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	runErr := root.ExecuteContext(context.Background())
	require.Error(t, runErr, "the command must refuse --checkpoint on a repo-state suite")
	combined := outBuf.String() + errBuf.String() + runErr.Error()
	assert.Contains(t, combined, "--checkpoint is not supported for a repo-state-v1 suite")
}

// The routing comparison was an exact case-sensitive string match, so a manifest
// declaring "Repo-State-V1" fell through to the standard-v1 arm, missed the
// known-other-format guard, and died on "diff path is required" — the exact
// misleading message the discriminator check exists to prevent (the repo-state
// manifest has no `diff` field at all). Case-insensitive ROUTING keeps that
// message unreachable: the cased discriminator now reaches the repo-state arm,
// where the loader's own exact tier check produces a precise, actionable error.
// (Full case-insensitivity — accepting the cased name at load — needs a change
// in internal/benchmark/repostate.go, outside this session's group scope, so the
// TD row stays open and this test pins the routing half only.)
func TestRunBenchmarkRun_CasedDiscriminatorRoutesToTheRepoStateArm(t *testing.T) {
	dir := t.TempDir()
	// os.CopyFS clones the whole case directory (base/, head files, case.json)
	// into the temp suite so only the manifest's discriminator spelling differs.
	require.NoError(t, os.CopyFS(dir, os.DirFS(repoStateMiniPath)))
	manifest, err := os.ReadFile(filepath.Join(repoStateMiniPath, "suite.json"))
	require.NoError(t, err)
	cased := strings.Replace(string(manifest), `"repo-state-v1"`, `"Repo-State-V1"`, 1)
	require.NotEqual(t, string(manifest), cased, "fixture rewrite must actually change the discriminator")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"), []byte(cased), 0o600))

	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	restoreCfg := benchmarkLoadConfig
	restoreCompleter := benchmarkNewCompleter
	t.Cleanup(func() {
		benchmarkLoadConfig = restoreCfg
		benchmarkNewCompleter = restoreCompleter
	})
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }
	benchmarkNewCompleter = func(context.Context) fanout.Completer { return stubLocatedCompleter{} }

	// Drives the real cobra RunE: the routing arm must pick the repo-state runner
	// for a cased discriminator (the stderr routing line names it), and the
	// standard loader's misleading message must stay out of the error path.
	_, _, stderr := execCmdSplit(t, "benchmark", "run", "--suite-path", dir)
	assert.Contains(t, stderr, "executeRepoStateBenchmarkRun",
		"a cased repo-state discriminator must route to the repo-state arm, not fall through to the standard loader")
	assert.NotContains(t, stderr, "diff path is required",
		"the misleading standard-v1 message must stay unreachable through benchmark run")
}

// The skipped-row reviewer recovery was two verbatim copies driving the SAME
// out-of-vocabulary denominator on two tiers — a fix to one silently diverged
// the other, and the divergence changes a published metric rather than crashing.
// One helper now serves both projections; this pins that they still agree.
func TestSkippedRowReviewer_BothReadersAttributeIdentically(t *testing.T) {
	dir := t.TempDir()
	pool := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(pool, 0o755))
	content := "# atcr-findings/v1\n" +
		"HIGH|app/calc.py:12|p|f|correctness|15|sol|greta\n" +
		"HIGH|app/calc.py:13|p|f|correctness|15|sol|overflow|greta\n"
	require.NoError(t, os.WriteFile(filepath.Join(pool, "findings.txt"), []byte(content), 0o600))

	categorical, err := readCaseFindings(dir)
	require.NoError(t, err)
	located, locatedCat, _, _, err := readCaseFindingsLocated(dir, map[string]bool{"greta": true})
	require.NoError(t, err)

	// The well-formed row parses for both readers; the over-column row is skipped
	// by the parser and folded back in with an empty category — under the SAME
	// recovered reviewer in BOTH projections. The category sequences must be
	// identical, or the two tiers' out-of-vocabulary denominators have diverged.
	assert.Equal(t, []string{"correctness", ""}, categorical["greta"])
	assert.Equal(t, categorical["greta"], locatedCat["greta"],
		"both projections attribute the skipped row to the same reviewer: the recovery is one helper, not two copies")
	assert.Len(t, located["greta"], 1, "the located projection carries only the well-formed row")
}

// A missing findings file is the one pool shape where "every reviewer wrote
// nothing" and "the review produced nothing" are indistinguishable from the
// returned maps alone. The flag surfaces the difference so the runner can warn.
func TestReadCaseFindingsLocated_FlagsAMissingFindingsFile(t *testing.T) {
	dir := t.TempDir()
	located, categorical, unattributed, missing, err := readCaseFindingsLocated(dir, map[string]bool{"greta": true})
	require.NoError(t, err)
	assert.True(t, missing, "no findings file was written: the flag must say so")
	assert.Empty(t, located)
	assert.Empty(t, categorical)
	assert.Zero(t, unattributed)

	pool := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(pool, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pool, "findings.txt"),
		[]byte("# atcr-findings/v1\nHIGH|app/calc.py:12|p|f|correctness|15|sol|greta\n"), 0o600))
	_, _, _, missing, err = readCaseFindingsLocated(dir, map[string]bool{"greta": true})
	require.NoError(t, err)
	assert.False(t, missing, "a present findings file is not missing")
}

// The refusal must name the ALTERNATIVE, not just say no. --checkpoint is reached
// for by an operator trying not to forfeit a long paid run, and that concern is
// already half-answered: a failed run retains its work dir and names the path. An
// operator told only "not supported" has no way to learn that, and the natural next
// move — re-running without the flag — reads as accepting the loss.
//
// Pinned because the actionable half is the part a future reword would drop first.
func TestCheckRepoStateFlags_RefusalNamesTheRetainedWorkDir(t *testing.T) {
	err := checkRepoStateFlags(benchmark.FormatRepoStateV1, "cp.json")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "--checkpoint is not supported for a repo-state-v1 suite",
		"the refusal itself is unchanged")
	assert.Contains(t, err.Error(), "RETAINS its work dir",
		"and it must point at the retention that already recovers the paid artifacts")

	// The refusal is scoped: standard-v1 with a checkpoint, and repo-state without
	// one, both stay legal. A guard that fired on either would be a regression the
	// assertion above cannot see.
	require.NoError(t, checkRepoStateFlags("standard-v1", "cp.json"))
	require.NoError(t, checkRepoStateFlags(benchmark.FormatRepoStateV1, ""))
}

// The published coverage row states whether the grounding gate was live.
//
// CorroborationRate's formula and denominator are identical on every suite and stay
// that way. Its INPUT is not: the categories it scores come from the merged
// findings.txt, written AFTER grounding. On repo-state the gate is on, so a reviewer
// that found the planted out-of-diff defect and labelled it correctly still scores 0
// for it; on standard-v1 the gate fails open and the same reviewer scores 1. Two
// rows then carry the same number about different populations, and
// corroboration_rate reaches the frozen scorecard.PublicRecord the public board
// shares with production leaderboard --export.
//
// Tagging the row is what makes that legible without forking a published metric.
func TestExecuteRepoStateBenchmarkRun_CoverageRowTagsTheGroundingGateAsLive(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Unix(0, 0).UTC()

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{}, repoStateMiniPath, gen, 0)
	require.NoError(t, err)
	require.NotEmpty(t, rr.Coverage)

	for _, row := range rr.Coverage {
		require.NotNil(t, row.GroundingEnabled,
			"a repo-state row must state the gate's state, not leave it to be inferred")
		assert.True(t, *row.GroundingEnabled,
			"the repo-state runner supplies a Range, so the gate is live for every case")
	}
}

// The tag rides the JSON, under the same key PoolSummary already uses.
func TestExecuteRepoStateBenchmarkRun_GroundingTagSerializes(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{}, repoStateMiniPath, time.Unix(0, 0).UTC(), 0)
	require.NoError(t, err)

	b, err := json.Marshal(rr)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"grounding_enabled":true`)
}

// The fold is AND with nil absorbing. The shipped fixture has ONE case, so the
// runner tests only ever exercise the opening branch -- these are the arms a
// multi-case suite reaches.
//
// Both failure directions point at "unmeasured" rather than at a claim: a row built
// from a mix of gated and ungated cases measured a mixed population, and a nil from
// any case (a rebuilt summary cannot know its run's gate state) makes the row nil.
// An overstated tag is worse than an absent one -- it is the exact overstatement the
// untagged row was already making.
func TestFoldGroundingEnabled(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		name          string
		prior, caseSt *bool
		first         bool
		want          *bool
	}{
		{name: "first case adopts its own state", caseSt: &on, first: true, want: &on},
		{name: "first case adopts a nil too", caseSt: nil, first: true, want: nil},
		{name: "gated AND gated stays gated", prior: &on, caseSt: &on, want: &on},
		{name: "ungated AND ungated stays ungated", prior: &off, caseSt: &off, want: &off},
		// A DISAGREEMENT is unmeasured, not ungated. The AND this replaced folded a
		// mixed population to &false, which a consumer cannot tell from "every case
		// this row scored was ungated" — and only the latter is comparable with a
		// standard-v1 row. Emitting a positive claim about a mixed population is the
		// same overstatement the nil arm below already refuses.
		{name: "gated then ungated is unmeasured, not ungated", prior: &on, caseSt: &off, want: nil},
		{name: "ungated then gated is unmeasured, not ungated", prior: &off, caseSt: &on, want: nil},
		{name: "a nil case absorbs a known prior", prior: &on, caseSt: nil, want: nil},
		{name: "a nil prior absorbs a known case", prior: nil, caseSt: &on, want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := foldGroundingEnabled(tc.prior, tc.caseSt, tc.first)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, *tc.want, *got)
		})
	}
}

// The stated invariant — "the accumulator must not end up sharing storage with a
// PoolSummary the caller still holds" — must hold for EVERY row length. The shipped
// fixture has ONE case, so the opening branch's `return caseState` aliased the
// caller's summary on exactly the row the runner tests exercised: later mutation of
// that summary through its own pointer would silently rewrite the folded row's
// gate state. Same latent-aliasing class publicCoverage fixed one level up.
func TestFoldGroundingEnabled_FirstCaseDoesNotAliasTheCallerSummary(t *testing.T) {
	on := true
	caseSt := &on
	got := foldGroundingEnabled(nil, caseSt, true)
	require.NotNil(t, got)
	require.NotSame(t, caseSt, got,
		"the first-case branch must hand back fresh storage, not the caller's pointer")
	*caseSt = false
	assert.True(t, *got,
		"mutating the caller's PoolSummary after the fold must not rewrite the accumulator")
}

// The two halves of this fix must agree: the ungrounded outcome can only arise
// where the gate ran, so a row that reports the gate as live is the only kind that
// may carry ungrounded in its tally. Pinned because the two were fixed separately
// and a future change to either could make the run-result self-contradictory --
// claiming a gate-driven outcome on a row that says the gate was off.
//
// The fixture and the completer are BOTH load-bearing, and an earlier version of
// this test got both wrong: it ran stubUntouchedFileCompleter against the mini
// fixture, whose reviewer keeps its in-patch calc.py finding and therefore scores
// `findings`. Every row then failed the `== 0` test and was skipped, so the loop
// body never executed and the test asserted nothing -- it stayed green with
// reviewerOutcome's ungrounded arm deleted outright. The tally assertion below is
// the guard against that recurring: it fails on a fixture that produces no
// ungrounded outcome, rather than passing vacuously over one.
func TestExecuteRepoStateBenchmarkRun_UngroundedOutcomeOnlyOnAGatedRow(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubOnlyUntouchedFileCompleter{}, writeUntouchedFileSuite(t), time.Unix(0, 0).UTC(), 0)
	require.NoError(t, err)
	require.NotEmpty(t, rr.Coverage)

	ungrounded := 0
	for _, row := range rr.Coverage {
		ungrounded += row.Outcomes[benchmark.OutcomeUngrounded]
	}
	require.Positive(t, ungrounded,
		"the fixture must actually produce an ungrounded outcome, or the per-row checks below assert nothing")

	for _, row := range rr.Coverage {
		if row.Outcomes[benchmark.OutcomeUngrounded] == 0 {
			continue
		}
		require.NotNil(t, row.GroundingEnabled,
			"a row tallying ungrounded must state the gate state that produced it")
		assert.True(t, *row.GroundingEnabled,
			"ungrounded is unreachable with the gate off; the row contradicts itself")
	}
}

// AC1 — a mid-suite infrastructure failure must produce a PARTIAL run-result, not
// nothing. Before this, the first failing case aborted the whole run: a four-case
// suite failing on case four forfeited three cases of paid panel output, and
// --checkpoint is refused for this tier so there was no resume to fall back on.
//
// The failed case is recorded in the failure channel and scored NOWHERE. Recording
// it as a zero-scored row instead would increment every reviewer's ExpectedTotal
// without giving them a chance at the case — scoring an infrastructure failure as a
// genuine missed defect, which docs/benchmark.md forbids for this tier.
func TestExecuteRepoStateBenchmarkRun_MidSuiteFailureYieldsAPartialRunResult(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case", "third-case")
	faultMaterialization(t, suite, "second-case")

	rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)
	require.NoError(t, err, "a single case's infrastructure failure must not forfeit the cases that succeeded")
	require.NotNil(t, rr)

	require.Len(t, rr.CaseFailures, 1)
	assert.Equal(t, "second-case", rr.CaseFailures[0].CaseID)
	assert.Equal(t, benchmark.CaseFailureMaterialize, rr.CaseFailures[0].Reason)

	assert.Equal(t, []string{"first-case", "second-case", "third-case"}, rr.SuiteCaseIDs,
		"the suite denominator names every case; dropping the failed one would hide the shortfall instead of showing it")

	require.Len(t, rr.Coverage, 1)
	assert.Equal(t, []string{"first-case", "third-case"}, rr.Coverage[0].CaseIDs,
		"the failed case is unmeasured, so it is absent from the covered set")
	require.Len(t, rr.Reviewers, 1)
	assert.Equal(t, 2, rr.Reviewers[0].Runs, "runs counts scored cases only")
}

// AC2 — the failed case contributes nothing to either recall denominator. The
// reviewer scored both surviving cases perfectly, so recall over a 3-case suite
// with 1 failed case must read exactly as recall over the 2 scored ones: 1.0, not
// 2/3. A zero-scored row for the failed case would produce the latter.
func TestExecuteRepoStateBenchmarkRun_FailedCaseLeavesTheDenominatorsAlone(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case", "third-case")
	faultMaterialization(t, suite, "second-case")
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})

	partial, retained, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)
	require.NoError(t, err)

	// The reference run: the same two cases, with no failed case present at all.
	clean, cleanRetained, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		writeCaseSuite(t, "first-case", "third-case"), time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, cleanRetained)
	require.NoError(t, err)

	require.Len(t, partial.PositionalRecall, 1)
	require.Len(t, clean.PositionalRecall, 1)
	assert.Equal(t, clean.PositionalRecall[0].ExpectedTotal, partial.PositionalRecall[0].ExpectedTotal,
		"an unmeasured case adds 0 to expected_total")
	require.NotNil(t, partial.PositionalRecall[0].Recall)
	require.NotNil(t, clean.PositionalRecall[0].Recall)
	assert.Equal(t, *clean.PositionalRecall[0].Recall, *partial.PositionalRecall[0].Recall,
		"recall over 3 cases with 1 failed must equal recall over the 2 scored cases")

	// The differential above is blind to the NUMERATOR: both sides run the same
	// function, so a uniform miscount moves them together and the equality still
	// holds. These pin the answer this test's own comment states — 1.0 over the 2
	// scored cases, not 2/3 — so a runner that matched nothing would fail here
	// instead of passing with recall 0 on both sides.
	assert.Equal(t, 1.0, *partial.PositionalRecall[0].Recall,
		"the reviewer scored both surviving cases perfectly")
	assert.Equal(t, 4, partial.PositionalRecall[0].ExpectedTotal,
		"expected_total counts the 2 scored cases' expectations only")

	require.Len(t, partial.Reviewers, 1)
	require.Len(t, clean.Reviewers, 1)
	assert.Equal(t, clean.Reviewers[0].CorroborationRate, partial.Reviewers[0].CorroborationRate,
		"the category-recall denominator excludes the failed case too")
}

// The run-level tests covered exactly two shapes: ONE middle case fails, and ALL
// cases fail. This is the two that were missing, in one suite — several cases fail,
// and the LAST one is among them.
//
// Multi-failure matters because nothing checked the failure channel's ORDER or
// uniqueness beyond a single entry, and warnCaseFailures' "N of M" scale line was
// never exercised for N greater than 1. A trailing failure matters because the
// post-loop code — including the cases-scored arithmetic — only runs when the final
// iteration is not the one that succeeded.
func TestExecuteRepoStateBenchmarkRun_RecordsEveryFailureInSuiteOrderIncludingTheLast(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case", "third-case", "fourth-case")
	faultMaterialization(t, suite, "second-case")
	faultMaterialization(t, suite, "fourth-case")

	rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)
	require.NoError(t, err, "two failures including the last case still leave two scored cases")
	require.NotNil(t, rr)

	require.Len(t, rr.CaseFailures, 2, "each failed case is recorded once, not once per remaining case")
	assert.Equal(t, []string{"second-case", "fourth-case"},
		[]string{rr.CaseFailures[0].CaseID, rr.CaseFailures[1].CaseID},
		"the channel is read as a list beside the suite, so it carries suite order")

	require.Len(t, rr.Coverage, 1)
	assert.Equal(t, []string{"first-case", "third-case"}, rr.Coverage[0].CaseIDs,
		"the covered set is the survivors, in suite order")
	require.Len(t, rr.Reviewers, 1)
	assert.Equal(t, 2, rr.Reviewers[0].Runs, "runs counts scored cases only")

	require.NotEmpty(t, retained, "a partial run retains its work dir")
	assert.DirExists(t, filepath.Join(retained, "review-0"),
		"the scored cases' paid review artifacts are what retention protects")

	var warn bytes.Buffer
	warnCaseFailures(&warn, rr, retained)
	assert.Contains(t, warn.String(), "2 of 4", "the scale line has to be right for more than one failure")
}

// AC5 — the shipped all-agents-failed abort is the one ExecuteReview failure the
// continue path must NOT swallow. A total-roster failure is exactly the transient
// infrastructure failure docs/benchmark.md:265 forbids scoring as a genuine missed
// defect, and recording it as an unmeasured case would publish a run whose missing
// case looks identical to a disk fault.
func TestExecuteRepoStateBenchmarkRun_TotalRosterFailureStillAborts(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), failingCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDirFromError(t, err)

	require.Error(t, err, "a total-roster failure aborts; it is not recorded as an unmeasured case")
	assert.ErrorIs(t, err, fanout.ErrAllAgentsFailed)
	assert.Nil(t, rr, "no partial run-result is produced for a total-roster failure")
}

// AC6 — ValidateAgainstHead is a suite-AUTHORING defect, not transient bad luck: it
// is deterministic, a re-run cannot fix it, and it fires before the case's first
// paid completer call, so aborting forfeits nothing. Continuing would score around a
// suite known to be broken and publish the number as if it measured something.
func TestExecuteRepoStateBenchmarkRun_UnwinnableCaseStillAborts(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")
	faultUnwinnableExpectation(t, suite, "second-case")

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDirFromError(t, err)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unwinnable", "the diagnostic still names the authoring defect")
	assert.Nil(t, rr, "an unwinnable case is not recorded into the failure channel")
}

// A suite in which EVERY case failed measured nothing, so there is no run-result
// worth returning. Left to build one, it would carry suite_case_ids with zero
// reviewer rows — which checkCoverage calls "malformed", the wrong diagnosis for a
// runner that behaved exactly as designed. The shipped all-agents-failed abort does
// not cover this: it fires inside ONE case's review call and cannot see a
// suite-wide zero-scored outcome.
func TestExecuteRepoStateBenchmarkRun_EveryCaseFailingIsAnError(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")
	faultMaterialization(t, suite, "first-case")
	faultMaterialization(t, suite, "second-case")

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDirFromError(t, err)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no case could be scored")
	assert.Nil(t, rr)
}

// The zero-scored guard used to carry TWO returns: an all-cases-failed message and
// a fallback for a shape no test can reach (an empty accumulator with no recorded
// failure — the empty case list is rejected at load, and an all-roster failure
// aborts per case). The untested fallback is folded into the one return, so the
// single message carries the failure tally and stays legible at zero failures too —
// the file no longer carries a branch nothing can execute.
func TestExecuteRepoStateBenchmarkRun_ZeroScoredErrorCarriesTheTally(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")
	faultMaterialization(t, suite, "first-case")
	faultMaterialization(t, suite, "second-case")

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDirFromError(t, err)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no case could be scored")
	assert.Contains(t, err.Error(), "2 of 2 case(s) failed",
		"the folded message reports the tally, not a separate all-failed phrasing")
	assert.Nil(t, rr)
}

// publicSlotFailures silently dropped a slot failure whose key was absent from
// scrubOf — a silent drop inside the channel built to end silent drops. The shape is
// unreachable through the runner (every key in slotFailures is registered into order
// before the skip fires, and scrubOf is built from order), but an unreachable case
// must not quietly become a silent one: the skip warns, naming the key and the
// failures it is dropping, while still never emitting an untranslated identity.
func TestPublicSlotFailuresWarnsOnAnUntranslatableKey(t *testing.T) {
	var logs bytes.Buffer
	key := reviewerKey{model: "m-greta", persona: "greta"}
	out := publicSlotFailures(
		map[reviewerKey][]benchmark.SlotFailure{
			key: {{CaseID: "case-01", Reason: benchmark.SlotFailureCall}},
		},
		[]reviewerKey{key},
		map[reviewerKey]reviewerKey{}) // scrubOf deliberately lacks the key

	assert.Nil(t, out, "an untranslated identity is never emitted raw")
	assert.Contains(t, logs.String(), "slot failure dropped",
		"the skip must warn rather than discard silently")
}

// The all-cases-failed diagnostic used to name ONE reason — whichever happened to be
// last. On a mixed systemic failure that is an arbitrary pick out of N, and the same
// sentence then tells the operator a re-run helps "only if the cause was transient"
// without saying which causes there were. The full list is already in hand.
//
// Tested on the tally directly rather than end to end. A run in which EVERY case
// fails makes no completer call at all — every case dies before execute — so the
// fixtures have no in-loop hook to plant a SECOND fault kind with, and the only
// hook-free fault (a diff that does not apply) yields one reason for every case.
// The summarizer is where the arbitrary pick lived, so it is where the fix is pinned.
func TestSummarizeCaseFailureReasons(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures []benchmark.CaseFailure
		want     string
	}{
		{
			name: "mixed reasons are all named, sorted for determinism",
			failures: []benchmark.CaseFailure{
				{CaseID: "c1", Reason: benchmark.CaseFailurePoolSummary},
				{CaseID: "c2", Reason: benchmark.CaseFailureExecute},
				{CaseID: "c3", Reason: benchmark.CaseFailureExecute},
			},
			want: "execute x2, pool_summary x1",
		},
		{
			name:     "a single reason still reads as a tally",
			failures: []benchmark.CaseFailure{{CaseID: "c1", Reason: benchmark.CaseFailureMaterialize}},
			want:     "materialize x1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, summarizeCaseFailureReasons(tc.failures))
		})
	}
}

// The tally reaches the operator, not just the helper's unit test.
func TestExecuteRepoStateBenchmarkRun_EveryCaseFailingNamesTheReasonTally(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")
	faultMaterialization(t, suite, "first-case")
	faultMaterialization(t, suite, "second-case")

	_, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDirFromError(t, err)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "materialize x2",
		"the operator is told how many cases died of what, not just the last one")
}

// AC4 — retention on a HARD failure already shipped; this is the other half. A
// PARTIAL run returns err == nil, so the deferred cleanup fired and destroyed the
// paid review artifacts of every case that DID succeed — the precise artifacts the
// continue path exists to preserve.
func TestExecuteRepoStateBenchmarkRun_RetainsTheWorkDirOnAPartialRun(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case", "third-case")
	faultMaterialization(t, suite, "second-case")
	var logs bytes.Buffer

	rr, retained, err := executeRepoStateBenchmarkRun(logCapturingContext(t, &logs),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)
	require.NoError(t, err)
	require.NotEmpty(t, rr.CaseFailures)

	path := releaseRetainedWorkDir(t, retainedWorkDirFromLog(t, logs.String()))
	_, statErr := os.Stat(path)
	assert.NoError(t, statErr, "a partial run's paid artifacts must survive for inspection or manual rescoring")
}

// The retained path must survive a TMPDIR containing a space. log/slog's text
// handler QUOTES any attribute value containing a space, so the naive "cut at the
// first space" parse of the retention line returns a truncated quote-prefixed
// string and the os.Stat below fails for a reason unrelated to retention — the
// exact failure the helper exists to prevent.
func TestExecuteRepoStateBenchmarkRun_RetainsWorkDirWithSpaceInTMPDIR(t *testing.T) {
	spaced := filepath.Join(t.TempDir(), "has space")
	require.NoError(t, os.Mkdir(spaced, 0o755))
	t.Setenv("TMPDIR", spaced)

	suite := writeCaseSuite(t, "first-case", "second-case")
	faultMaterialization(t, suite, "second-case")
	var logs bytes.Buffer

	_, retained, err := executeRepoStateBenchmarkRun(logCapturingContext(t, &logs),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)
	require.NoError(t, err)

	path := retainedWorkDirFromLog(t, logs.String())
	assert.True(t, strings.HasPrefix(path, spaced),
		"the parsed path %q must live under the spaced TMPDIR %q — truncation at the first space reads a quoted slog value as bare text", path, spaced)
	_, statErr := os.Stat(path)
	require.NoError(t, statErr, "the reported path must exist exactly as logged; logs were:\n%s", logs.String())
	_, err = os.Stat(filepath.Join(path, "review-0"))
	assert.NoError(t, err, "the scored case's paid review artifacts survive under the spaced path")
}

// workDirNamingCompleter records the run's work dir path from INSIDE the loop. A
// clean run logs no path and returns none — it has nothing to retain — so a completer
// call is the only moment the directory can be named while it still exists.
type workDirNamingCompleter struct {
	since time.Time
	tmp   string
}

func (c *workDirNamingCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "atcr-repo-state-*"))
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.ModTime().After(c.since) {
			c.tmp = m
		}
	}
	return stubLocatedCompleter{}.Complete(ctx, inv)
}

// A walk that cannot even read the ROOT used to log retained_bytes=0 for a
// directory that IS retained — docs/benchmark.md tells the operator to watch exactly
// that number for growth before the volume fills, and a zero reads as "nothing
// retained". dirSizeBytes therefore reports whether the size was measured at all, so
// the caller can log "unknown" instead of a lying zero. A mid-walk failure still
// returns the partial total as a signal, and the deferred cleanup stays
// warn-never-fail either way.
func TestDirSizeBytesReportsUnmeasuredRootWalk(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.bin"), make([]byte, 128), 0o600))

	size, measured := dirSizeBytes(dir)
	require.True(t, measured, "a readable root measures fine")
	assert.Equal(t, int64(128), size)

	size, measured = dirSizeBytes(filepath.Join(dir, "missing"))
	assert.False(t, measured, "a walk that cannot read the root measures nothing — the caller must log unknown, not zero")
	assert.Equal(t, int64(0), size)
}

// Retention on a partial run is unbounded and unconditional ON PURPOSE — the
// artifacts are the only copy of a paid panel, so a byte cap or a keep-only-the-failed-
// case policy would destroy exactly what the arm exists to save. That makes growth
// something the operator has to SEE, so the retention line reports the size beside the
// path. A scheduled suite losing one case per run otherwise fills the volume silently.
func TestExecuteRepoStateBenchmarkRun_PartialRunReportsTheRetainedSize(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")
	faultMaterialization(t, suite, "second-case")
	var logs bytes.Buffer

	rr, retained, err := executeRepoStateBenchmarkRun(logCapturingContext(t, &logs),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)
	require.NoError(t, err)
	require.NotEmpty(t, rr.CaseFailures)

	assert.Contains(t, logs.String(), "retained_bytes=",
		"the retention line reports how much is being kept, not only where")
	assert.NotContains(t, logs.String(), "retained_bytes=0",
		"a retained run holds real paid artifacts, so the measured size is non-zero")
}

// A run with no failures at all still cleans up: retention is the exception the
// failure channel earns, not the new default.
//
// Asserted on the FILESYSTEM, not on the absence of a log line. The old assertion —
// NotContains(logs, "work dir retained") — is equally true when the cleanup is deleted
// outright, so a regression that stopped reclaiming the dir on every clean run passed
// the one test named for catching it.
func TestExecuteRepoStateBenchmarkRun_CleanRunStillCleansUp(t *testing.T) {
	var logs bytes.Buffer
	cc := &workDirNamingCompleter{since: time.Now()}

	rr, retained, err := executeRepoStateBenchmarkRun(logCapturingContext(t, &logs),
		benchCfg([3]string{"greta", "m-greta", "greta"}), cc, repoStateMiniPath, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)
	require.NoError(t, err)
	assert.Empty(t, rr.CaseFailures)
	assert.NotContains(t, logs.String(), "work dir retained", "a clean run has nothing to retain")

	require.NotEmpty(t, cc.tmp, "the fixture must actually have observed the run's work dir")
	_, statErr := os.Stat(cc.tmp)
	assert.True(t, os.IsNotExist(statErr),
		"a clean run reclaims the work dir it created; %s still exists", cc.tmp)
}

// releaseRetainedWorkDir registers the removal of a retained work dir the MOMENT its
// path is known, before any assertion that could fail and skip the registration.
//
// Every abort path in this file retains the work dir, and retention is the whole point
// — so a test that registers cleanup after its assertions leaks a full review tree on
// exactly the runs where an assertion fires. Measured before this helper: seven
// atcr-repo-state-* dirs per run of the abort-path subset, accumulating in $TMPDIR
// forever.
func releaseRetainedWorkDir(t *testing.T, path string) string {
	t.Helper()
	if path != "" {
		t.Cleanup(func() { _ = os.RemoveAll(path) })
	}
	return path
}

// releaseRetainedWorkDirFromError is the same registration for the HARD-failure arm,
// which reports the path by wrapping it into the returned error rather than by
// returning it. Best-effort by design: a run that did not retain anything carries no
// token, and that is not a test failure.
func releaseRetainedWorkDirFromError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	const token = "work dir retained at "
	i := strings.Index(err.Error(), token)
	if i < 0 {
		return
	}
	path := err.Error()[i+len(token):]
	// Terminate at the closing paren, not at the first space: the wrap site
	// (benchmark_repostate.go:171) always appends "path)" with the path UNQUOTED —
	// it is fmt.Errorf, not the slog text handler — so on a host whose $TMPDIR
	// contains a space, cutting at a space truncated the path and the cleanup
	// silently leaked the retained dir.
	if end := strings.Index(path, ")"); end >= 0 {
		path = path[:end]
	}
	releaseRetainedWorkDir(t, path)
}

// logCapturingContext wires a text logger into the context so a test can assert on
// what the runner REPORTED, not just on what it left on disk. The retained path is
// only useful to an operator if it reaches them.
func logCapturingContext(t *testing.T, into *bytes.Buffer) context.Context {
	t.Helper()
	logger, err := log.New("info", "text", into)
	require.NoError(t, err)
	return log.NewContext(context.Background(), logger)
}

// retainedWorkDirFromLog pulls the retained path out of the runner's own log line,
// which is the only channel a partial run has to report it: unlike a hard failure,
// it returns no error to carry the path in.
//
// The anchor is the FULL partial-run message, not the substring "work dir
// retained": the failed-run arm (benchmark_repostate.go:170) logs a message that
// contains that same substring, so a test producing both lines would silently read
// the wrong one. The path value is read QUOTE-AWARE: slog's text handler quotes any
// value containing a space, so on a host whose $TMPDIR has a space the naive
// cut-at-first-space parse returned a truncated quote-prefixed string.
func retainedWorkDirFromLog(t *testing.T, logs string) string {
	t.Helper()
	const partialAnchor = "benchmark work dir retained after a partial run"
	i := strings.Index(logs, partialAnchor)
	require.GreaterOrEqual(t, i, 0, "the run must report a retained work dir; logs were:\n%s", logs)
	j := strings.Index(logs[i:], "path=")
	require.GreaterOrEqual(t, j, 0, "the retention line must name the path; logs were:\n%s", logs)
	path := logs[i+j+len("path="):]
	if strings.HasPrefix(path, "\"") {
		// Quoted by the text handler: the value ends at the closing quote, not at
		// the first space, and strconv.Unquote resolves any escapes inside it.
		end := strings.Index(path[1:], "\"")
		require.GreaterOrEqual(t, end, 0, "the quoted path value must terminate; logs were:\n%s", logs)
		unquoted, err := strconv.Unquote(path[:end+2])
		require.NoError(t, err, "the quoted path value must unquote cleanly; logs were:\n%s", logs)
		return unquoted
	}
	if end := strings.IndexAny(path, " \n"); end >= 0 {
		path = path[:end]
	}
	require.NotEmpty(t, path, "the reported path is usable")
	return path
}

// An operator INTERRUPT is not an infrastructure failure. cli/main.go cancels the
// root context on SIGINT/SIGTERM, and MaterializeCase and PrepareReview both run
// under it — so with a record-and-continue path in place, Ctrl-C would be recorded
// as a per-case "materialize" fault, the loop would burn through every remaining
// case recording the same thing, and the command would write a partial run-result
// and exit 0. An interrupted run must never become a publishable artifact.
func TestExecuteRepoStateBenchmarkRun_CancellationAbortsRatherThanRecording(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rr, _, err := executeRepoStateBenchmarkRun(ctx,
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{},
		writeCaseSuite(t, "first-case", "second-case"), time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDirFromError(t, err)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled, "cancellation propagates rather than being classified")
	assert.Nil(t, rr, "an interrupted run produces no run-result to publish")
}

// The same rule when the interrupt lands on the LAST case: the loop has no further
// iteration to catch it, so without a check after the loop the run would return a
// partial result whose missing case was the operator's own Ctrl-C.
//
// cancelAfter=2 makes the cancel land DURING case 2's review of a 2-case suite, so
// no loop iteration follows the interrupt and only the post-loop guard can catch it
// — the arm this test is named for. (The completer's default, cancel on the first
// call, is caught by the IN-LOOP check at the next iteration, which leaves the
// post-loop guard and its scored-count arithmetic dead code as far as this suite is
// concerned: deleting the guard outright left the cli package green.)
func TestExecuteRepoStateBenchmarkRun_CancellationOnTheFinalCaseStillAborts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cc := &cancellingCompleter{cancel: cancel, cancelAfter: 2}

	rr, _, err := executeRepoStateBenchmarkRun(ctx,
		benchCfg([3]string{"greta", "m-greta", "greta"}), cc,
		writeCaseSuite(t, "first-case", "second-case"), time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDirFromError(t, err)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, rr)
	assert.Equal(t, 2, cc.calls, "the interrupt must land on the LAST case's review, not an earlier one")
}

// Both cancellation diagnostics print the SAME sentence, so they must print it from
// the same quantity. The in-loop site used the loop index — cases ATTEMPTED, failures
// included — while the post-loop site used cases scored, so on a run that lost a case
// to infrastructure the two disagreed for one interrupt and an operator reading one
// line could not tell which number they had. Scored cases is the useful one: it is
// what the run would have published.
func TestExecuteRepoStateBenchmarkRun_CancellationCountsScoredCasesNotAttempted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	suite := writeCaseSuite(t, "first-case", "second-case", "third-case")
	faultMaterialization(t, suite, "first-case")

	// Case 1 fails at materialization and never reaches a completer, so the single
	// call below lands on case 2 — cancelling after case 2 has been scored and before
	// case 3 begins. Attempted is 2; scored is 1.
	rr, _, err := executeRepoStateBenchmarkRun(ctx,
		benchCfg([3]string{"greta", "m-greta", "greta"}), &cancellingCompleter{cancel: cancel},
		suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDirFromError(t, err)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, rr)
	assert.Contains(t, err.Error(), "cancelled after 1 of 3 case(s)",
		"the count is cases SCORED; counting the failed case as progress overstates what the run measured")
}

// cancellingCompleter serves the first case normally, then cancels the run's
// context — standing in for a SIGINT that arrives mid-suite. cancelAfter picks
// WHICH call cancels: 1 (the default, for the mid-suite arms) or, on the final-case
// arm, the last case's call, which no in-loop check can catch.
type cancellingCompleter struct {
	cancel      context.CancelFunc
	calls       int
	cancelAfter int
}

func (c *cancellingCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	c.calls++
	out, err := stubLocatedCompleter{}.Complete(ctx, inv)
	if c.calls == max(c.cancelAfter, 1) {
		c.cancel()
	}
	return out, err
}

// An EMPTY ROSTER is a configuration defect, not bad luck: no slot ran, and no
// re-run fixes it until the config changes. Recorded as a transient "execute"
// failure it would repeat on every case and surface as "all cases failed", hiding
// the real cause behind the class this code deliberately keeps fatal for a
// suite-authoring defect.
//
// An empty roster reaches the runner at PREPARE, not at execute: ErrEmptyRoster is
// raised by validateReviewRequest inside fanout.PrepareReview, and
// validatePublishableReviewerRoster returns nil on an empty list rather than
// rejecting it. So the prepare branch carries the same sentinel split the execute
// branch does, and this test pins BOTH halves — the sentinel survives for a caller
// to classify, and the run does not misreport itself as a suite of transient
// per-case failures.
func TestExecuteRepoStateBenchmarkRun_EmptyRosterAborts(t *testing.T) {
	cfg := benchCfg()

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		writeCaseSuite(t, "first-case", "second-case"), time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDirFromError(t, err)

	require.Error(t, err)
	assert.Nil(t, rr, "an empty roster is a configuration defect, not an unmeasured case")
	assert.ErrorIs(t, err, fanout.ErrEmptyRoster,
		"the sentinel must survive so a caller can tell a config defect from a transient fault")
	assert.NotContains(t, err.Error(), "all 2 case(s) failed",
		"recorded per-case it repeats on every case and buries the real cause under the transient class")
}

// outside_diff_recall printed bare is the tier's headline number stripped of the one
// caveat that decides how to read it. internal/benchmark/benchmark.go, score_repostate.go
// and docs/benchmark.md each spell out at length that a zero conflates "never
// consulted unchanged code" with "found it and the grounding gate discarded it" — and
// the single operator-facing surface for the number carried none of it, which is
// where an operator actually reads it.
//
// The caveat rides the SUMMARY, not each row: it is a property of how the metric is
// computed, identical for every reviewer, so repeating it per row would bury the
// numbers it qualifies.
func TestWarnPositionalRecallSummary_CarriesTheGroundingCaveat(t *testing.T) {
	var buf bytes.Buffer
	zero, one := 0.0, 1.0

	warnPositionalRecallSummary(&buf, &benchmark.RunResult{
		PositionalRecall: []benchmark.ReviewerPositionalRecall{{
			Model: "m-greta", Persona: "greta",
			Recall: &one, MatchedTotal: 4, ExpectedTotal: 4,
			OutsideDiffRecall: &zero, MatchedOutsideDiff: 0, ExpectedOutsideDiff: 4,
		}},
	})

	out := buf.String()
	require.Contains(t, out, "outside_diff_recall 0.00", "the number itself must still be printed")
	assert.Contains(t, out, "never consulted unchanged code",
		"a zero must not read as reviewer inattention when it may be gate attrition")
	assert.Contains(t, out, "grounding gate discarded",
		"the other half of the conflation must be named too, or the caveat explains nothing")
	assert.Equal(t, 1, strings.Count(out, "never consulted unchanged code"),
		"the caveat qualifies the metric, not each row — repeating it per reviewer buries the numbers")
}

// A partial run prints recall numbers that read exactly like a full-suite
// measurement. The failure channel is in the run-result, but an operator watching
// the terminal sees only the recall summary — so the one number this tier exists to
// produce would be read as covering the whole suite. Reported beside that summary
// on stderr, independent of the context logger's level, for the same reason
// warnPositionalRecallSummary exists at all.
func TestWarnCaseFailures(t *testing.T) {
	var buf bytes.Buffer

	warnCaseFailures(&buf, &benchmark.RunResult{
		SuiteCaseIDs: []string{"case-01", "case-02", "case-03"},
		CaseFailures: []benchmark.CaseFailure{
			{CaseID: "case-02", Reason: benchmark.CaseFailurePrepare},
		},
	}, "")

	out := buf.String()
	assert.Contains(t, out, "case-02")
	assert.Contains(t, out, benchmark.CaseFailurePrepare, "the stage the case died at is the actionable part")
	assert.Contains(t, out, "1 of 3", "the operator needs the scale of the shortfall, not just the list")
	assert.Contains(t, out, "UNMEASURED", "a failed case must not read as a scored zero")
	assert.Contains(t, out, "excluded from every recall denominator",
		"the message must say what unmeasured DOES to the numbers printed beside it")
	assert.Contains(t, out, "retained", "the paid artifacts survive, and the operator has to know that to use them")
}

// The multi-failure shape, which the single-failure case above cannot pin: the
// scale line must count the FAILURES (2), not the suite, every failed case gets its
// own "failed at <stage>" line in the order recorded, and both stages are named —
// an operator triaging a partial run needs to know TWO different stages died, not
// just that something did.
func TestWarnCaseFailures_MultipleFailures(t *testing.T) {
	var buf bytes.Buffer

	warnCaseFailures(&buf, &benchmark.RunResult{
		SuiteCaseIDs: []string{"case-01", "case-02", "case-03"},
		CaseFailures: []benchmark.CaseFailure{
			{CaseID: "case-02", Reason: benchmark.CaseFailureMaterialize},
			{CaseID: "case-03", Reason: benchmark.CaseFailureExecute},
		},
	}, "")

	out := buf.String()
	assert.Contains(t, out, "2 of 3", "the scale line counts the failures against the suite")
	assert.Contains(t, out, "case-02: failed at materialize")
	assert.Contains(t, out, "case-03: failed at execute")
	assert.Less(t, strings.Index(out, "case-02"), strings.Index(out, "case-03"),
		"failures are listed in the order the run recorded them")
}

// workDirFaultingCompleter makes the run's own work dir unwritable once case 1's
// panel has been paid for, so case 2's os.MkdirAll(repo-1) fails MID-LOOP. It is the
// only observation point a test has for that site: the work dir is created inside the
// runner by os.MkdirTemp, so it has no name until the run is under way, and a
// completer call is the one moment a test executes while the loop is running.
//
// EACCES on purpose, not ENOSPC: a permission fault is path-specific, which is
// exactly the class that KEEPS the record-and-continue treatment (isFatalWorkDirError
// deliberately excludes it). Faulting with a host-level errno would exercise the
// abort arm instead, which is a different test.
type workDirFaultingCompleter struct {
	since   time.Time
	tmp     string
	calls   int
	perCase int
}

func (c *workDirFaultingCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	c.calls++
	if c.calls == c.perCase {
		matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "atcr-repo-state-*"))
		for _, m := range matches {
			if fi, err := os.Stat(m); err == nil && fi.ModTime().After(c.since) {
				c.tmp = m
				_ = os.Chmod(m, 0o555)
			}
		}
	}
	return stubLocatedCompleter{}.Complete(ctx, inv)
}

// prepareFaultingCompleter plants a regular FILE where the NEXT case's review dir
// must be created, so that case dies inside fanout.PrepareReview. Planted from a
// completer call because the work dir is named by os.MkdirTemp inside the runner and
// has no name until the run is under way.
type prepareFaultingCompleter struct {
	since     time.Time
	caseIndex int
	calls     int
	planted   string
}

func (c *prepareFaultingCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	c.calls++
	if c.calls == c.caseIndex {
		matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "atcr-repo-state-*"))
		for _, m := range matches {
			if fi, err := os.Stat(m); err == nil && fi.ModTime().After(c.since) {
				blocker := filepath.Join(m, fmt.Sprintf("review-%d", c.caseIndex))
				if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err == nil {
					c.planted = blocker
				}
			}
		}
	}
	return stubLocatedCompleter{}.Complete(ctx, inv)
}

// The prepare site was driven only INCIDENTALLY, by the empty-roster test — which now
// aborts rather than recording, so nothing reached the record-and-continue arm at all.
// This drives the arm directly and pins the constant it writes.
func TestExecuteRepoStateBenchmarkRun_RecordsAPrepareFailureAndContinues(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")
	cc := &prepareFaultingCompleter{since: time.Now(), caseIndex: 1}

	rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), cc, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)

	require.NoError(t, err, "a case that cannot be prepared is unmeasured, not fatal")
	require.NotEmpty(t, cc.planted, "the fixture must actually have blocked the next case's review dir")
	require.Len(t, rr.CaseFailures, 1)
	assert.Equal(t, "second-case", rr.CaseFailures[0].CaseID)
	assert.Equal(t, benchmark.CaseFailurePrepare, rr.CaseFailures[0].Reason,
		"the reason names the stage the case died at, and this is the only test that pins it for this site")
	assert.Equal(t, []string{"first-case"}, rr.Coverage[0].CaseIDs, "case 1 still scored")
}

// poolWriteFaultingCompleter makes ONE case's post-fan-out persistence fail, by
// planting a DIRECTORY where ExecuteReview must write summary.json. The agents
// themselves all succeed, so the error that comes back is neither ErrAllAgentsFailed
// nor ErrEmptyRoster — the non-sentinel execute failure that is supposed to be
// recorded and skipped rather than aborting the run.
//
// A completer call is the only moment a test runs while the loop is mid-case, and the
// planted path has to appear after PrepareReview built the scaffold and before
// writePool reaches it, which is exactly that window.
type poolWriteFaultingCompleter struct {
	since     time.Time
	caseIndex int
	calls     int
	planted   string
}

func (c *poolWriteFaultingCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	c.calls++
	if c.calls == c.caseIndex+1 {
		matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "atcr-repo-state-*"))
		for _, m := range matches {
			if fi, err := os.Stat(m); err == nil && fi.ModTime().After(c.since) {
				pool := filepath.Join(m, fmt.Sprintf("review-%d", c.caseIndex), "sources", "pool")
				if err := os.MkdirAll(filepath.Join(pool, "summary.json"), 0o755); err == nil {
					c.planted = pool
				}
			}
		}
	}
	return stubLocatedCompleter{}.Complete(ctx, inv)
}

// The non-sentinel ExecuteReview failure had coverage count 0. Every execute-path
// test drove ErrAllAgentsFailed, which ABORTS — so the record-and-continue arm beside
// it, and the reason constant it writes, were never executed. A wrong constant there
// ships invisibly, and the arm is the one that decides whether one case's bad luck
// costs the whole suite.
func TestExecuteRepoStateBenchmarkRun_RecordsANonSentinelExecuteFailureAndContinues(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")
	cc := &poolWriteFaultingCompleter{since: time.Now(), caseIndex: 1}

	rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), cc, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)

	require.NoError(t, err, "a non-sentinel execute failure is one case's bad luck, not the run's")
	require.NotEmpty(t, cc.planted, "the fixture must actually have planted the fault in the run's review dir")
	require.Len(t, rr.CaseFailures, 1)
	assert.Equal(t, "second-case", rr.CaseFailures[0].CaseID)
	assert.Equal(t, benchmark.CaseFailureExecute, rr.CaseFailures[0].Reason,
		"the reason constant is the actionable part, and this is the only test that pins it for this site")
	assert.Equal(t, []string{"first-case"}, rr.Coverage[0].CaseIDs, "case 1 still scored")
}

// The existing exit gates (--fail-on-case-failure, --max-case-failures) judge the run
// AFTER it finishes, so a provider failing every case still bills the whole suite
// before the operator learns anything. This cap is the mid-run half: it stops paying.
//
// OPT-IN, default off, which is what keeps it compatible with those gates rather than
// overlapping them — a default cap would take the abort decision away from the
// operator, which is why the work-dir arm's comment argues against a general one.
func TestExecuteRepoStateBenchmarkRun_ConsecutiveFailureCap(t *testing.T) {
	t.Run("aborts once the run of consecutive failures reaches the cap", func(t *testing.T) {
		suite := writeCaseSuite(t, "first-case", "second-case", "third-case", "fourth-case")
		for _, id := range []string{"first-case", "second-case", "third-case", "fourth-case"} {
			faultMaterialization(t, suite, id)
		}

		rr, _, err := executeRepoStateBenchmarkRun(context.Background(),
			benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 2)
		releaseRetainedWorkDirFromError(t, err)

		require.Error(t, err)
		assert.Nil(t, rr)
		assert.Contains(t, err.Error(), "2 consecutive case(s) failed",
			"the abort says WHY it stopped, so the operator can tell it from a suite that simply ended")
		assert.Contains(t, err.Error(), "materialize", "and names what kept failing")
	})

	t.Run("a cap of 0 is off, and the whole suite still runs", func(t *testing.T) {
		suite := writeCaseSuite(t, "first-case", "second-case", "third-case")
		faultMaterialization(t, suite, "first-case")
		faultMaterialization(t, suite, "second-case")

		rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
			benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
		releaseRetainedWorkDir(t, retained)

		require.NoError(t, err, "off by default: two consecutive failures do not stop a run that can still score case 3")
		assert.Len(t, rr.CaseFailures, 2)
		assert.Equal(t, []string{"third-case"}, rr.Coverage[0].CaseIDs)
	})

	t.Run("a scored case resets the run of failures", func(t *testing.T) {
		suite := writeCaseSuite(t, "first-case", "second-case", "third-case", "fourth-case")
		faultMaterialization(t, suite, "first-case")
		faultMaterialization(t, suite, "third-case")

		rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
			benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 2)
		releaseRetainedWorkDir(t, retained)

		require.NoError(t, err,
			"CONSECUTIVE, not cumulative: two failures separated by a scored case are not a systemic run")
		assert.Len(t, rr.CaseFailures, 2)
	})
}

// oneAgentFailingCompleter fails every call made on behalf of one named agent and
// serves the rest normally, so a case comes back with a MIXED roster: some slots OK,
// one failed. The whole-case failure channel cannot see that shape — the case was
// reviewed, and only one reviewer's slot died.
type oneAgentFailingCompleter struct{ agent string }

func (c oneAgentFailingCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	// Keyed on the MODEL, the only slot-identifying field an Invocation carries.
	if strings.Contains(inv.Model, c.agent) {
		return "", errors.New("provider timeout")
	}
	return stubLocatedCompleter{}.Complete(ctx, inv)
}

// The unmeasured-not-missed rule applies AT SLOT GRANULARITY too. The whole-case
// channel covers the all-reviewers case; one slot down, a provider timeout on 1 of 2
// reviewers still produced a CaseScore with Raised nil and a positional row matched
// against nothing — charging that reviewer full recall-0 for a case it never saw,
// which is the one conflation this tier's contract forbids.
//
// Score, covered set and outcome tally are skipped TOGETHER, which is what the second
// half of this test pins. Leaving the failure in the tally while omitting it from the
// covered set would break the runs == len(case_ids) == sum(outcomes) tamper check and
// make every run with a failed slot read as malformed at export.
//
// The failed slot is still VISIBLE, just on its own axis: benchmark.SlotFailure records
// the cause, which is what keeps the three-way skip from being silent.
func TestExecuteRepoStateBenchmarkRun_AFailedSlotIsUnmeasuredNotMissed(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")

	rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}, [3]string{"otto", "m-otto", "otto"}),
		oneAgentFailingCompleter{agent: "otto"}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)
	require.NoError(t, err)
	require.Len(t, rr.PositionalRecall, 2, "both reviewers still appear; one of them simply measured nothing")

	byPersona := map[string]benchmark.ReviewerPositionalRecall{}
	for _, r := range rr.PositionalRecall {
		byPersona[r.Persona] = r
	}
	require.Contains(t, byPersona, "greta")
	require.Contains(t, byPersona, "otto")

	assert.Positive(t, byPersona["greta"].ExpectedTotal, "the surviving reviewer scored the cases it saw")
	assert.Zero(t, byPersona["otto"].ExpectedTotal,
		"a slot that never ran adds 0 to the denominator; scoring it as recall-0 charges a reviewer for a case it was never shown")

	// The export gate enforces runs == len(case_ids) == sum(outcomes) as a tamper
	// check. Excluding a failed slot from the score but leaving it in either of the
	// other two would make every run with a failed slot read as MALFORMED at export,
	// after the panel was paid for — so the three have to move together.
	for i, cov := range rr.Coverage {
		tally := 0
		for _, n := range cov.Outcomes {
			tally += n
		}
		assert.Equal(t, len(cov.CaseIDs), tally,
			"coverage row %d: the outcomes tally and the covered set are written together", i)
		assert.Equal(t, rr.Reviewers[i].Runs, len(cov.CaseIDs),
			"coverage row %d: runs and the covered set are written together", i)
	}
}

// A skipped slot must leave a CAUSE behind, and the paid artifacts must survive it.
//
// Keeping the failed slot out of the score is right, but it recorded the shortfall
// nowhere: CaseFailures stays empty (the case itself was reviewed by the others), so
// warnCaseFailures printed nothing, the run exited 0, and the deferred cleanup took
// its clean-run branch and deleted the work dir — including the review-N/status.json
// the skip's own comment named as where the failure "remains readable". The operator
// then met the defect days later as an export rejection with no artifact to consult.
//
// The identity asserted here is the PUBLIC one, because the export diagnostic that
// consumes this array matches it against reviewer_coverage rows, which are emitted
// scrubbed.
func TestExecuteRepoStateBenchmarkRun_ASlotFailureIsRecordedAndRetainsTheWorkDir(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")

	rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}, [3]string{"otto", "m-otto", "otto"}),
		oneAgentFailingCompleter{agent: "otto"}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)
	require.NoError(t, err, "one failed slot is not a failed run")

	require.Empty(t, rr.CaseFailures,
		"the CASE was reviewed by the surviving reviewer — this is not a case-level failure")

	require.Len(t, rr.SlotFailures, 2,
		"otto's slot failed on both cases, and each (reviewer, case) pair is its own record")
	for _, sf := range rr.SlotFailures {
		assert.Equal(t, "m-otto", sf.Model, "the failed reviewer is named by its public identity")
		assert.Equal(t, "otto", sf.Persona)
		assert.Equal(t, benchmark.SlotFailureCall, sf.Reason,
			"a completer returning an error is a failed call, not a timeout")
	}
	ids := []string{rr.SlotFailures[0].CaseID, rr.SlotFailures[1].CaseID}
	assert.ElementsMatch(t, []string{"first-case", "second-case"}, ids,
		"every case the slot missed is named, so the short row can be explained case by case")

	// The retention half. Without it the cleanup deletes the only copy of the paid
	// panel's artifacts on exactly the run that needs them for diagnosis.
	require.NotEmpty(t, retained,
		"a run that lost a slot must retain its work dir and report the path")
	_, statErr := os.Stat(retained)
	require.NoError(t, statErr, "the retained work dir must still exist on disk")
}

// The operator surface for the same defect. A slot failure is invisible on stderr
// because warnCaseFailures keys on CaseFailures alone, so a run that silently lost a
// reviewer looked exactly like a clean one.
func TestWarnCaseFailures_ReportsSlotFailures(t *testing.T) {
	rr := &benchmark.RunResult{
		SuiteCaseIDs: []string{"case-01", "case-02"},
		SlotFailures: []benchmark.SlotFailure{
			{Model: "m-otto", Persona: "otto", CaseID: "case-01", Reason: benchmark.SlotFailureTimeout},
		},
	}

	var buf bytes.Buffer
	warnCaseFailures(&buf, rr, "/tmp/atcr-repo-state-probe")
	out := buf.String()

	assert.Contains(t, out, "m-otto/otto", "the warning names which reviewer lost a case")
	assert.Contains(t, out, "case-01", "and which case it lost")
	assert.Contains(t, out, benchmark.SlotFailureTimeout, "and why")
	assert.Contains(t, out, "/tmp/atcr-repo-state-probe",
		"the retained path reaches stderr on a slot-only failure too, not just a case failure")
}

// The two POST-PAYMENT record-and-continue sites. Their doc comments promise "the
// panel ran and was paid for; its artifacts are retained in the work dir", and nothing
// reached either one: a completer-planted fault always lands on the WRITE, one step
// earlier, and is recorded as `execute`. Faulted through the read-back seams instead,
// which is what those seams exist for.
//
// Each case asserts three things: the run continues, the reason constant is the
// site's own, and the paid review dir SURVIVES in the retained work dir — the last
// being the promise itself, and the half no test made.
func TestExecuteRepoStateBenchmarkRun_RecordsPostPaymentReadBackFailures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		wantReason string
		install    func(t *testing.T)
	}{
		{
			name:       "the pool summary cannot be read back",
			wantReason: benchmark.CaseFailurePoolSummary,
			install: func(t *testing.T) {
				real := readPoolSummaryFn
				calls := 0
				readPoolSummaryFn = func(reviewDir string) (fanout.PoolSummary, error) {
					calls++
					if calls == 2 {
						return fanout.PoolSummary{}, errors.New("summary.json unreadable")
					}
					return real(reviewDir)
				}
				t.Cleanup(func() { readPoolSummaryFn = real })
			},
		},
		{
			name:       "the findings cannot be read back",
			wantReason: benchmark.CaseFailureReadFindings,
			install: func(t *testing.T) {
				real := readCaseFindingsLocatedFn
				calls := 0
				readCaseFindingsLocatedFn = func(reviewDir string, agents map[string]bool) (map[string][]benchmark.ReportedFinding, map[string][]string, int, bool, error) {
					calls++
					if calls == 2 {
						return nil, nil, 0, false, errors.New("findings.txt unreadable")
					}
					return real(reviewDir, agents)
				}
				t.Cleanup(func() { readCaseFindingsLocatedFn = real })
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.install(t)
			suite := writeCaseSuite(t, "first-case", "second-case")

			rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
				benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
			releaseRetainedWorkDir(t, retained)

			require.NoError(t, err, "a read-back fault is one case's bad luck, not the run's")
			require.Len(t, rr.CaseFailures, 1)
			assert.Equal(t, "second-case", rr.CaseFailures[0].CaseID)
			assert.Equal(t, tc.wantReason, rr.CaseFailures[0].Reason,
				"the reason names the stage the case died at, and this is the only test that pins it")
			assert.Equal(t, []string{"first-case"}, rr.Coverage[0].CaseIDs, "case 1 still scored")

			require.NotEmpty(t, retained, "a partial run retains its work dir")
			assert.DirExists(t, filepath.Join(retained, "review-1"),
				"the FAILED case's panel ran and was paid for, so its artifacts are what the promise is about")
			assert.DirExists(t, filepath.Join(retained, "review-0"),
				"the scored case's artifacts survive alongside")
		})
	}
}

// The repo-state emit tail carried only ONE of buildRunResult's two identity guards.
// validatePublishableReviewerRoster covers the CONFIGURED registry values, but
// reviewerModel prefers the usage-reported (and fallback) model over the registry —
// so a Cc/Cf rune arriving in a provider's own usage payload passes the roster gate
// untouched. scorecard.ScrubPublicString leaves those runes alone, so it survived
// into Reviewers[i].Model and export hard-rejected the finished artifact.
//
// It is worse on this tier than on standard-v1: that rejection's remedy is a
// hand-repair of the checkpoint, and checkRepoStateFlags REFUSES --checkpoint here,
// so the only way out was re-running the whole paid panel — which re-derives the same
// rune. The run must fail BEFORE the panel is paid for.
//
// The rune is injected through the pool-summary seam because that is exactly where a
// provider-reported model enters: the stub completer reports no usage, so without it
// the realized model falls back to the registry value the roster gate already covers.
func TestExecuteRepoStateBenchmarkRun_RejectsANonPrintingRuneInTheRealizedIdentity(t *testing.T) {
	real := readPoolSummaryFn
	readPoolSummaryFn = func(reviewDir string) (fanout.PoolSummary, error) {
		s, err := real(reviewDir)
		if err != nil {
			return s, err
		}
		require.NotEmpty(t, s.Agents, "the fixture must produce an agent slot to poison")
		for i := range s.Agents {
			// A bidi override in the USAGE-REPORTED model — invisible in a rendered
			// document, and never present in any local config file.
			s.Agents[i].Model = "m-greta\u202eevil"
		}
		return s, nil
	}
	t.Cleanup(func() { readPoolSummaryFn = real })

	_, retained, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)

	require.Error(t, err, "a non-printing rune in the realized identity must fail the run, not reach export")
	assert.Contains(t, err.Error(), "non-printing rune", "the rejection must name the defect class")
	assert.Contains(t, err.Error(), "U+202E", "and the offending rune, which is invisible in the value itself")
}

// The FallbackUsed accounting (`if a.FallbackUsed { acc[key].fallbackCases++ }`) was
// an uncovered added line: no repo-state test drove a case whose reviewer was served
// by a fallback, so the per-reviewer fallback_cases figure this tier PUBLISHES was
// unverified end to end.
//
// The fault is injected at readPoolSummaryFn rather than by configuring a real
// fallback chain, and the boundary is deliberate. What is untested here is the
// RUNNER's accounting — does a reported fallback reach the emitted coverage row —
// not fanout's decision to report one, which internal/fanout tests on its own. The
// seam stands in for exactly one fact ("fanout said this slot was served by a
// fallback") and the assertion is about what the runner then does with it, so the
// stub cannot guarantee its own result.
func TestExecuteRepoStateBenchmarkRun_FallbackCasesReachTheCoverageRow(t *testing.T) {
	real := readPoolSummaryFn
	readPoolSummaryFn = func(reviewDir string) (fanout.PoolSummary, error) {
		s, err := real(reviewDir)
		if err != nil {
			return s, err
		}
		require.NotEmpty(t, s.Agents, "the fixture must produce at least one agent slot to mark")
		for i := range s.Agents {
			s.Agents[i].FallbackUsed = true
		}
		return s, nil
	}
	t.Cleanup(func() { readPoolSummaryFn = real })

	rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)
	require.NoError(t, err)
	require.NotEmpty(t, rr.Coverage)

	assert.Equal(t, 1, rr.Coverage[0].FallbackCases,
		"a fallback-served case must be counted on the row this tier publishes")

	// And onto the wire, since the count is only useful to a reader of the file.
	data, jerr := json.Marshal(rr)
	require.NoError(t, jerr)
	assert.Contains(t, string(data), `"fallback_cases":1`)
}

// The two warn branches beside the findings read-back were uncovered added lines: no
// test drove the RUNNER with a case that produced no findings file, or with skipped
// rows naming an off-panel reviewer. readCaseFindingsLocated returns both signals and
// is itself tested; the runner's SURFACING of them is what was unverified, so a
// regression that silently stopped warning would have shipped green.
//
// Neither is a case failure — the case is still scored — which is exactly why they
// need their own coverage: nothing else in the run-result records that they fired.
func TestExecuteRepoStateBenchmarkRun_WarnsOnMissingFindingsFileAndUnattributedRows(t *testing.T) {
	for _, tc := range []struct {
		name     string
		missing  bool
		unattrib int
		wantLog  string
	}{
		// Anchored on the FULL message, not a bare word: "findings" and
		// "unattributed" each appear in the other branch's line and in unrelated
		// runner output, so a bare-substring assertion would pass with the branch
		// under test deleted.
		{
			name:    "no findings file for the case",
			missing: true,
			wantLog: "case produced no findings file; every reviewer reads as raised-nothing",
		},
		{
			name:     "skipped rows name a reviewer off the panel",
			unattrib: 3,
			wantLog:  "skipped finding rows name a reviewer not in the panel; counted as unattributed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			real := readCaseFindingsLocatedFn
			readCaseFindingsLocatedFn = func(reviewDir string, agents map[string]bool) (map[string][]benchmark.ReportedFinding, map[string][]string, int, bool, error) {
				loc, cat, _, _, err := real(reviewDir, agents)
				return loc, cat, tc.unattrib, tc.missing, err
			}
			t.Cleanup(func() { readCaseFindingsLocatedFn = real })

			var logs bytes.Buffer
			ctx := logCapturingContext(t, &logs)

			rr, retained, err := executeRepoStateBenchmarkRun(ctx,
				benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{},
				repoStateMiniPath, time.Unix(0, 0).UTC(), 0)
			releaseRetainedWorkDir(t, retained)

			require.NoError(t, err, "neither signal is a case failure — the case is still scored")
			require.Empty(t, rr.CaseFailures, "these are warnings, not failures")
			assert.Contains(t, logs.String(), tc.wantLog,
				"the runner must surface the signal the reader returned; nothing else records that it fired")
		})
	}
}

// The work_dir record-and-continue site had coverage count 0: no test drove the
// RUNNER to a work-dir failure, so the reason constant it records was unverified end
// to end and a wrong one would have shipped invisibly.
func TestExecuteRepoStateBenchmarkRun_RecordsAWorkDirFailureAndContinues(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")
	cc := &workDirFaultingCompleter{since: time.Now(), perCase: 1}
	t.Cleanup(func() {
		if cc.tmp != "" {
			_ = os.Chmod(cc.tmp, 0o755)
			_ = os.RemoveAll(cc.tmp)
		}
	})

	rr, _, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), cc, suite, time.Unix(0, 0).UTC(), 0)

	require.NoError(t, err, "a path-specific work-dir fault is one case's bad luck, not the run's")
	require.NotEmpty(t, cc.tmp, "the fixture must actually have found and faulted the run's work dir")
	require.Len(t, rr.CaseFailures, 1)
	assert.Equal(t, "second-case", rr.CaseFailures[0].CaseID)
	assert.Equal(t, benchmark.CaseFailureWorkDir, rr.CaseFailures[0].Reason,
		"the reason constant is the actionable part, and this is the only test that pins it for this site")
	assert.Equal(t, []string{"first-case"}, rr.Coverage[0].CaseIDs, "case 1 still scored")
}

// The retained path reaches the operator on the UN-SUPPRESSIBLE channel. It used to
// exist only in the runner's Warn line, and ATCR_LOG_LEVEL=error -- a level
// log.LevelFromString accepts -- drops that line entirely, leaving the only copy of a
// paid panel's artifacts in an unnamed /tmp/atcr-repo-state-* directory. The hard
// failure arm never had this hole because it wraps the path into its error.
func TestWarnCaseFailures_NamesTheRetainedWorkDirWhenItHasOne(t *testing.T) {
	var buf bytes.Buffer

	warnCaseFailures(&buf, &benchmark.RunResult{
		SuiteCaseIDs: []string{"case-01", "case-02"},
		CaseFailures: []benchmark.CaseFailure{{CaseID: "case-02", Reason: benchmark.CaseFailurePrepare}},
	}, "/tmp/atcr-repo-state-abc123")

	assert.Contains(t, buf.String(), "/tmp/atcr-repo-state-abc123",
		"the operator must not have to go hunting for the artifacts on stderr's own channel")
}

// A PARTIAL run hands the path back to its caller, so the wiring above has something
// real to print. This is the end-to-end half: the runner's deferred retention arm is
// the only place that knows the dir survived, and it is what sets the return value.
func TestExecuteRepoStateBenchmarkRun_ReturnsTheRetainedWorkDirOnAPartialRun(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")
	faultMaterialization(t, suite, "second-case")

	rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)

	releaseRetainedWorkDir(t, retained)
	require.NoError(t, err)
	require.Len(t, rr.CaseFailures, 1, "the fixture has to actually produce a partial run")
	require.NotEmpty(t, retained, "a partial run retains its work dir and must say where")
	assert.DirExists(t, retained, "the reported path is the real retained dir, not a stale string")
}

// A CLEAN run retains nothing, so it reports nothing -- otherwise the caller would
// print a path that the deferred cleanup has already removed.
func TestExecuteRepoStateBenchmarkRun_ReportsNoRetainedWorkDirOnACleanRun(t *testing.T) {
	rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{},
		writeCaseSuite(t, "first-case", "second-case"), time.Unix(0, 0).UTC(), 0)

	require.NoError(t, err)
	require.Empty(t, rr.CaseFailures)
	assert.Empty(t, retained, "a clean run cleans up, so there is no path to hand back")
}

// releaseCaseRepo's doc comment is explicitly about the FAILURE paths, and its only
// test covered the success path. The materialize arm is the one where MaterializeCase
// may have been interrupted part-way, so it is the arm worth proving: after the run,
// the failed case's repo dir is gone from the retained work dir.
func TestExecuteRepoStateBenchmarkRun_ReleasesTheFailedCaseRepo(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")
	faultMaterialization(t, suite, "second-case")

	rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)
	require.NoError(t, err)
	require.Len(t, rr.CaseFailures, 1)
	require.NotEmpty(t, retained)

	// repo-1 is the failed case (cases are keyed by index), and the review dirs are
	// what retention is FOR, so they must survive alongside.
	assert.NoDirExists(t, filepath.Join(retained, "repo-1"),
		"the failed case's materialized repo is released, not left to accumulate")
	assert.DirExists(t, filepath.Join(retained, "review-0"),
		"the scored case's paid review artifacts are exactly what retention protects")
}

// Both interpolated fields are stripped, and nothing drove either wrapper: removing
// them was invisible to the suite. Its sibling on the other operator-facing surface,
// TestCheckCoverage_StripsTerminalControlRunesFromShortfallWarning, pins exactly this
// for checkCoverage, so only the case was missing. Defence in depth rather than the
// live boundary — validateSuitePublishableCaseIDs rejects control runes at load — which
// is why an untested wrapper here would have rotted quietly.
func TestWarnCaseFailures_StripsTerminalControlRunesFromBothFields(t *testing.T) {
	var buf bytes.Buffer

	warnCaseFailures(&buf, &benchmark.RunResult{
		SuiteCaseIDs: []string{"case-01", "case-02"},
		CaseFailures: []benchmark.CaseFailure{
			{CaseID: "case-02\x1b[2J", Reason: benchmark.CaseFailurePrepare + "\u200b"},
		},
	}, "")

	out := buf.String()
	assert.NotContains(t, out, "\x1b", "an ANSI sequence in a case id must not reach the operator's terminal")
	assert.NotContains(t, out, "\u200b", "the reason is read off the same untrusted file as the case id")
	assert.Contains(t, out, "case-02", "stripping removes the control runes, not the identifier")
	assert.Contains(t, out, benchmark.CaseFailurePrepare, "the stage still has to be readable")
}

// warnCaseFailures used to print one line per failure with no cap, unlike every
// sibling diagnostic in this file. A suite that loses 200 cases to a systemic fault
// wrote 200 lines to stderr AHEAD of the recall summary the warning exists to
// qualify — pushing the number the operator actually needs off the visible terminal,
// which is the exact misreading the function was added to prevent.
func TestWarnCaseFailures_CapsTheListWithAnOverflowCount(t *testing.T) {
	var buf bytes.Buffer
	rr := &benchmark.RunResult{}
	for i := 0; i < maxNamedFailedCases+4; i++ {
		id := fmt.Sprintf("case-%02d", i)
		rr.SuiteCaseIDs = append(rr.SuiteCaseIDs, id)
		rr.CaseFailures = append(rr.CaseFailures,
			benchmark.CaseFailure{CaseID: id, Reason: benchmark.CaseFailureExecute})
	}

	warnCaseFailures(&buf, rr, "")

	out := buf.String()
	assert.Equal(t, maxNamedFailedCases, strings.Count(out, ": failed at "),
		"the per-case list is capped, like summarizeMissing's")
	assert.Contains(t, out, "and 4 more", "the cases past the cap are counted, not dropped silently")
	assert.Contains(t, out, fmt.Sprintf("%d of %d", len(rr.CaseFailures), len(rr.SuiteCaseIDs)),
		"the scale line still reports the true total, which is the number the cap must not hide")
}

// A clean run says nothing, exactly as the sibling summaries do on a suite that
// carries none of their signal.
func TestWarnCaseFailures_SilentOnACleanRun(t *testing.T) {
	var buf bytes.Buffer

	warnCaseFailures(&buf, &benchmark.RunResult{SuiteCaseIDs: []string{"case-01"}}, "")
	warnCaseFailures(&buf, nil, "")

	assert.Empty(t, buf.String())
}

// The fatal/transient split is a PREDICATE over errnos, and it had no unit test at
// all — so the four fatal values and the three deliberately-excluded ones were
// asserted nowhere, in either direction.
//
// The exclusions matter as much as the inclusions: EACCES/EPERM/ENOTDIR can be
// specific to the path being created, so classifying them fatal would abort a whole
// paid suite over one bad case directory. Both directions are pinned here.
func TestIsFatalWorkDirError(t *testing.T) {
	for _, e := range []syscall.Errno{syscall.ENOSPC, syscall.EMFILE, syscall.ENFILE, syscall.EROFS, syscall.EDQUOT} {
		assert.Truef(t, isFatalWorkDirError(e),
			"%v is a HOST fault: every remaining case repeats the identical syscall and fails identically", e)
		// Wrapped, because the call site never sees a bare errno — os.MkdirAll returns
		// a *PathError. A predicate using == instead of errors.Is would pass the bare
		// case above and fail every real one.
		assert.Truef(t, isFatalWorkDirError(&os.PathError{Op: "mkdir", Path: "/x", Err: e}),
			"%v must still classify through the *PathError os.MkdirAll actually returns", e)
	}

	for _, e := range []syscall.Errno{syscall.EACCES, syscall.EPERM, syscall.ENOTDIR} {
		assert.Falsef(t, isFatalWorkDirError(e),
			"%v can be specific to the path being created, so it stays a per-case fault", e)
	}

	assert.False(t, isFatalWorkDirError(nil), "no error is not a fatal one")
	assert.False(t, isFatalWorkDirError(errors.New("some other failure")),
		"an unclassified error stays per-case rather than aborting a paid suite")
}

// The CALL SITE, driven through the seam. The unit test above proves the predicate;
// nothing proved the runner consults it, and deleting the guard left the cli suite
// green because the only fault a test could previously stage at this site was EACCES
// — which is excluded from the fatal set by design.
//
// Asserts the three things the abort promises: the run stops, the message says the
// fault is host-level rather than case-specific, and NO case_failures entry was
// written — recording one is precisely the 200-entries-and-exit-0 behaviour the
// guard exists to prevent.
func TestExecuteRepoStateBenchmarkRun_AHostLevelWorkDirFaultAborts(t *testing.T) {
	real := mkdirAllFn
	mkdirAllFn = func(path string, perm os.FileMode) error {
		return &os.PathError{Op: "mkdir", Path: path, Err: syscall.ENOSPC}
	}
	t.Cleanup(func() { mkdirAllFn = real })

	suite := writeCaseSuite(t, "first-case", "second-case")
	rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{},
		suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)
	releaseRetainedWorkDirFromError(t, err)

	require.Error(t, err, "a full disk is not one case's bad luck")
	assert.Contains(t, err.Error(), "host-level fault",
		"the message must say why this is not being recorded as a per-case failure")
	assert.Contains(t, err.Error(), "first-case", "and which case hit it")
	assert.Nil(t, rr, "an aborted run publishes no run-result")
}

// The mirror, and it is what keeps the guard NARROW. A path-specific fault at the
// same site must still record-and-continue, or one unwritable case directory would
// forfeit a whole paid panel.
func TestExecuteRepoStateBenchmarkRun_APathSpecificWorkDirFaultContinues(t *testing.T) {
	real := mkdirAllFn
	calls := 0
	mkdirAllFn = func(path string, perm os.FileMode) error {
		calls++
		if calls == 1 {
			return &os.PathError{Op: "mkdir", Path: path, Err: syscall.EACCES}
		}
		return real(path, perm)
	}
	t.Cleanup(func() { mkdirAllFn = real })

	suite := writeCaseSuite(t, "first-case", "second-case")
	rr, retained, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{},
		suite, time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDir(t, retained)

	require.NoError(t, err, "EACCES is path-specific, so the run continues with the next case")
	require.Len(t, rr.CaseFailures, 1, "the lost case is recorded rather than aborting the run")
	assert.Equal(t, benchmark.CaseFailureWorkDir, rr.CaseFailures[0].Reason)
	assert.Equal(t, "first-case", rr.CaseFailures[0].CaseID)
}

// The IN-LOOP cancellation check exists to stop the run at the next case boundary.
// Nothing asserted that, and the check survived deletion: with it gone the loop
// keeps iterating, every remaining case fails under the cancelled context, and the
// POST-LOOP guard then prints the identical "cancelled after N of M case(s)"
// sentence the old assertions matched. Both arms produce the same error, so only the
// work actually attempted tells them apart.
//
// Counted through mkdirAllFn, which runs exactly once per case entered — the first
// thing the loop body does after the two guards. One call means the loop stopped at
// the boundary; three means it ground through the suite the operator interrupted.
func TestExecuteRepoStateBenchmarkRun_CancellationStopsTheLoopAtTheNextCase(t *testing.T) {
	realMkdir := mkdirAllFn
	cases := 0
	mkdirAllFn = func(path string, perm os.FileMode) error {
		cases++
		return realMkdir(path, perm)
	}
	t.Cleanup(func() { mkdirAllFn = realMkdir })

	ctx, cancel := context.WithCancel(context.Background())
	cc := &cancellingCompleter{cancel: cancel}

	rr, _, err := executeRepoStateBenchmarkRun(ctx,
		benchCfg([3]string{"greta", "m-greta", "greta"}), cc,
		writeCaseSuite(t, "first-case", "second-case", "third-case"), time.Unix(0, 0).UTC(), 0)
	releaseRetainedWorkDirFromError(t, err)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, rr)
	assert.Equal(t, 1, cases,
		"the interrupt landed during case 1, so case 2 must never be entered — the in-loop check "+
			"is what stops the bill, and without it the loop walks the rest of the suite")
}

// The slot list is CAPPED like the case list, and for the same reason: one dead
// provider on a 200-case suite would otherwise write 200 lines ahead of the recall
// summary this warning exists to qualify, scrolling it off the terminal.
func TestWarnCaseFailures_CapsTheSlotList(t *testing.T) {
	rr := &benchmark.RunResult{SuiteCaseIDs: make([]string, 25)}
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("case-%02d", i)
		rr.SuiteCaseIDs[i] = id
		rr.SlotFailures = append(rr.SlotFailures, benchmark.SlotFailure{
			Model: "m", Persona: "p", CaseID: id, Reason: benchmark.SlotFailureCall,
		})
	}

	var buf bytes.Buffer
	warnCaseFailures(&buf, rr, "")
	out := buf.String()

	assert.Equal(t, maxNamedFailedCases, strings.Count(out, ": "+benchmark.SlotFailureCall),
		"exactly maxNamedFailedCases slot lines are printed")
	assert.Contains(t, out, fmt.Sprintf("... and %d more", 25-maxNamedFailedCases),
		"the overflow count carries the rest")
	assert.Contains(t, out, "25 reviewer slot(s)",
		"and the scale line still carries the true total")
}
