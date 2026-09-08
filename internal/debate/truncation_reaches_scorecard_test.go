package debate

import (
	"context"
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

	path, data, err := syncVerificationTruncation(reviewDir, findings, judgeRulingOnA())
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
	path, data, err := syncVerificationTruncation(reviewDir, ruledFindings(), nil)
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

	_, data, err := syncVerificationTruncation(reviewDir, findings, judgeRulingOnA())
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

	path, data, err := syncVerificationTruncation(reviewDir, findings, judgeRulingOnA())
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

	_, data, err := syncVerificationTruncation(reviewDir, findings, judgeRulingOnA())
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

// overturnedRulingOnA is the ruling shape the sync got wrong: the judge read the
// evidence itself and REVERSED the skeptic's confirmed to refuted.
func overturnedRulingOnA() map[FindingKey]ruleApply {
	return map[FindingKey]ruleApply{
		{File: "a.go", Line: 1, Problem: "p1"}: {
			verdict: reclib.VerdictRefuted, survived: false, judge: "greta",
		},
	}
}

// TestSyncVerificationTruncation_CarriesTheRuledVerdictWithTheCaveat pins that
// the two fields describing ONE verdict move together.
//
// Clearing tool_budget_bytes takes the finding out of the score's truncated
// exclusion and puts it back into survived_skeptic_rate. The verdict it is then
// counted under comes from the SAME file — and runDebate deliberately never
// rewrites it (debate.go's atomic-group scope note). So on an OVERTURN the sync
// used to restore a finding to the ratio under the pre-debate verdict: the judge
// refuted it, and the score credited the reviewer for a confirm.
//
// Before the caveat was cleared at all, such a finding was excluded from both
// numerator and denominator, so this is a regression the sync itself introduced.
func TestSyncVerificationTruncation_CarriesTheRuledVerdictWithTheCaveat(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, staleVerification)

	findings := ruledFindings()
	applyRulings(findings, overturnedRulingOnA())
	require.Equal(t, reclib.VerdictRefuted, findings[0].Verification.Verdict,
		"precondition: the judge overturned the skeptic's confirmed")
	require.False(t, findings[0].Verification.Truncated,
		"precondition: the ruling cleared the caveat, so the sync owes a rewrite")

	_, data, err := syncVerificationTruncation(reviewDir, findings, overturnedRulingOnA())
	require.NoError(t, err)
	require.NotNil(t, data)

	assert.Equal(t, reclib.VerdictRefuted, parseVerdicts(t, data)["a.go"],
		"the caveat and the verdict describe the same verdict — clearing one while leaving the other stale hands the score a verdict the judge replaced")
}

// TestOverturnedRulingDoesNotCreditTheReviewer is the cross-stage half: the same
// artifacts the pipeline leaves on disk, read by the scorecard that consumes them.
func TestOverturnedRulingDoesNotCreditTheReviewer(t *testing.T) {
	reviewDir := t.TempDir()
	verPath := writeVerificationFixture(t, reviewDir, staleVerification)

	findings := ruledFindings()
	applyRulings(findings, overturnedRulingOnA())

	_, data, err := syncVerificationTruncation(reviewDir, findings, overturnedRulingOnA())
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
	assert.InDelta(t, 0.0, *bruce.SurvivedSkepticRate, 1e-9,
		"the judge refuted both findings — crediting either is the stale-verdict leak")
}

// parseVerdicts maps each finding's file to its recorded verdict.
func parseVerdicts(t *testing.T, data []byte) map[string]string {
	t.Helper()
	var vf struct {
		Findings []struct {
			File    string `json:"file"`
			Verdict string `json:"verdict"`
		} `json:"findings"`
	}
	require.NoError(t, json.Unmarshal(data, &vf))
	out := map[string]string{}
	for _, f := range vf.Findings {
		out[f.File] = f.Verdict
	}
	return out
}

