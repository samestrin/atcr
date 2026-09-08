package debate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/scorecard"
	reclib "github.com/samestrin/atcr/reconcile"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const staleVerification = `{"findings":[
	{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"bruce","trippedBudgets":["tool_budget_bytes"]},
	{"file":"b.go","line":2,"problem":"p2","verdict":"refuted","skeptic":"bruce","trippedBudgets":[]}
]}`

// ruledFindings is the verify-stage output for the fixture below: a confirmed
// verdict reached from a shortened read, plus a clean refuted one.
func ruledFindings() []reconcile.JSONFinding {
	return []reconcile.JSONFinding{{
		File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"bruce"},
		Verification: &reclib.Verification{Verdict: reclib.VerdictConfirmed, Skeptic: "bruce", Truncated: true},
	}, {
		File: "b.go", Line: 2, Problem: "p2", Reviewers: []string{"bruce"},
		Verification: &reclib.Verification{Verdict: reclib.VerdictRefuted, Skeptic: "bruce"},
	}}
}

func judgeRulingOnA() map[FindingKey]ruleApply {
	return map[FindingKey]ruleApply{
		{File: "a.go", Line: 1, Problem: "p1"}: {
			verdict: reclib.VerdictConfirmed, survived: true, judge: "greta",
		},
	}
}

// TestSyncVerificationTruncation_ClearsTheTripTheRulingInvalidated is the fix for
// the two artifacts that encoded the same fact while only one was updated.
//
// applyRulings clears Verification.Truncated on a judge ruling: the recorded
// verdict is the judge's, produced from the judge's own read, so carrying the
// original skeptic's caveat onto it would attach "answered from a truncated read"
// to the wrong agent's answer. verification.json kept its tool_budget_bytes
// entry, so report.md showed a ruling with no caveat while the scorecard still
// dropped the finding from survived_skeptic_rate.
//
// verification.json is the artifact that has to change, not findings.json:
// EmitForReconcile runs after RunReconcile, which rebuilds findings.json from
// sources/ and strips every verification block, while verification.json is never
// recomputed. It is therefore the only record of a verdict still standing when
// the score is computed.
func TestSyncVerificationTruncation_ClearsTheTripTheRulingInvalidated(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, staleVerification)

	findings := ruledFindings()
	applyRulings(findings, judgeRulingOnA())
	require.False(t, findings[0].Verification.Truncated, "precondition: the ruling cleared the caveat")

	path, data, err := syncVerificationTruncation(reviewDir, findings)
	require.NoError(t, err)
	require.NotNil(t, data, "the trip the ruling invalidated is still on disk, so a rewrite is owed")
	require.Equal(t, filepath.Join(reviewDir, "reconciled", "verification.json"), path)

	got := parseVerification(t, data)
	assert.Empty(t, got["a.go"], "the ruling was reached from the judge's own read — the trip that described the skeptic's must go")
	assert.Empty(t, got["b.go"], "untouched, and it had none to begin with")
}

// TestSyncVerificationTruncation_LeavesAnUnruledFindingAlone is the scope guard.
// This is emphatically NOT a recompute of verification.json — debate.go states
// that snapshot is a point-in-time verify audit artifact. Exactly one entry is
// cleared, on exactly the findings a ruling changed.
func TestSyncVerificationTruncation_LeavesAnUnruledFindingAlone(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, staleVerification)

	// No ruling applied at all: nothing about how any verdict was reached changed.
	path, data, err := syncVerificationTruncation(reviewDir, ruledFindings())
	require.NoError(t, err)
	assert.Nil(t, data, "no ruling cleared a caveat, so verification.json is not rewritten at all")
	assert.Empty(t, path)
}

