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
	cleared := applyRulings(findings, judgeRulingOnA())
	require.False(t, findings[0].Verification.Truncated, "precondition: the ruling cleared the caveat")

	path, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
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
	path, data, err := syncVerificationTruncation(reviewDir, ruledFindings(), nil, nil)
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
	cleared := applyRulings(findings, judgeRulingOnA())

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
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
	cleared := applyRulings(findings, judgeRulingOnA())

	path, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
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
	cleared := applyRulings(findings, judgeRulingOnA())

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
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
	cleared := applyRulings(findings, overturnedRulingOnA())
	require.Equal(t, reclib.VerdictRefuted, findings[0].Verification.Verdict,
		"precondition: the judge overturned the skeptic's confirmed")
	require.False(t, findings[0].Verification.Truncated,
		"precondition: the ruling cleared the caveat, so the sync owes a rewrite")

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
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
	cleared := applyRulings(findings, overturnedRulingOnA())

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
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
	cleared := applyRulings(findings, judgeRulingOnA())

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
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

	path, data, err := syncVerificationTruncation(reviewDir, findings, nil, nil)
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
			cleared := applyRulings(findings, judgeRulingOnA())

			path, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
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
	cleared := applyRulings(findings, judgeRulingOnA())

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
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
	cleared := applyRulings(findings, badRuling)
	require.True(t, findings[0].Verification.Truncated,
		"precondition: applyRulings rejected the out-of-enum verdict, so the caveat still describes the standing verdict")

	path, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
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

	bak, err := os.ReadFile(verPath + ".debate.bak")
	require.NoError(t, err, "debate rewrote verification.json, so the state it replaced must be recoverable")
	assert.Equal(t, body, string(bak), "the snapshot is the PRE-debate bytes, verbatim")
}

// TestRunDebate_TakesNoSnapshotWhenItRewritesNothing keeps the snapshot paired
// with an actual rewrite. A snapshot written by a run that changed nothing would
// overwrite the genuinely-prior state kept from the last run that did.
//
// It asserts on debateBakSuffix, not ".bak". The two are not interchangeable and
// checking the wrong one pins nothing: runDebate writes only
// verification.json.debate.bak, so os.IsNotExist on verification.json.bak is true
// by construction for every possible implementation of this stage. The whole
// package stayed green with the snapshot hoisted out of the `if verBytes != nil`
// pairing — the exact regression this test names — while that assertion stood.
func TestRunDebate_TakesNoSnapshotWhenItRewritesNothing(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{truncatedSplitFinding()})
	verPath := writeVerificationFixture(t, dir, `{"findings":[
		{"file":"a.go","line":10,"problem":"nil deref","verdict":"confirmed","skeptic":"bob","trippedBudgets":[]}
	]}`)
	// internal/verify's one backed-up generation. Seeding it is what makes the
	// two-names invariant testable here: a run that rewrites nothing owes no
	// snapshot under EITHER name, and this is the name that is not debate's to
	// spend even when it does.
	preVerify := `{"findings":[{"file":"a.go","line":10,"problem":"nil deref","verdict":"unverifiable","skeptic":"bob","trippedBudgets":[]}]}`
	require.NoError(t, os.WriteFile(verPath+".bak", []byte(preVerify), 0o600))

	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `{"outcome":"overturn","reasoning":"false positive"}`},
	}}
	_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)

	_, err = os.Stat(verPath + debateBakSuffix)
	assert.True(t, os.IsNotExist(err),
		"no tool_budget_bytes entry means no rewrite, and a snapshot of an unchanged file would clobber the generation kept from the last run that did")

	bak, rerr := os.ReadFile(verPath + ".bak")
	require.NoError(t, rerr)
	assert.Equal(t, preVerify, string(bak),
		"verify's one backed-up generation is not debate's to spend, under this name or any other")
}