// TestSyncVerificationTruncation_LeavesADeclaredBudgetVoidingAlone is the
// containment guard the `cleared` set was missing.
//
// `cleared` admitted every finding with a non-nil Verification whose Truncated
// was false — which is most of them, ruled or not. internal/verify's voiding path
// (invoke.go) records Verification{Verdict: unverifiable} for a verdict a DECLARED
// tool_budget_bytes ceiling overruled, and never sets Truncated, so such a finding
// walked straight into `cleared`. Its tool_budget_bytes entry — the only record of
// WHY that verdict was voided — was then deleted from verification.json by a
// debate run that never ruled on it. There is no verification.json.bak on this
// path, so the deletion is unrecoverable.
//
// This contradicted the function's own doc ("on exactly the findings that were
// ruled") and debate.go's atomic-group note ("one entry, on ruled findings only").
func TestSyncVerificationTruncation_LeavesADeclaredBudgetVoidingAlone(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"bruce","trippedBudgets":["tool_budget_bytes"]},
		{"file":"c.go","line":3,"problem":"p3","verdict":"unverifiable","skeptic":"bruce","trippedBudgets":["tool_budget_bytes"]}
	]}`)

	findings := append(ruledFindings(), reconcile.JSONFinding{
		// The verify voiding path's exact shape: a verdict a DECLARED ceiling
		// overruled. Truncated is false because the read was not merely shortened —
		// the answer was thrown out.
		File: "c.go", Line: 3, Problem: "p3", Reviewers: []string{"bruce"},
		Verification: &reclib.Verification{Verdict: reclib.VerdictUnverifiable, Skeptic: "bruce"},
	})
	applyRulings(findings, judgeRulingOnA())

	_, data, err := syncVerificationTruncation(reviewDir, findings, judgeRulingOnA())
	require.NoError(t, err)
	require.NotNil(t, data, "a.go WAS ruled, so a rewrite is still owed")

	got := parseVerification(t, data)
	assert.Empty(t, got["a.go"], "a.go was ruled — its caveat goes")
	assert.Equal(t, []string{"tool_budget_bytes"}, got["c.go"],
		"c.go was never ruled — deleting the only record of why its verdict was voided is not this stage's to do")
}

// TestSyncVerificationTruncation_NoRulingsRewritesNothing pins the early exit.
// debate.go calls this unconditionally, including on a run that ruled nothing at
// all; such a run has no correction to make by construction.
func TestSyncVerificationTruncation_NoRulingsRewritesNothing(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"c.go","line":3,"problem":"p3","verdict":"unverifiable","skeptic":"bruce","trippedBudgets":["tool_budget_bytes"]}
	]}`)

	findings := []reconcile.JSONFinding{{
		File: "c.go", Line: 3, Problem: "p3", Reviewers: []string{"bruce"},
		Verification: &reclib.Verification{Verdict: reclib.VerdictUnverifiable, Skeptic: "bruce"},
	}}

	path, data, err := syncVerificationTruncation(reviewDir, findings, nil)
	require.NoError(t, err)
	assert.Nil(t, data, "no ruling was applied, so there is nothing this stage may correct")
	assert.Empty(t, path)
}

// truncatedSplitFinding is splitFinding() carrying the verify-stage shape this
// sync exists for: a verdict reached from a shortened read.
func truncatedSplitFinding() reconcile.JSONFinding {
	f := splitFinding()
	f.Verification = &reclib.Verification{
		Verdict: reclib.VerdictConfirmed, Skeptic: "bob", Truncated: true,
	}
	return f
}

