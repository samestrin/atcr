package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runBCheckpointPath is the REAL checkpoint from epic 35.16.5.3's Run B, promoted to
// a committed fixture. It is the first recorded instance of a mid-suite model
// failover, and it is the artifact that exposed the misattribution this epic fixes.
//
// It is committed rather than referenced in place because its original home
// (.planning/epics/completed/35.16.5.3-results/shipped/) is gitignored and so cannot
// serve as CI evidence. 35.16.5.3 had to reconstruct its entire before-side from
// prose in a code review because the preceding dry-run's raw output was never kept;
// committing this one makes the regression permanent instead of repeating that loss.
// It contains no secrets — public-OSS case ids, category words, model names, timings.
// It is preserved verbatim, INCLUDING a production finding-parse artifact: three
// cases (index 9, 12, 16) record numeric or empty strings in `raised` — EST_MINUTES
// values a finding-parse misalignment captured into the category column. Do not
// sanitize them; the fixture's value is that it is what a real run actually wrote.
const runBCheckpointPath = "../internal/benchmark/testdata/run-b.ckpt.json"

// runBSuitePath is the standard-v1 suite Run B executed against, bundled in this
// repo. Its reproducibility hash equals the one the checkpoint recorded, which is
// what lets the fixture resume through the production path with every suite-identity
// guard armed instead of against a manifest derived from the checkpoint itself.
const runBSuitePath = "../benchmarks/standard-v1"

// runBManifest reconstructs the suite manifest Run B executed against, from the
// checkpoint's own recorded case ids and expected categories. The suite content is
// not needed — no diff is read and no review is executed — only the case identity
// list that coverage is measured against.
func runBManifest(t *testing.T, cp *runCheckpoint) *benchmark.Manifest {
	t.Helper()
	m := &benchmark.Manifest{Suite: cp.Suite, SuiteVersion: cp.SuiteVersion}
	for _, c := range cp.Cases {
		m.Cases = append(m.Cases, benchmark.Case{ID: c.CaseID, ExpectedCategories: c.Expected})
	}
	return m
}

// resumeRunB drives a checkpoint at cpPath through the PRODUCTION resume entry
// point — executeBenchmarkRun against the committed standard-v1 suite — and returns
// the run-result, or the error the resume guards raised.
//
// Nothing is executed and nothing is paid for: the checkpoint holds all 17 cases, so
// every index is replayed and the completer below is never reached.
func resumeRunB(t *testing.T, cpPath string) (*benchmark.RunResult, error) {
	t.Helper()
	// The roster must be the panel the checkpoint recorded, or validateCheckpointRoster
	// rejects it — which is the point: that guard is one of the things this path
	// exercises and the hand-written loop skipped.
	cfg := benchCfg([3]string{"brad", "qwen3.8-max", "brad"}, [3]string{"kai", "kimi-k3", "kai"})
	_ = cfg
	gen := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)

	// STUB (RED): the hand-written fold this item exists to replace — no suite is
	// loaded, so no hash, roster or case-identity guard can fire.
	cp, err := loadCheckpoint(cpPath)
	if err != nil {
		return nil, err
	}
	accs := map[reviewerKey]*reviewerAcc{}
	var order []reviewerKey
	for _, entry := range cp.Cases {
		if ferr := replayCheckpointCase(accs, &order, entry, entry.Expected); ferr != nil {
			return nil, ferr
		}
	}
	return buildRunResult(accs, order, runBManifest(t, cp), gen)
}

// mustNotCallCompleter fails the test if the resume path reaches an LLM at all. A
// fully-checkpointed run must replay every case; a single call here would mean the
// fixture silently stopped being a resume test.
type mustNotCallCompleter struct{ t *testing.T }

func (c mustNotCallCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	c.t.Fatal("the Run B checkpoint covers every case; resuming it must execute no review")
	return "", nil
}