// TestSyncVerificationTruncation_LeavesARuledDeclaredBudgetVoidingAlone is the
// ruled counterpart of TestSyncVerificationTruncation_LeavesADeclaredBudgetVoidingAlone,
// and the case that guard could not reach.
//
// applyRulings force-clears Verification.Truncated on every ruling it applies
// (emit.go: the recorded verdict is the judge's, produced from the judge's own
// read). syncVerificationTruncation then read that POST-apply flag, so
// `!Truncated` was true by construction for every ruled finding — the gate
// admitted findings whose caveat no ruling had ever cleared.
//
// internal/verify's voiding path (invoke.go) records
// Verification{Verdict: unverifiable, TrippedBudgets: ["tool_budget_bytes"]} for a
// verdict a DECLARED ceiling overruled, and never sets Truncated. Radar tiering is
// verdict-independent, so such a finding can be debated on a severity split. The
// ruling then deleted the only record of WHY the verdict was voided AND overwrote
// the verdict itself — and since envelope.go can only produce confirmed/refuted,
// the flip is one-directional. An all-unverifiable file becomes
// not-all-unverifiable, which silently disables reconcile's all-unverifiable
// safety gate.
func TestSyncVerificationTruncation_LeavesARuledDeclaredBudgetVoidingAlone(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"bruce","trippedBudgets":["tool_budget_bytes"]},
		{"file":"c.go","line":3,"problem":"p3","verdict":"unverifiable","skeptic":"bruce","trippedBudgets":["tool_budget_bytes"]}
	]}`)

	findings := append(ruledFindings(), reconcile.JSONFinding{
		// The verify voiding path's exact shape: a DECLARED ceiling overruled the
		// verdict. Truncated is false because the answer was thrown out, not merely
		// shortened.
		File: "c.go", Line: 3, Problem: "p3", Reviewers: []string{"bruce"},
		Verification: &reclib.Verification{Verdict: reclib.VerdictUnverifiable, Skeptic: "bruce"},
	})
	rulings := judgeRulingOnA()
	rulings[FindingKey{File: "c.go", Line: 3, Problem: "p3"}] = ruleApply{
		verdict: reclib.VerdictConfirmed, survived: true, judge: "greta",
	}

	cleared := applyRulings(findings, rulings)

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
	require.NoError(t, err)
	require.NotNil(t, data, "a.go carried a real caveat the ruling cleared, so a rewrite is still owed")

	got := parseVerification(t, data)
	assert.Empty(t, got["a.go"], "a.go's caveat described the shortened read the ruling replaced")
	assert.Equal(t, []string{"tool_budget_bytes"}, got["c.go"],
		"c.go's trip is the record of a VOIDED verdict, not of a shortened read — no ruling cleared it")
	assert.Equal(t, "unverifiable", parseVerdicts(t, data)["c.go"],
		"overwriting a voided verdict is what disables reconcile's all-unverifiable gate")
}

// TestDebateRulingLeavesTheAllUnverifiableGateStanding is the gate-level guard
// for the same defect, one stage further out.
//
// internal/reconcile/gate.go returns nil at the FIRST non-unverifiable verdict,
// so a single rewritten record is enough: `atcr reconcile --require-verified`
// then exits 0 with no output over an under-funded roster instead of printing the
// "run atcr doctor" diagnostic. Neither package's own suite sees this seam —
// debate asserts a JSON field, reconcile asserts an error — and the artifact in
// between is the one the gate actually reads.
func TestDebateRulingLeavesTheAllUnverifiableGateStanding(t *testing.T) {
	reviewDir := t.TempDir()
	verPath := writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"c.go","line":3,"problem":"p3","verdict":"unverifiable","skeptic":"bruce","trippedBudgets":["tool_budget_bytes"]},
		{"file":"d.go","line":4,"problem":"p4","verdict":"unverifiable","skeptic":"bruce","trippedBudgets":["tool_budget_bytes"]}
	]}`)
	require.Error(t, reconcile.ValidateRequireVerified(reviewDir),
		"precondition: every verdict is unverifiable, so the gate must refuse to pass silently")

	findings := []reconcile.JSONFinding{{
		File: "c.go", Line: 3, Problem: "p3", Reviewers: []string{"bruce"},
		Verification: &reclib.Verification{Verdict: reclib.VerdictUnverifiable, Skeptic: "bruce"},
	}, {
		File: "d.go", Line: 4, Problem: "p4", Reviewers: []string{"bruce"},
		Verification: &reclib.Verification{Verdict: reclib.VerdictUnverifiable, Skeptic: "bruce"},
	}}
	// A severity split upholds one item. Radar tiering is verdict-independent, so
	// this is an ordinary ruling, not a contrived one.
	rulings := map[FindingKey]ruleApply{
		{File: "c.go", Line: 3, Problem: "p3"}: {
			verdict: reclib.VerdictConfirmed, survived: true, judge: "greta",
		},
	}
	cleared := applyRulings(findings, rulings)

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
	require.NoError(t, err)
	if data != nil {
		require.NoError(t, os.WriteFile(verPath, data, 0o600))
	}

	assert.Error(t, reconcile.ValidateRequireVerified(reviewDir),
		"a debate ruling must not turn an all-unverifiable run into one the gate waves through")
}