// TestRunDebate_PublishesTheCorrectedVerificationSnapshot is the wiring proof.
//
// syncVerificationTruncation is unit-tested in isolation everywhere above — the
// tests call it directly and inspect its return value. None of them prove
// runDebate joins that output to the artifacts it actually writes, which is the
// entire user-visible deliverable: disabling the append in debate.go left
// internal/debate, cli AND internal/mcp green.
//
// This drives the real entry point and reads the file off disk afterwards.
func TestRunDebate_PublishesTheCorrectedVerificationSnapshot(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{truncatedSplitFinding()})
	verPath := writeVerificationFixture(t, dir, `{"findings":[
		{"file":"a.go","line":10,"problem":"nil deref","verdict":"confirmed","skeptic":"bob","trippedBudgets":["max_turns","tool_budget_bytes"]}
	]}`)

	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `{"outcome":"overturn","reasoning":"false positive"}`},
	}}

	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	require.Equal(t, 1, res.Overturned, "precondition: the judge ruled, so a correction is owed")

	// findings.json is the half already covered; assert it so a failure below is
	// unambiguously about the OTHER artifact rather than about the ruling.
	f := readFindings(t, dir)
	require.Len(t, f, 1)
	require.False(t, f[0].Verification.Truncated, "the ruling cleared the caveat")

	onDisk, err := os.ReadFile(verPath)
	require.NoError(t, err)

	assert.Equal(t, []string{"max_turns"}, parseVerification(t, onDisk)["a.go"],
		"the corrected snapshot must reach DISK — a return value runDebate never publishes fixes nothing")
	assert.Equal(t, reclib.VerdictRefuted, parseVerdicts(t, onDisk)["a.go"],
		"and it must carry the judge's verdict, since the score reads it from this same record")
}

// TestRunDebate_LeavesTheVerificationSnapshotAloneWithNoRuling is the negative
// half of the wiring: runDebate calls the sync unconditionally, so a run that
// rules nothing must still leave the snapshot byte-identical.
func TestRunDebate_LeavesTheVerificationSnapshotAloneWithNoRuling(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{truncatedSplitFinding()})
	body := `{"findings":[
		{"file":"a.go","line":10,"problem":"nil deref","verdict":"confirmed","skeptic":"bob","trippedBudgets":["tool_budget_bytes"]}
	]}`
	verPath := writeVerificationFixture(t, dir, body)

	// The judge halts: an item is selected but no ruling is applied.
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `not a parseable ruling`},
	}}

	_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)

	onDisk, err := os.ReadFile(verPath)
	require.NoError(t, err)
	assert.Equal(t, body, string(onDisk),
		"nothing was ruled, so the verify snapshot must not be rewritten at all — not even reformatted")
}

