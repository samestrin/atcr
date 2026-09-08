package debate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/scorecard"
	reclib "github.com/samestrin/atcr/reconcile"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJudgeRulingClearingTruncationReachesTheScorecard is the cross-stage guard
// for the desync between the two artifacts that encode the truncation fact.
//
// applyRulings clears Verification.Truncated on a judge ruling: the recorded
// verdict is now the judge's, produced from the judge's own read, so carrying
// the original skeptic's caveat onto it would attach "answered from a truncated
// read" to the wrong agent's answer. debate.go deliberately does NOT recompute
// the verify stage's verification.json — it is a point-in-time audit artifact —
// so its trippedBudgets entry survives the ruling.
//
// The two stages are owned by different packages and neither test suite alone
// can see the disagreement: debate asserts the flag is cleared, scorecard
// asserts a rate. This test spans the seam, holding the exact on-disk pair the
// pipeline leaves behind and asserting the score follows the artifact report.md
// renders from.
func TestJudgeRulingClearingTruncationReachesTheScorecard(t *testing.T) {
	reviewDir := t.TempDir()
	reconDir := filepath.Join(reviewDir, "reconciled")
	require.NoError(t, os.MkdirAll(reconDir, 0o755))

	// The verify stage's output: a confirmed verdict reached from a shortened read.
	findings := []reconcile.JSONFinding{{
		File:      "a.go",
		Line:      1,
		Problem:   "p1",
		Reviewers: []string{"bruce"},
		Verification: &reclib.Verification{
			Verdict:   reclib.VerdictConfirmed,
			Skeptic:   "bruce",
			Truncated: true,
		},
	}, {
		File:      "b.go",
		Line:      2,
		Problem:   "p2",
		Reviewers: []string{"bruce"},
		Verification: &reclib.Verification{
			Verdict: reclib.VerdictRefuted,
			Skeptic: "bruce",
		},
	}}

	// The debate stage rules on a.go from the judge's own read.
	applyRulings(findings, map[FindingKey]ruleApply{
		{File: "a.go", Line: 1, Problem: "p1"}: {
			verdict:  reclib.VerdictConfirmed,
			survived: true,
			judge:    "greta",
		},
	})
	require.NotNil(t, findings[0].Verification)
	require.False(t, findings[0].Verification.Truncated,
		"precondition: the ruling clears the caveat — this is what report.md then renders")

	path, data, err := computeFindingsBytes(reviewDir, findings)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	// The verify snapshot is left exactly as debate.go leaves it: stale, still
	// recording the trip that produced the pre-ruling verdict.
	verPath := filepath.Join(reconDir, "verification.json")
	require.NoError(t, os.WriteFile(verPath, []byte(`{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","trippedBudgets":["tool_budget_bytes"]},
		{"file":"b.go","line":2,"problem":"p2","verdict":"refuted","trippedBudgets":[]}
	]}`), 0o600))

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