// TestSyncVerificationTruncation_AttributesTheRewrittenVerdictToTheJudge pins the
// other half of the record the verdict rewrite touches.
//
// internal/verify/emit_verification.go states this record's contract explicitly:
// "Model names only the skeptics whose verdict produced the recorded outcome" and
// "DurationMs is the wall-clock of the run that produced the verdict", and
// docs/verification.md documents the fields as one coherent audit unit. Rewriting
// rec["verdict"] alone left skeptic/model/reasoning/durationMs as the verify stage
// wrote them, so on an OVERTURN the published record read as a refutation
// justified by the confirming argument it replaced, credited to a model that never
// produced it. reconciled/debate.json holds the judge's real reasoning, and nothing
// in this file pointed at it.
func TestSyncVerificationTruncation_AttributesTheRewrittenVerdictToTheJudge(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"otto",
		 "model":"claude-sonnet-4-6","reasoning":"read token.go:42 - jwt.Parse is called without Verify",
		 "durationMs":1840,"trippedBudgets":["tool_budget_bytes"]}
	]}`)

	findings := ruledFindings()
	rulings := map[FindingKey]ruleApply{
		{File: "a.go", Line: 1, Problem: "p1"}: {
			verdict: reclib.VerdictRefuted, survived: false, judge: "greta",
			reasoning: "the call site guards the parse; the skeptic read the wrong overload",
		},
	}
	cleared := applyRulings(findings, rulings)

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
	require.NoError(t, err)
	require.NotNil(t, data)

	rec := parseRecord(t, data, "a.go")
	require.Equal(t, reclib.VerdictRefuted, rec["verdict"], "precondition: the judge overturned the skeptic")
	assert.Equal(t, "greta", rec["debateJudge"],
		"a record whose verdict a judge produced must name that judge, or it mis-attributes the outcome to the skeptic beside it")
	assert.Equal(t, "the call site guards the parse; the skeptic read the wrong overload", rec["debateReasoning"],
		"the reasoning field beside it still argues for the verdict the judge replaced — the judge's own must be reachable from this file")
}

// TestSyncVerificationTruncation_LeavesAnUnrewrittenRecordUnattributed keeps the
// attribution paired with an actual verdict rewrite. A record this call did not
// rewrite still holds the verify stage's own coherent verdict+provenance unit, and
// stamping a judge onto it would be the mis-attribution in reverse.
func TestSyncVerificationTruncation_LeavesAnUnrewrittenRecordUnattributed(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"otto","trippedBudgets":["tool_budget_bytes"]},
		{"file":"b.go","line":2,"problem":"p2","verdict":"refuted","skeptic":"otto","trippedBudgets":["max_turns"]}
	]}`)

	findings := ruledFindings()
	findings[1].Verification.Truncated = true
	rulings := judgeRulingOnA()
	rulings[FindingKey{File: "b.go", Line: 2, Problem: "p2"}] = ruleApply{
		verdict: reclib.VerdictRefuted, survived: true, judge: "greta", reasoning: "stands",
	}
	cleared := applyRulings(findings, rulings)

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
	require.NoError(t, err)
	require.NotNil(t, data)

	b := parseRecord(t, data, "b.go")
	assert.Equal(t, []any{"max_turns"}, b["trippedBudgets"],
		"b.go carried no tool-bytes entry, so this call dropped nothing on it")
	assert.Nil(t, b["debateJudge"],
		"no verdict was rewritten on b.go — attributing its untouched verify record to a judge is the same mis-attribution in reverse")
}