// TestSyncVerificationTruncation_BestEffortFallbacks covers the four exits the
// function's doc calls load-bearing and nothing exercised.
//
// The contract is stated as a guarantee — "an absent or unparseable snapshot
// yields no rewrite rather than an error, so a debate over a review that was
// never verified still completes" — and a corrupt verification.json is exactly
// the state a debate run has to survive. All four exits returned ("", nil, nil)
// on paper only.
func TestSyncVerificationTruncation_BestEffortFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"unparseable snapshot", `{"findings": [`},
		{"not JSON at all", "this is not json"},
		{"top level is not an object", `["a","b"]`},
		{"findings key absent", `{"verifiedAt":"2026-06-01T00:00:00Z"}`},
		{"findings is not an array", `{"findings":{"a.go":1}}`},
		{"findings holds a non-object", `{"findings":["a.go",42,null]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reviewDir := t.TempDir()
			writeVerificationFixture(t, reviewDir, tc.body)

			findings := ruledFindings()
			applyRulings(findings, judgeRulingOnA())

			path, data, err := syncVerificationTruncation(reviewDir, findings, judgeRulingOnA())
			assert.NoError(t, err, "a debate over an unusable snapshot must still complete")
			assert.Nil(t, data, "nothing legible to correct means no rewrite is proposed")
			assert.Empty(t, path)
		})
	}
}

// TestSyncVerificationTruncation_NonObjectItemsAreSkippedNotFatal pins that a
// malformed entry does not cost the correction owed to its well-formed
// neighbours. The skip is a `continue`, not a bail-out, and only a snapshot
// mixing both shapes can tell the two apart.
func TestSyncVerificationTruncation_NonObjectItemsAreSkippedNotFatal(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		"a stray string",
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"bruce","trippedBudgets":["tool_budget_bytes"]}
	]}`)

	findings := ruledFindings()
	applyRulings(findings, judgeRulingOnA())

	_, data, err := syncVerificationTruncation(reviewDir, findings, judgeRulingOnA())
	require.NoError(t, err)
	require.NotNil(t, data, "the well-formed neighbour is still owed its correction")

	// parseVerification is deliberately strict and cannot read this output: the
	// stray string is PRESERVED in the rewrite (the function only skips it, it
	// never drops unrecognised entries), which is itself part of the contract.
	var doc struct {
		Findings []any `json:"findings"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Len(t, doc.Findings, 2, "the malformed entry is skipped for correction, not deleted from the snapshot")
	assert.Equal(t, "a stray string", doc.Findings[0], "an entry this stage cannot read is left exactly as found")

	rec, ok := doc.Findings[1].(map[string]any)
	require.True(t, ok)
	assert.Empty(t, rec["trippedBudgets"], "the well-formed neighbour still got its correction")
}

// TestSyncVerificationTruncation_RuledButCaveatStillStandsRewritesNothing covers
// the exit between "no rulings" and "a correction is owed".
//
// applyRulings SKIPS a ruling whose verdict is not in the enum — persisting a
// malformed verification block is a contract violation downstream consumers choke
// on — so the finding keeps Truncated=true. The rulings map is non-empty, the
// early exit above does not fire, and nothing was in fact cleared. Rewriting the
// snapshot here would strip a caveat that still stands.
func TestSyncVerificationTruncation_RuledButCaveatStillStandsRewritesNothing(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, staleVerification)

	badRuling := map[FindingKey]ruleApply{
		{File: "a.go", Line: 1, Problem: "p1"}: {verdict: "not-a-verdict", judge: "greta"},
	}
	findings := ruledFindings()
	applyRulings(findings, badRuling)
	require.True(t, findings[0].Verification.Truncated,
		"precondition: applyRulings rejected the out-of-enum verdict, so the caveat still describes the standing verdict")

	path, data, err := syncVerificationTruncation(reviewDir, findings, badRuling)
	require.NoError(t, err)
	assert.Nil(t, data, "the caveat still describes the recorded verdict — there is nothing to correct")
	assert.Empty(t, path)
}

// TestRunDebate_SnapshotsVerificationBeforeRewritingIt pins the recovery path.
//
// internal/verify snapshots verification.json to .bak before every rewrite
// (backupExistingVerification). debate rewrites the same file and did not, so the
// pre-debate state was unrecoverable — and this stage's rewrite is lossier than
// verify's: the generic-map round-trip re-sorts every key, so the file that comes
// back is not byte-comparable with the one verify wrote even where no value
// changed.
func TestRunDebate_SnapshotsVerificationBeforeRewritingIt(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{truncatedSplitFinding()})
	body := `{"findings":[
		{"file":"a.go","line":10,"problem":"nil deref","verdict":"confirmed","skeptic":"bob","trippedBudgets":["tool_budget_bytes"]}
	]}`
	verPath := writeVerificationFixture(t, dir, body)

	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `{"outcome":"overturn","reasoning":"false positive"}`},
	}}
	_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)

	bak, err := os.ReadFile(verPath + ".bak")
	require.NoError(t, err, "debate rewrote verification.json, so the state it replaced must be recoverable")
	assert.Equal(t, body, string(bak), "the snapshot is the PRE-debate bytes, verbatim")
}

// TestRunDebate_TakesNoSnapshotWhenItRewritesNothing keeps the snapshot paired
// with an actual rewrite. A .bak written by a run that changed nothing would
// overwrite the genuinely-prior state kept from the last run that did.
func TestRunDebate_TakesNoSnapshotWhenItRewritesNothing(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{truncatedSplitFinding()})
	verPath := writeVerificationFixture(t, dir, `{"findings":[
		{"file":"a.go","line":10,"problem":"nil deref","verdict":"confirmed","skeptic":"bob","trippedBudgets":[]}
	]}`)

	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `{"outcome":"overturn","reasoning":"false positive"}`},
	}}
	_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)

	_, err = os.Stat(verPath + ".bak")
	assert.True(t, os.IsNotExist(err),
		"no tool_budget_bytes entry means no rewrite, and a snapshot of an unchanged file would clobber a real prior state")
}