// TestSyncVerificationTruncation_KeepsOtherTrippedBudgets pins that only the
// tool-bytes entry is touched. max_turns and timeout trips describe how the run
// halted, which a ruling on the finding does not change.
func TestSyncVerificationTruncation_KeepsOtherTrippedBudgets(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"bruce","trippedBudgets":["max_turns","tool_budget_bytes","timeout_secs"]}
	]}`)

	findings := ruledFindings()
	applyRulings(findings, judgeRulingOnA())

	_, data, err := syncVerificationTruncation(reviewDir, findings)
	require.NoError(t, err)
	require.NotNil(t, data)

	assert.Equal(t, []string{"max_turns", "timeout_secs"}, parseVerification(t, data)["a.go"],
		"only the tool-bytes entry described the shortened read the ruling replaced")
}

// TestSyncVerificationTruncation_MissingFileIsNotAnError keeps the sync
// best-effort in the same spirit as the rest of the stage: a debate run over a
// review with no verify snapshot has nothing to correct and must not fail.
func TestSyncVerificationTruncation_MissingFileIsNotAnError(t *testing.T) {
	reviewDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(reviewDir, "reconciled"), 0o755))

	findings := ruledFindings()
	applyRulings(findings, judgeRulingOnA())

	path, data, err := syncVerificationTruncation(reviewDir, findings)
	assert.NoError(t, err)
	assert.Nil(t, data)
	assert.Empty(t, path)
}

// TestJudgeRulingClearingTruncationReachesTheScorecard is the cross-stage guard:
// it holds the exact pair of artifacts the pipeline leaves on disk after a
// ruling and asserts the score follows.
//
// Neither package's own suite can see this seam — debate asserts a flag,
// scorecard asserts a rate — and the artifact in between is the one the score
// actually reads.
func TestJudgeRulingClearingTruncationReachesTheScorecard(t *testing.T) {
	reviewDir := t.TempDir()
	verPath := writeVerificationFixture(t, reviewDir, staleVerification)

	findings := ruledFindings()
	applyRulings(findings, judgeRulingOnA())

	_, data, err := syncVerificationTruncation(reviewDir, findings)
	require.NoError(t, err)
	require.NotNil(t, data)
	require.NoError(t, os.WriteFile(verPath, data, 0o600))

	storeDir := t.TempDir()
	require.NoError(t, scorecard.Emit(scorecard.EmitInput{
		RunID: "2026-06-01T00:00:00Z-run",
		Findings: []scorecard.Finding{
			{File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"bruce"}},
			{File: "b.go", Line: 2, Problem: "p2", Reviewers: []string{"bruce"}},
		},
		Reviewers:        map[string]scorecard.ReviewerMeta{"bruce": {Model: "m"}},
		VerificationPath: verPath,
	}, scorecard.EmitOpts{Dir: storeDir}))

	recs, err := scorecard.ReadRecords(filepath.Join(storeDir, "2026-06.jsonl"), scorecard.ReadOpts{})
	require.NoError(t, err)

	var bruce *scorecard.Record
	for i := range recs {
		if recs[i].Reviewer == "bruce" && recs[i].RecordType == scorecard.RecordTypeReviewer {
			bruce = &recs[i]
		}
	}
	require.NotNil(t, bruce, "no reviewer record for bruce")
	require.NotNil(t, bruce.SurvivedSkepticRate)
	assert.InDelta(t, 0.5, *bruce.SurvivedSkepticRate, 1e-9,
		"report.md shows the judge's ruling with no truncated caveat, so the score must count it too — 1 confirmed / (1 confirmed + 1 refuted)")
}

func writeVerificationFixture(t *testing.T, reviewDir, body string) string {
	t.Helper()
	dir := filepath.Join(reviewDir, "reconciled")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	p := filepath.Join(dir, "verification.json")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

// parseVerification maps each finding's file to its trippedBudgets.
func parseVerification(t *testing.T, data []byte) map[string][]string {
	t.Helper()
	var vf struct {
		Findings []struct {
			File           string   `json:"file"`
			TrippedBudgets []string `json:"trippedBudgets"`
		} `json:"findings"`
	}
	require.NoError(t, json.Unmarshal(data, &vf))
	out := map[string][]string{}
	for _, f := range vf.Findings {
		out[f.File] = f.TrippedBudgets
	}
	return out
}