// parseRecord returns the raw record for one file, so a test can assert on fields
// the typed helpers above deliberately do not carry.
func parseRecord(t *testing.T, data []byte, file string) map[string]any {
	t.Helper()
	var doc struct {
		Findings []map[string]any `json:"findings"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	for _, r := range doc.Findings {
		if s, _ := r["file"].(string); s == file {
			return r
		}
	}
	t.Fatalf("no record for %q", file)
	return nil
}

// TestRunDebate_SnapshotDoesNotClobberVerifyBackup pins the one generation the
// debate snapshot was silently consuming.
//
// atomicfs.BackupToDotBak is documented as "replacing any existing backup" and
// keeps exactly ONE generation. internal/verify already writes
// verification.json.bak (backupExistingVerification), contracted as "the
// generation the last verify replaced". debate always runs after verify
// (cli/review.go, and standalone `atcr debate` likewise), so a debate snapshot
// under the same name overwrites verify's pre-verify generation with the
// post-verify one, and the pre-verify state becomes unrecoverable.
//
// Two stages backing up one file need two names.
func TestRunDebate_SnapshotDoesNotClobberVerifyBackup(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{truncatedSplitFinding()})
	postVerify := `{"findings":[
		{"file":"a.go","line":10,"problem":"nil deref","verdict":"confirmed","skeptic":"bob","trippedBudgets":["tool_budget_bytes"]}
	]}`
	verPath := writeVerificationFixture(t, dir, postVerify)
	// What internal/verify left behind: the generation the last verify replaced.
	preVerify := `{"findings":[{"file":"a.go","line":10,"problem":"nil deref","verdict":"unverifiable","skeptic":"bob","trippedBudgets":[]}]}`
	require.NoError(t, os.WriteFile(verPath+".bak", []byte(preVerify), 0o600))

	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `{"outcome":"overturn","reasoning":"false positive"}`},
	}}
	_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)

	bak, err := os.ReadFile(verPath + ".bak")
	require.NoError(t, err)
	assert.Equal(t, preVerify, string(bak),
		"verify's one backed-up generation is not debate's to spend")

	debateBak, err := os.ReadFile(verPath + ".debate.bak")
	require.NoError(t, err, "debate rewrote verification.json, so its own prior generation must be recoverable")
	assert.Equal(t, postVerify, string(debateBak), "debate's snapshot is the PRE-debate bytes, verbatim")
}

// TestRunDebate_FailedPublishTakesNoSnapshot pins the sharper sub-case.
//
// The snapshot was taken BEFORE atomicwrite.WriteGroup, and WriteGroup stages
// every entry before renaming any — so a publish that fails leaves
// verification.json untouched while the snapshot beside it has already been
// spent. A backup that records a generation the run never replaced is worse than
// no backup: it looks current and is not.
func TestRunDebate_FailedPublishTakesNoSnapshot(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{truncatedSplitFinding()})
	body := `{"findings":[
		{"file":"a.go","line":10,"problem":"nil deref","verdict":"confirmed","skeptic":"bob","trippedBudgets":["tool_budget_bytes"]}
	]}`
	verPath := writeVerificationFixture(t, dir, body)
	preVerify := `{"findings":[{"file":"a.go","line":10,"problem":"nil deref","verdict":"unverifiable","skeptic":"bob","trippedBudgets":[]}]}`
	require.NoError(t, os.WriteFile(verPath+".bak", []byte(preVerify), 0o600))

	// A directory where debate.json belongs makes WriteGroup's FIRST rename fail,
	// after every temp is staged. That is the window the defect lives in: staging
	// succeeded, so a snapshot taken ahead of the group has already been spent,
	// and then nothing is published. A read-only reconciled/ would not reproduce
	// it — the same permission that stops the group also stops the snapshot.
	recon := filepath.Dir(verPath)
	require.NoError(t, os.MkdirAll(filepath.Join(recon, "debate.json", "occupied"), 0o755))

	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `{"outcome":"overturn","reasoning":"false positive"}`},
	}}
	_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.Error(t, err, "precondition: the publish cannot write into a read-only reconciled/")

	got, rerr := os.ReadFile(verPath)
	require.NoError(t, rerr)
	require.Equal(t, body, string(got), "precondition: nothing was published")

	bak, rerr := os.ReadFile(verPath + ".bak")
	require.NoError(t, rerr)
	assert.Equal(t, preVerify, string(bak), "a failed publish replaced nothing, so it owes no snapshot")
	_, serr := os.Stat(verPath + ".debate.bak")
	assert.True(t, os.IsNotExist(serr), "a snapshot of a generation that was never replaced records a lie")
}

// TestSyncVerificationTruncation_VerdictRewriteIsScopedToTheDroppedRecord is the
// mutation guard for the `if dropped` branch.
//
// That branch is what scopes the verdict rewrite to the records whose
// tool_budget_bytes caveat THIS call dropped. Without it, every record in
// `cleared` has its verdict overwritten whenever ANY record in the file dropped a
// caveat, because `changed` is file-scoped — the verification.json recompute
// debate.go's atomic-group scope note rules out, and a strictly wider version of
// the declared-ceiling defect above.
//
// The whole suite stayed green with the guard mutated to `if dropped || true`,
// because every prior fixture gave the undropped record the same verdict the
// ruling settled on. This one does not: b.go's record says confirmed and the
// judge overturned it to refuted, so an unscoped rewrite is visible.
func TestSyncVerificationTruncation_VerdictRewriteIsScopedToTheDroppedRecord(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"bruce","trippedBudgets":["tool_budget_bytes"]},
		{"file":"b.go","line":2,"problem":"p2","verdict":"confirmed","skeptic":"bruce","trippedBudgets":["max_turns"]}
	]}`)

	findings := ruledFindings()
	// b.go reached its verdict from a shortened read too, so a ruling on it clears
	// a caveat and puts it in `cleared` — but its record carries no tool-bytes
	// entry for this call to drop.
	findings[1].Verification.Verdict = reclib.VerdictConfirmed
	findings[1].Verification.Truncated = true

	rulings := judgeRulingOnA()
	rulings[FindingKey{File: "b.go", Line: 2, Problem: "p2"}] = ruleApply{
		verdict: reclib.VerdictRefuted, survived: false, judge: "greta", reasoning: "false positive",
	}
	cleared := applyRulings(findings, rulings)
	require.Len(t, cleared, 2, "precondition: both findings were ruled and both carried a caveat")

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
	require.NoError(t, err)
	require.NotNil(t, data, "a.go's tool-bytes entry was dropped, so the file is rewritten")

	got := parseVerdicts(t, data)
	assert.Equal(t, "confirmed", got["a.go"], "a.go's caveat was dropped, so its verdict is corrected with it")
	assert.Equal(t, "confirmed", got["b.go"],
		"b.go carried no tool-bytes entry for this call to drop — rewriting its verdict is the file-wide recompute the scope note rules out")
	assert.Nil(t, parseRecord(t, data, "b.go")["debateJudge"],
		"and no judge is attributed to a verdict this call did not write")
}

