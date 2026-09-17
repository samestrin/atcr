package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
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
	root := t.TempDir()
	for _, id := range []string{"first-case", "second-case"} {
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
	require.NoError(t, os.WriteFile(filepath.Join(root, "suite.json"), []byte(
		"{\"suite\":\"repo-state-v1\",\"suite_version\":\"1.0.0\",\"cases\":[{\"id\":\"first-case\",\"dir\":\"first-case\"},{\"id\":\"second-case\",\"dir\":\"second-case\"}]}"), 0o600))
	return root
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

	_, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), cc, suite, time.Unix(0, 0).UTC())

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
	suite := writeTwoCaseSuite(t)
	cc := &repoCountingCompleter{}

	_, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), cc, suite, time.Unix(0, 0).UTC())
	require.NoError(t, err)

	require.NotEmpty(t, cc.reposSeenPerCall, "the stub completer must have been called")
	assert.Equal(t, 1, cc.reposSeenPerCall[len(cc.reposSeenPerCall)-1],
		"case 2's completer call must see only case 2's repo; case 1's must already be released")
}

// repoCountingCompleter counts the repo-N directories visible under the run's
// temp prefix at every completer call — the only observation point a test has
// inside the paid loop.
type repoCountingCompleter struct {
	reposSeenPerCall []int
}

func (c *repoCountingCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "atcr-repo-state-*", "repo-*"))
	c.reposSeenPerCall = append(c.reposSeenPerCall, len(matches))
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

	rr, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), cc, suite, time.Unix(0, 0).UTC())
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

	_, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC())

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

	rr, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "gpt-4o", "greta"}), cc, writeTwoCaseSuite(t), time.Unix(0, 0).UTC())
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

	rr, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC())
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

	_, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC())

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

	rr, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC())
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

	rr, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC())
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

	_, err := executeRepoStateBenchmarkRun(context.Background(),
		benchCfg([3]string{"greta", "m-greta", "greta"}), stubLocatedCompleter{}, suite, time.Unix(0, 0).UTC())

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

	located, categorical, unattributed, err := readCaseFindingsLocated(dir, map[string]bool{"greta": true})
	require.NoError(t, err)
	assert.Equal(t, 1, unattributed, "the skipped row naming an unknown reviewer is counted, not silently dropped")
	assert.Len(t, located["greta"], 1)
	assert.Len(t, categorical["greta"], 1)

	// With nobody ON the panel the same row is attributed normally.
	_, _, unattributed, err = readCaseFindingsLocated(dir, map[string]bool{"greta": true, "nobody": true})
	require.NoError(t, err)
	assert.Zero(t, unattributed)
}

func TestExecuteRepoStateBenchmarkRun_ReportsBothMetrics(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	rr, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{}, repoStateMiniPath, gen)
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

	rr, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubInDiffOnlyCompleter{}, repoStateMiniPath, gen)
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
	rr, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC())
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
	_, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, time.Unix(0, 0).UTC())
	require.Error(t, err)
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

// The repo-state runner was missing the REALIZED half of the printability rule
// validatePublishableReviewerRoster applies at load: reviewerModel prefers the
// provider-reported model over the registry, so a Cc/Cf control rune echoed back
// in a usage payload never passes the roster gate. buildRunResult catches it at
// the producer on the standard tier; this runner folded per raw key, so the rune
// flowed into the run-result and validateRunResultForPublication refused the file
// permanently — with no checkpoint on this tier, so nothing to repair-and-resume
// from. The runner must fail closed at the producer instead.
func TestExecuteRepoStateBenchmarkRun_RefusesANonPrintingRealizedIdentity(t *testing.T) {
	// U+200D ZERO WIDTH JOINER is unicode.Cf: invisible, survives every upstream
	// whitespace scrub, and reorders nothing — but validateRunResultForPublication
	// hard-rejects it, so the runner is the only surface that can say so cheaply.
	cfg := benchCfg([3]string{"greta", "m-greta\u200d", "greta"})
	_, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC())
	require.Error(t, err, "a realized identity carrying a Cf rune must fail the run at the producer")
	assert.Contains(t, err.Error(), "non-printing rune")
	assert.Contains(t, err.Error(), "m-greta\u200d",
		"the error must name the offending identity so the operator can find the source")
}