// copyRunBCheckpoint copies the committed fixture into a temp dir so a test may
// tamper with it, and so the resume path can never write back to the real artifact.
// mutate may rewrite the raw JSON before it is written; nil copies verbatim.
func copyRunBCheckpoint(t *testing.T, mutate func(string) string) string {
	t.Helper()
	raw, err := os.ReadFile(runBCheckpointPath)
	require.NoError(t, err)
	body := string(raw)
	if mutate != nil {
		body = mutate(body)
	}
	path := filepath.Join(t.TempDir(), "run-b.ckpt.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// foldRunB replays every checkpointed case through the SAME accumulator path a live
// run uses, and folds the result into a RunResult.
func foldRunB(t *testing.T) (*runCheckpoint, *benchmark.RunResult) {
	t.Helper()
	cp, err := loadCheckpoint(runBCheckpointPath)
	require.NoError(t, err)
	require.NotNil(t, cp, "the Run B fixture must be committed and loadable")
	require.Len(t, cp.Cases, 17, "Run B is a 17-case suite run")

	accs := map[reviewerKey]*reviewerAcc{}
	var order []reviewerKey
	for _, entry := range cp.Cases {
		require.NoError(t, replayCheckpointCase(accs, &order, entry, entry.Expected))
	}
	gen := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	rr, err := buildRunResult(accs, order, runBManifest(t, cp), gen)
	require.NoError(t, err)
	return cp, rr
}

// The resume guards must actually fire on this fixture. A checkpoint whose case at
// one index no longer names the suite's case at that index is a changed suite, and
// resuming it would score a recorded result against the wrong case.
//
// This is the property the hand-written fold loop could not have: it derived the
// manifest from the checkpoint's own case ids, so the identity guard compared the
// artifact against itself and could never fail.
func TestRunBFixture_TamperedCaseIDIsRejectedByTheResumeGuards(t *testing.T) {
	cpPath := copyRunBCheckpoint(t, func(body string) string {
		return strings.Replace(body,
			`"case_id": "bluewave-labs-checkmate-pr-2883"`,
			`"case_id": "not-a-suite-case"`, 1)
	})

	_, err := resumeRunB(t, cpPath)

	require.Error(t, err, "a checkpoint case that is not the suite's case at that index must be rejected")
	assert.Contains(t, err.Error(), "not-a-suite-case", "the diagnostic names the offending id")
}

// AC1 + AC6: folding the real Run B checkpoint yields ONE ROW PER REALIZED MODEL.
//
// Two lanes both failed over mid-suite. brad's primary `qwen3.8-max` exhausted its
// Alibaba token plan after 8 cases and `llm-large` served the remaining 9; kai's
// `kimi-k3` served 16 and `kimi-k3-256k` served 1. Before this epic the run-result
// reported TWO rows — one per lane — crediting `qwen3.8-max` with 17 runs, 9 of which
// another model produced. That is a data-integrity defect at exactly the point the
// public leaderboard consumes.
//
// Note the epic's own AC6 text names only brad's split; kai's second failover is in
// the artifact too and is asserted here for the same reason.
func TestRunBFixture_SplitsIntoFourRealizedModelRows(t *testing.T) {
	_, rr := foldRunB(t)

	got := map[string]int{}
	for _, c := range rr.Coverage {
		got[c.Model+"/"+c.Persona] = len(c.CaseIDs)
	}
	assert.Equal(t, map[string]int{
		"qwen3.8-max/brad": 8,
		"llm-large/brad":   9,
		"kimi-k3/kai":      16,
		"kimi-k3-256k/kai": 1,
	}, got, "each row carries exactly the cases its model actually served")

	// Nothing discarded and nothing double-counted: each lane's rows partition its 17
	// cases. This is the property that makes scoring the backup AS ITSELF the right
	// response to a failover, rather than discarding it or misattributing it.
	assert.Equal(t, 17, got["qwen3.8-max/brad"]+got["llm-large/brad"], "brad's rows partition the suite")
	assert.Equal(t, 17, got["kimi-k3/kai"]+got["kimi-k3-256k/kai"], "kai's rows partition the suite")

	// The rows must be disjoint, not merely correctly sized.
	seen := map[string]map[string]bool{}
	for _, c := range rr.Coverage {
		seen[c.Persona] = orEmpty(seen[c.Persona])
		for _, id := range c.CaseIDs {
			assert.False(t, seen[c.Persona][id], "case %q attributed twice within lane %q", id, c.Persona)
			seen[c.Persona][id] = true
		}
	}

	// And the reviewer rows agree with the coverage rows they describe.
	require.Len(t, rr.Reviewers, 4)
	for i, rev := range rr.Reviewers {
		assert.Equal(t, rev.Runs, len(rr.Coverage[i].CaseIDs),
			"row %s/%s: `runs` must equal the size of its covered set", rev.Model, rev.Persona)
	}
}

func orEmpty(m map[string]bool) map[string]bool {
	if m == nil {
		return map[string]bool{}
	}
	return m
}

// AC5 + AC6: the run-result Run B produces is REJECTED by `benchmark export` by
// default. No row covers the full 17-case suite once attribution is honest, so
// publishing any of them would compare a partial measurement against a full one.
func TestRunBFixture_RejectedByExportGate(t *testing.T) {
	_, rr := foldRunB(t)

	for _, c := range rr.Coverage {
		require.Less(t, len(c.CaseIDs), 17,
			"the premise of this test: after the split, NO row covers the whole suite")
	}

	in := filepath.Join(t.TempDir(), "run-result.json")
	data, err := json.MarshalIndent(rr, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(in, data, 0o600))

	// execCmdCapture (not execCmdSplit) because the rejection travels as the RunE
	// error, which only that helper folds into the captured output.
	code, msg := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.NotEqual(t, 0, code, "a run in which every row is short must not publish: %s", msg)

	for _, want := range []string{"qwen3.8-max", "llm-large", "kimi-k3", "8/17", "9/17", "16/17"} {
		assert.Contains(t, msg, want, "the rejection must name the short rows and their shortfall")
	}

	// The opt-out publishes the same run, but names the shortfall on stderr so an
	// operator who insists cannot do so silently.
	code, stdout, stderr := execCmdSplit(t, "benchmark", "export", "--in", in, "--allow-partial-coverage")
	require.Equal(t, 0, code, "the explicit opt-out permits publication: %s%s", stdout, stderr)
	assert.Contains(t, stdout, "benchmark-suite")
	assert.Contains(t, stderr, "partial coverage")
	assert.Contains(t, stderr, "qwen3.8-max")
}

// AC8: Run B's checkpoint predates the outcome vocabulary, so every one of its cases
// replays as `unknown` — never as `clean`.
//
// This pins the backward-compatibility contract against a REAL pre-epic file rather
// than asserting it in prose. Run B recorded 17 zero-finding results whose meaning
// 35.16.5.3 could not recover; the honest report is that nobody knows which were
// clean reviews and which were unusable output, and `unknown` says exactly that.
// A boolean-pair encoding would have defaulted them all to "reviewed and found
// nothing" — inventing the very claim the epic exists to stop making.
func TestRunBFixture_PreOutcomeCasesReplayAsUnknown(t *testing.T) {
	cp, rr := foldRunB(t)

	// Guard the premise: if the fixture ever gains outcome data, this test is
	// asserting nothing and must be rewritten rather than silently passing.
	for _, c := range cp.Cases {
		for _, r := range c.Reviewers {
			require.Equal(t, benchmark.OutcomeUnknown, r.Outcome,
				"fixture premise: the Run B checkpoint predates the outcome field")
		}
	}

	total := 0
	for _, c := range rr.Coverage {
		assert.Equal(t, len(c.CaseIDs), c.Outcomes[benchmark.OutcomeUnknownLabel],
			"row %s/%s: every replayed case is unknown", c.Model, c.Persona)
		assert.Zero(t, c.Outcomes[benchmark.OutcomeClean],
			"row %s/%s: an unrecorded outcome must NEVER read as a clean review", c.Model, c.Persona)
		total += c.Outcomes[benchmark.OutcomeUnknownLabel]
	}
	assert.Equal(t, 34, total, "2 lanes x 17 cases, all unknown")
}

// The fixture must remain loadable through the ordinary checkpoint path — including
// its integrity checks — so it keeps testing the real resume contract rather than a
// hand-shaped struct. It also proves the T5 fields are purely additive: a checkpoint
// written before they existed still parses.
func TestRunBFixture_LoadsThroughTheRealCheckpointPath(t *testing.T) {
	cp, err := loadCheckpoint(runBCheckpointPath)
	require.NoError(t, err, "a pre-epic checkpoint must still load — the new fields are additive")
	require.NotNil(t, cp)
	assert.Equal(t, "standard-v1", cp.Suite)
	assert.Equal(t, []string{"brad=qwen3.8-max=brad", "kai=kimi-k3=kai"}, cp.Roster,
		"the roster records the CONFIGURED panel, which is why a mid-run failover does not invalidate a resume")
}