// TestSyncVerificationTruncation_RestatesTheJudgeOnASecondRuling pins the
// attribution a SECOND debate over one review dir was leaving stale.
//
// Run 1's ruling clears Verification.Truncated and lands debateJudge in
// verification.json. Run 2 rules the same finding again — an overturn leaves
// ChallengeSurvived false, so filterAlreadyDebated does not exclude it — but the
// caveat is already gone, so applyRulings reports nothing cleared and the sync
// used to return no rewrite at all. findings.json then carried run 2's verdict
// while verification.json still named run 1's judge for it: the record credited
// the SUPERSEDED judge with the standing outcome, which is the same
// mis-attribution the judge stamp exists to prevent, one generation later.
func TestSyncVerificationTruncation_RestatesTheJudgeOnASecondRuling(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"otto",
		 "model":"claude-sonnet-4-6","reasoning":"skeptic read token.go:42","durationMs":1840,
		 "trippedBudgets":[],"debateJudge":"greta","debateReasoning":"greta upheld the skeptic"}
	]}`)

	findings := []reconcile.JSONFinding{{
		File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"otto"},
		Verification: &reclib.Verification{
			Verdict: reclib.VerdictConfirmed, Skeptic: "otto", ChallengeSurvived: true,
		},
	}}
	rulings := map[FindingKey]ruleApply{
		{File: "a.go", Line: 1, Problem: "p1"}: {
			verdict: reclib.VerdictRefuted, survived: false, judge: "hank",
			reasoning: "hank re-read the call site and reversed greta",
		},
	}
	cleared := applyRulings(findings, rulings)
	require.Empty(t, cleared, "precondition: run 1 already cleared the caveat, so run 2 clears none")

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, rulings)
	require.NoError(t, err)
	require.NotNil(t, data, "the record still names run 1's judge for a verdict run 2 replaced")

	rec := parseRecord(t, data, "a.go")
	assert.Equal(t, reclib.VerdictRefuted, rec["verdict"],
		"findings.json carries run 2's verdict; the record debate owns must not disagree with it")
	assert.Equal(t, "hank", rec["debateJudge"],
		"the STANDING verdict is hank's — naming greta credits the judge whose ruling was replaced")
	assert.Equal(t, "hank re-read the call site and reversed greta", rec["debateReasoning"],
		"greta's reasoning argues for the verdict hank overturned")
}

// TestSyncVerificationTruncation_DoesNotStampAJudgeOnAVerifyOwnedRecord is the
// scope guard for the restatement above. The restatement keys on an EXISTING
// debateJudge — the marker that says debate already owns this record. A record
// the verify stage alone produced holds a coherent verdict+provenance unit, and a
// ruling that cleared no caveat is exactly the case debate.go's scope note says
// this stage must leave alone (see LeavesARuledDeclaredBudgetVoidingAlone).
func TestSyncVerificationTruncation_DoesNotStampAJudgeOnAVerifyOwnedRecord(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"unverifiable","skeptic":"otto",
		 "trippedBudgets":["tool_budget_bytes"]}
	]}`)

	// A DECLARED ceiling voided the verdict: internal/verify records the trip but
	// never sets Truncated, so this ruling clears no caveat.
	findings := []reconcile.JSONFinding{{
		File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"otto"},
		Verification: &reclib.Verification{Verdict: reclib.VerdictUnverifiable, Skeptic: "otto"},
	}}
	rulings := judgeRulingOnA()
	cleared := applyRulings(findings, rulings)
	require.Empty(t, cleared, "precondition: nothing to clear on a declared-ceiling voiding")

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, rulings)
	require.NoError(t, err)
	assert.Nil(t, data,
		"no debateJudge on the record means verify owns it — the trip records why the verdict was voided and must survive")
}

// writeDebateFixture lands a reconciled/debate.json naming one ruled item, which
// is the on-disk evidence the residue repair below keys on. debate.json is the
// FIRST entry of runDebate's atomic group, so it is on disk in exactly the state
// a rename that failed later leaves behind.
func writeDebateFixture(t *testing.T, reviewDir string, items ...ItemResult) {
	t.Helper()
	require.NoError(t, writeDebateFile(reviewDir, DebateFile{
		SchemaVersion: DebateSchemaVersion, Items: items,
	}))
}

// TestSyncVerificationTruncation_RepairsAPartialWriteResidue pins the reconciling
// pass over a publish that failed part-way.
//
// atomicwrite.WriteGroup stages every entry then renames them in sequence with no
// rollback, so a rename that fails after findings.json lands leaves findings.json
// with Truncated cleared while verification.json keeps its tool_budget_bytes entry
// and its pre-debate verdict. filterAlreadyDebated then excludes the finding from
// a later run, so nothing ever revisits it.
//
// The repair is gated on the ON-DISK verdict. A record reading confirmed or
// refuted beside a tool_budget_bytes entry can only be the derived-ceiling
// exemption — the read was shortened but the answer stood — so a findings.json
// that says the caveat is gone proves a ruling cleared it and the write was lost.
// The judge comes from reconciled/debate.json, which is written earlier in the
// same group and therefore survives the failure that lost this file.
func TestSyncVerificationTruncation_RepairsAPartialWriteResidue(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"otto",
		 "model":"m-x","reasoning":"otto read token.go:42","durationMs":1840,
		 "trippedBudgets":["tool_budget_bytes"]}
	]}`)
	writeDebateFixture(t, reviewDir, ItemResult{
		File: "a.go", Line: 1, Problem: "p1", Kind: "verification_disagreement",
		Outcome: "overturned", Judge: "greta", Reasoning: "greta re-read the call site",
	})

	// findings.json as the lost publish left it: the ruling landed here.
	findings := []reconcile.JSONFinding{{
		File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"otto"},
		Verification: &reclib.Verification{Verdict: reclib.VerdictRefuted, Skeptic: "otto"},
	}}

	// No ruling this run — filterAlreadyDebated is why this finding never comes back.
	_, data, err := syncVerificationTruncation(reviewDir, findings, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, data, "the two artifacts disagree about a caveat findings.json says is gone")

	rec := parseRecord(t, data, "a.go")
	assert.Empty(t, rec["trippedBudgets"],
		"findings.json says the ruling cleared the caveat — the entry that survived the failed rename is the residue")
	assert.Equal(t, reclib.VerdictRefuted, rec["verdict"],
		"dropping the caveat alone returns the finding to the ratio under the verdict the judge replaced")
	assert.Equal(t, "greta", rec["debateJudge"],
		"the judge is recoverable from reconciled/debate.json, which the same failed publish left standing")
	assert.Equal(t, "greta re-read the call site", rec["debateReasoning"],
		"the reasoning beside the record still argues for the verdict the judge overturned")
}

// TestSyncVerificationTruncation_LeavesAnUnverifiableResidueCandidateAlone is the
// scope guard for the repair above, mirroring
// TestSyncVerificationTruncation_LeavesARuledDeclaredBudgetVoidingAlone one
// generation later.
//
// An `unverifiable` verdict beside a tool_budget_bytes entry is the DECLARED-
// ceiling voiding path: internal/verify throws the answer out and records the trip
// as the only account of why. It is indistinguishable on disk from a residue whose
// verdict happened to be unverifiable, so the repair declines both rather than
// deleting a real voiding record. That residual is accepted, not overlooked.
func TestSyncVerificationTruncation_LeavesAnUnverifiableResidueCandidateAlone(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"c.go","line":3,"problem":"p3","verdict":"unverifiable","skeptic":"bruce",
		 "trippedBudgets":["tool_budget_bytes"]}
	]}`)
	writeDebateFixture(t, reviewDir, ItemResult{
		File: "c.go", Line: 3, Problem: "p3", Kind: "severity_split",
		Outcome: "upheld", Judge: "greta", Reasoning: "severity settled",
	})

	findings := []reconcile.JSONFinding{{
		File: "c.go", Line: 3, Problem: "p3", Reviewers: []string{"bruce"},
		Verification: &reclib.Verification{Verdict: reclib.VerdictUnverifiable, Skeptic: "bruce"},
	}}

	_, data, err := syncVerificationTruncation(reviewDir, findings, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, data,
		"the trip is the only record of why a declared ceiling voided this verdict — an unverifiable record is never repaired")
}

// TestSyncVerificationTruncation_ARepairedRecordIsNotRewrittenAgain pins the
// idempotence the reconciling pass needs to be safe to run every time.
//
// The pass draws its candidates from the prior reconciled/debate.json, so a
// finding a debate ruled stays a candidate on every later run. Marking the file
// changed when the record already says what the ruling settled would republish
// verification.json — and mint a fresh verification.json.debate.bak, consuming the
// one snapshot generation that exists — on every subsequent `atcr debate`, with
// nothing to show for it.
func TestSyncVerificationTruncation_ARepairedRecordIsNotRewrittenAgain(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"refuted","skeptic":"otto",
		 "model":"m-x","reasoning":"otto read token.go:42","durationMs":1840,
		 "trippedBudgets":[],"debateJudge":"greta","debateReasoning":"greta re-read the call site"}
	]}`)
	writeDebateFixture(t, reviewDir, ItemResult{
		File: "a.go", Line: 1, Problem: "p1", Kind: "verification_disagreement",
		Outcome: "overturned", Judge: "greta", Reasoning: "greta re-read the call site",
	})

	findings := []reconcile.JSONFinding{{
		File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"otto"},
		Verification: &reclib.Verification{Verdict: reclib.VerdictRefuted, Skeptic: "otto"},
	}}

	_, data, err := syncVerificationTruncation(reviewDir, findings, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, data,
		"the record already carries the settled verdict and its judge — republishing it burns the one snapshot generation for no change")
}

// TestSyncVerificationTruncation_ClearsAStaleWithheldReasonOnTheRuledRecord keeps
// verification.json's two "why is model empty" markers disjoint.
//
// internal/verify stamps modelWithheldReason=verdict_shifted when a re-verify
// finds a prior whose verdict no longer matches — which is exactly what this file
// looks like while a ruling's correction is still owed. Once debate writes the
// settled verdict and names the judge, that reason is answered and superseded:
// debateJudge is the marker for a DELIBERATE withholding, and the contract on
// VerificationResult says the two never co-occur. Leaving both makes the record
// claim its attribution was rejected for a verdict mismatch it no longer has.
func TestSyncVerificationTruncation_ClearsAStaleWithheldReasonOnTheRuledRecord(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"otto",
		 "model":"","reasoning":"otto read token.go:42","durationMs":0,
		 "trippedBudgets":["tool_budget_bytes"],"modelWithheldReason":"verdict_shifted"}
	]}`)

	findings := ruledFindings()
	cleared := applyRulings(findings, judgeRulingOnA())

	_, data, err := syncVerificationTruncation(reviewDir, findings, cleared, nil)
	require.NoError(t, err)
	require.NotNil(t, data)

	rec := parseRecord(t, data, "a.go")
	require.Equal(t, "greta", rec["debateJudge"], "precondition: the ruling now owns this record")
	assert.Nil(t, rec["modelWithheldReason"],
		"debateJudge is the marker for a deliberate withholding — a verdict-mismatch reason beside it describes a mismatch the write just removed")
}

// TestSyncVerificationTruncation_IgnoresAPriorItemThatSettledNothing keeps the
// residue repair's judge honest.
//
// debate.go:457 assigns ir.Judge = cast.Judge.Agent BEFORE the ruling runs, so
// reconciled/debate.json carries a judge on items that applied nothing to
// findings.json: an `unresolved` outcome (judge_halted, unparseable_ruling) and a
// gray-zone item, whose decision is cluster-level and never enters the
// single-finding rulings map (debate.go:218-239). Projecting those back as
// rulings attributes a verdict to an agent that never ruled it — and because
// internal/verify/pipeline.go:434 reads a non-empty debateJudge as "a judge
// produced this verdict", the real skeptic's model and durationMs are then
// withheld on every later re-verify.
//
// priorDebateRulings must therefore mirror the live map's own admission rule,
// not merely require a judge to be present.
func TestSyncVerificationTruncation_IgnoresAPriorItemThatSettledNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		item ItemResult
		why  string
	}{
		{
			name: "unresolved",
			item: ItemResult{
				File: "a.go", Line: 1, Problem: "p1", Kind: "verification_disagreement",
				Outcome: OutcomeUnresolved, Reason: "judge_halted",
				Judge: "greta", Reasoning: "judge halted",
			},
			why: "an unresolved item settles nothing — debate.go:218 skips it before the rulings map is touched",
		},
		{
			name: "gray_zone",
			item: ItemResult{
				File: "a.go", Line: 1, Problem: "p1", Kind: reconcile.KindGrayZone,
				Outcome: OutcomeUphold, ClusterDecision: ClusterSeparate,
				Judge: "greta", Reasoning: "the two findings are distinct",
			},
			why: "a gray-zone ruling is a cluster-level decision — debate.go:220 keeps it out of the per-finding rulings map",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reviewDir := t.TempDir()
			// The residue shape: a surviving verdict beside a tool-bytes entry.
			writeVerificationFixture(t, reviewDir, `{"findings":[
				{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"otto",
				 "model":"m-x","reasoning":"otto read token.go:42","durationMs":1840,
				 "trippedBudgets":["tool_budget_bytes"]}
			]}`)
			writeDebateFixture(t, reviewDir, tc.item)

			findings := []reconcile.JSONFinding{{
				File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"otto"},
				Verification: &reclib.Verification{Verdict: reclib.VerdictRefuted, Skeptic: "otto"},
			}}

			_, data, err := syncVerificationTruncation(reviewDir, findings, nil, nil)
			require.NoError(t, err)
			assert.Nil(t, data, tc.why)
		})
	}
}

// TestSyncVerificationTruncation_LeavesAStandingCaveatAlone pins the skip that
// keeps the pending pass off findings whose caveat is still set.
//
// The pending pass exists to correct records a ruling settled. A finding whose
// Verification.Truncated is still true was settled by nothing: applyRulings is the
// only thing that clears that flag, so its presence proves no ruling applied here
// and none is owed. Without the skip such a finding becomes a pending candidate,
// and a record a PRIOR debate owns is then re-stamped with a verdict this run
// never settled — the file-wide recompute debate.go's atomic-group scope note
// rules out.
func TestSyncVerificationTruncation_LeavesAStandingCaveatAlone(t *testing.T) {
	reviewDir := t.TempDir()
	// A record a prior debate already owns, carrying a verdict findings.json now
	// disagrees with — the exact shape the recordedDebateJudge arm rewrites.
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"otto",
		 "model":"m-x","reasoning":"otto read token.go:42","durationMs":1840,
		 "trippedBudgets":[],"debateJudge":"greta","debateReasoning":"greta upheld the skeptic"}
	]}`)

	findings := []reconcile.JSONFinding{{
		File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"otto"},
		Verification: &reclib.Verification{
			Verdict: reclib.VerdictRefuted, Skeptic: "otto", Truncated: true,
		},
	}}
	rulings := map[FindingKey]ruleApply{
		{File: "a.go", Line: 1, Problem: "p1"}: {
			verdict: reclib.VerdictRefuted, judge: "hank", reasoning: "hank reversed greta",
		},
	}

	// No caveat cleared: this call is handed an empty cleared set precisely because
	// the flag on the finding is still standing.
	_, data, err := syncVerificationTruncation(reviewDir, findings, nil, rulings)
	require.NoError(t, err)
	assert.Nil(t, data,
		"Truncated is still set, so no ruling applied to this finding — correcting a record on the strength of the ruling alone is the recompute the scope note forbids")
}

// TestSyncVerificationTruncation_PrefersThePriorJudgeOverAnUnparseableRuling pins
// the validVerdict check on THIS run's ruling.
//
// applyRulings deliberately declines an out-of-enum verdict, so a ruling can sit
// in the rulings map having settled nothing. Taking its judge anyway would
// attribute the standing verdict — which the PRIOR debate produced — to the agent
// whose ruling this run could not parse. The check is what sends the lookup on to
// reconciled/debate.json, where the judge that actually settled it is recorded.
func TestSyncVerificationTruncation_PrefersThePriorJudgeOverAnUnparseableRuling(t *testing.T) {
	reviewDir := t.TempDir()
	writeVerificationFixture(t, reviewDir, `{"findings":[
		{"file":"a.go","line":1,"problem":"p1","verdict":"confirmed","skeptic":"otto",
		 "model":"m-x","reasoning":"otto read token.go:42","durationMs":1840,
		 "trippedBudgets":[],"debateJudge":"greta","debateReasoning":"greta overturned the skeptic"}
	]}`)
	writeDebateFixture(t, reviewDir, ItemResult{
		File: "a.go", Line: 1, Problem: "p1", Kind: "verification_disagreement",
		Outcome: OutcomeOverturn, Judge: "greta", Reasoning: "greta overturned the skeptic",
	})

	findings := []reconcile.JSONFinding{{
		File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"otto"},
		Verification: &reclib.Verification{Verdict: reclib.VerdictRefuted, Skeptic: "otto"},
	}}
	// This run ruled, but the ruling did not parse into a verdict — applyRulings
	// applied nothing from it, so it settled nothing.
	rulings := map[FindingKey]ruleApply{
		{File: "a.go", Line: 1, Problem: "p1"}: {
			verdict: "not_a_verdict", judge: "hank", reasoning: "hank's ruling was unparseable",
		},
	}

	_, data, err := syncVerificationTruncation(reviewDir, findings, nil, rulings)
	require.NoError(t, err)
	require.NotNil(t, data, "the record still names confirmed for a verdict findings.json now reports as refuted")

	rec := parseRecord(t, data, "a.go")
	assert.Equal(t, reclib.VerdictRefuted, rec["verdict"],
		"the standing verdict is the one findings.json carries")
	assert.Equal(t, "greta", rec["debateJudge"],
		"greta produced the standing verdict — crediting hank names the judge whose ruling settled nothing")
	assert.Equal(t, "greta overturned the skeptic", rec["debateReasoning"],
		"the reasoning must argue for the verdict actually recorded")
}
