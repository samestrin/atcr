package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coverageFor returns the ReviewerCoverage row matching (model, persona), or fails
// the test naming what was actually present — a missing row is otherwise reported as
// a nil-pointer panic several lines later.
func coverageFor(t *testing.T, rr *benchmark.RunResult, model, persona string) benchmark.ReviewerCoverage {
	t.Helper()
	var got []string
	for _, c := range rr.Coverage {
		if c.Model == model && c.Persona == persona {
			return c
		}
		got = append(got, c.Model+"/"+c.Persona)
	}
	require.FailNowf(t, "coverage row not found", "want %s/%s, have %v", model, persona, got)
	return benchmark.ReviewerCoverage{}
}

// The run-result must NAME the cases behind every reviewer row, not merely count
// them. `runs` is a count, and case difficulty on this suite varies enormously, so a
// recall computed over one subset is not comparable to one computed over a different
// subset of the same size. Splitting rows by realized model makes uneven coverage the
// normal case, which is what turns this from a nicety into a requirement.
func TestExecuteBenchmarkRun_RecordsSuiteCaseIDsAndCoverage(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"}, [3]string{"kai", "m-kai", "kai"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)

	assert.Equal(t, []string{"case-01-nil-deref", "case-02-sql-injection"}, rr.SuiteCaseIDs,
		"the suite's case-id list is recorded once, in manifest order — it is the denominator every row is measured against")

	require.Len(t, rr.Coverage, 2, "one coverage row per reviewer row")
	for _, model := range []string{"m-greta", "m-kai"} {
		cov := coverageFor(t, rr, model, map[string]string{"m-greta": "greta", "m-kai": "kai"}[model])
		assert.Equal(t, []string{"case-01-nil-deref", "case-02-sql-injection"}, cov.CaseIDs,
			"a reviewer that served every case covers every case")
	}
}

// Coverage rows are ordered identically to the reviewer rows they describe, so a
// consumer can join them positionally as well as by identity — and so the run-result
// stays byte-identical across runs.
func TestExecuteBenchmarkRun_CoverageOrderMatchesReviewerOrder(t *testing.T) {
	cfg := benchCfg([3]string{"kai", "m-kai", "kai"}, [3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)
	require.Len(t, rr.Coverage, len(rr.Reviewers))
	for i := range rr.Reviewers {
		assert.Equal(t, rr.Reviewers[i].Model, rr.Coverage[i].Model, "row %d identity must line up", i)
		assert.Equal(t, rr.Reviewers[i].Persona, rr.Coverage[i].Persona, "row %d identity must line up", i)
		assert.Equal(t, rr.Reviewers[i].Runs, len(rr.Coverage[i].CaseIDs),
			"row %d: the coverage set must have exactly as many entries as `runs` claims", i)
	}
}

// A resumed run reports the SAME coverage as an uninterrupted one. Coverage is
// accumulated on the shared fold path, so replay reconstructs it rather than
// recomputing it from a different source — the property that keeps a same-model
// resume honest (AC4). The failover boundary itself — replayed entries folding
// under model A while live cases fold under model B — is covered by
// TestExecuteBenchmarkRun_ResumeAcrossFailoverBoundarySplitsCoverage.
func TestExecuteBenchmarkRun_ResumeReportsSameCoverage(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := t.TempDir() + "/ckpt.json"

	baseline, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)
	require.NotEmpty(t, baseline.Coverage, "the baseline must actually carry coverage or the comparison is vacuous")

	// Fail after case 0 so the checkpoint holds a partial run, then resume it.
	_, err = executeBenchmarkRun(context.Background(), cfg, &failAfterCompleter{ok: 1}, suiteValidPath, gen, path)
	require.Error(t, err)
	resumed, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, path)
	require.NoError(t, err)

	assert.Equal(t, mustMarshal(t, baseline.Coverage), mustMarshal(t, resumed.Coverage),
		"a resumed run's coverage is identical to an uninterrupted run's")
	assert.Equal(t, baseline.SuiteCaseIDs, resumed.SuiteCaseIDs)
}

// A run-result produced before coverage existed unmarshals with both fields nil, and
// a coverage-free RunResult marshals without the keys at all. This is what lets
// `benchmark export` distinguish "measured and short" from "never measured" rather
// than reading an absent field as a violation.
func TestRunResult_CoverageAbsentWhenUnmeasured(t *testing.T) {
	var rr benchmark.RunResult
	require.NoError(t, json.Unmarshal([]byte(
		`{"suite":"mini","suite_version":"1.0.0","generated_at":"2026-06-24T12:00:00Z","reviewers":[]}`), &rr))
	assert.Nil(t, rr.SuiteCaseIDs, "a pre-coverage run-result reports unmeasured, not empty")
	assert.Nil(t, rr.Coverage)

	raw, err := json.Marshal(rr)
	require.NoError(t, err)
	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.NotContains(t, decoded, "suite_case_ids", "nil coverage drops the key entirely — not null, not []")
	assert.NotContains(t, decoded, "reviewer_coverage")
}

// The shortfall message caps missing case ids per row at maxNamedMissingCases,
// but deliberately names EVERY short row: each row is a distinct reviewer
// identity whose re-runs the operator must schedule, so an overflow count on the
// outer list would hide who owes work — the actionable unit of the message.
func TestCheckCoverage_EveryShortRowIsNamed(t *testing.T) {
	rr := benchmark.RunResult{SuiteCaseIDs: []string{"case-01"}}
	for i := 0; i < 5; i++ {
		model := fmt.Sprintf("m-%d", i)
		rr.Reviewers = append(rr.Reviewers, scorecard.PublicRecord{Model: model, Persona: "p", Runs: 0})
		rr.Coverage = append(rr.Coverage, benchmark.ReviewerCoverage{Model: model, Persona: "p"})
	}

	err := checkCoverage(io.Discard, rr, "rr.json", false)
	require.Error(t, err)
	msg := err.Error()
	for i := 0; i < 5; i++ {
		assert.Contains(t, msg, fmt.Sprintf("m-%d/p", i), "every short row is named, past any cap")
	}
}

// The outcomes tally is untrusted input at the export boundary, same as
// out_of_vocabulary_rate at load: the producer writes one closed-vocabulary outcome
// per case, so a NEGATIVE count or a key outside the vocabulary can only come from a
// hand-assembled file. The sum check alone catches neither — a -1 paired with an
// inflated positive still sums to the covered-set size, and a fabricated key simply
// adds to the tally.
func TestCheckCoverage_RejectsMalformedOutcomeTallies(t *testing.T) {
	base := func(outcomes map[string]int) benchmark.RunResult {
		return benchmark.RunResult{
			SuiteCaseIDs: []string{"case-01"},
			Reviewers:    []scorecard.PublicRecord{{Model: "m", Persona: "p", Runs: 1}},
			Coverage: []benchmark.ReviewerCoverage{{
				Model: "m", Persona: "p", CaseIDs: []string{"case-01"}, Outcomes: outcomes,
			}},
		}
	}

	// The two arms reject for different reasons and say so differently: a negative
	// count can only be hand-assembly, while an unknown KEY is more often a newer
	// producer than a tampered file, so that arm names version skew instead.
	for name, tc := range map[string]struct {
		outcomes map[string]int
		wants    []string
	}{
		"negative count": { // sums to 1: the sum check passes it
			outcomes: map[string]int{"clean": -1, "findings": 2},
			wants:    []string{"malformed", "negative outcome tally"},
		},
		"out-of-vocabulary key": { // sums to 1: also passes
			outcomes: map[string]int{"fabricated": 1},
			wants:    []string{"outside the outcome vocabulary", "version skew", "hand-assembled"},
		},
	} {
		err := checkCoverage(io.Discard, base(tc.outcomes), "rr.json", false)
		require.Error(t, err, name)
		for _, want := range tc.wants {
			assert.Contains(t, err.Error(), want, name)
		}
	}

	// The legitimate vocabulary — including the "unknown" tally label — stays legal.
	err := checkCoverage(io.Discard, base(map[string]int{"unknown": 1}), "rr.json", false)
	require.NoError(t, err, "the unknown tally label is a legitimate key")
}

// The outcome vocabulary GREW (repo-state-v1 added "ungrounded"), so the commonest
// way to reach the vocabulary rejection is no longer a hand-assembled file: it is an
// older atcr running `benchmark export` over a newer run-result. Accusing that
// operator of hand-assembly sends them auditing a file nobody edited, when the
// remedy is one upgrade. duplicateIdentityError in this same file already names both
// causes; this rejection is the boundary that ACTUALLY breaks on a new value.
func TestCheckCoverage_VocabularyRejectionNamesVersionSkew(t *testing.T) {
	rr := benchmark.RunResult{
		SuiteCaseIDs: []string{"case-01"},
		Reviewers:    []scorecard.PublicRecord{{Model: "m", Persona: "p", Runs: 1}},
		Coverage: []benchmark.ReviewerCoverage{{
			Model: "m", Persona: "p", CaseIDs: []string{"case-01"},
			// A value a FUTURE atcr writes and this build does not know — the exact
			// shape "ungrounded" had for a pre-35.16.10 reader.
			Outcomes: map[string]int{"from-a-newer-atcr": 1},
		}},
	}

	err := checkCoverage(io.Discard, rr, "rr.json", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "from-a-newer-atcr", "the offending key is named")
	assert.Contains(t, err.Error(), "version skew",
		"version skew is the likelier cause and must be named, as duplicateIdentityError names it")
	assert.Contains(t, err.Error(), "upgrade atcr",
		"the remedy for the likelier cause must be stated, not left to the reader")
	assert.Contains(t, err.Error(), "hand-assembled",
		"the other cause stays named — this is an additive rewording, not a swap")
}

// grounding_enabled is published verbatim into the public envelope and rides beside
// corroboration_rate as the tag saying which population that rate was computed over,
// yet every OTHER coverage field is treated as hostile input here. The producer
// guarantees one implication for free: the ungrounded outcome is reached only via
// AgentStatus.DroppedByGrounding > 0, which is unreachable with the gate off. So a row
// that tallies ungrounded beside an explicit "the gate was off for every case I scored"
// is self-contradictory and can only be hand-assembled.
//
// A nil tag is NOT rejected. nil means unmeasured — a rebuilt summary, or a row folded
// across a mix of gated and ungated cases — which is uninformative rather than
// contradictory, and rejecting it would make a legitimate paid run unexportable.
func TestCheckCoverage_RejectsUngroundedOutcomeUnderAGateOffClaim(t *testing.T) {
	on, off := true, false
	run := func(grounding *bool) benchmark.RunResult {
		return benchmark.RunResult{
			SuiteCaseIDs: []string{"case-01"},
			Reviewers:    []scorecard.PublicRecord{{Model: "m", Persona: "p", Runs: 1}},
			Coverage: []benchmark.ReviewerCoverage{{
				Model: "m", Persona: "p", CaseIDs: []string{"case-01"},
				Outcomes:         map[string]int{benchmark.OutcomeUngrounded: 1},
				GroundingEnabled: grounding,
			}},
		}
	}

	err := checkCoverage(io.Discard, run(&off), "rr.json", false)
	require.Error(t, err, "ungrounded is unreachable with the gate off")
	assert.Contains(t, err.Error(), benchmark.OutcomeUngrounded)
	assert.Contains(t, err.Error(), "grounding_enabled=false")
	// Both causes are named, like duplicateIdentityError's: a mixed multi-case fold
	// still reports ungated rather than unmeasured, so a legitimate run reaches here
	// and "hand-assembled" alone would misdiagnose it.
	assert.Contains(t, err.Error(), "hand-assembled")
	assert.Contains(t, err.Error(), "upgrade atcr")

	require.NoError(t, checkCoverage(io.Discard, run(&on), "rr.json", false),
		"a gated row tallying ungrounded is exactly what the producer writes")
	require.NoError(t, checkCoverage(io.Discard, run(nil), "rr.json", false),
		"an unmeasured tag is uninformative, not contradictory")
}

// Every reviewer identity this gate interpolates into an operator-facing message comes
// from the same untrusted, possibly hand-supplied run-result the gate is validating, and
// cobra prints the returned error to the same terminal as the `short` warnings that were
// already sanitized. Each malformed-file path below returns EARLY — before `short` is
// ever built — so sanitizing only the warning sites leaves the rejection sites, the ones
// a hostile file reaches FIRST, able to erase and rewrite the operator's line.
func TestCheckCoverage_SanitizesIdentityInEveryRejection(t *testing.T) {
	// Erase-line + cursor-home, the shape that overwrites what was already printed.
	const esc = "\x1b[2K\x1b[1Gall checks passed"
	model, persona := "m"+esc, "p"+esc

	cov := func(caseIDs []string, outcomes map[string]int) benchmark.ReviewerCoverage {
		return benchmark.ReviewerCoverage{Model: model, Persona: persona, CaseIDs: caseIDs, Outcomes: outcomes}
	}
	rev := func(runs int) scorecard.PublicRecord {
		return scorecard.PublicRecord{Model: model, Persona: persona, Runs: runs}
	}

	for name, rr := range map[string]benchmark.RunResult{
		"duplicate coverage identity": {
			SuiteCaseIDs: []string{"case-01"},
			Coverage:     []benchmark.ReviewerCoverage{cov([]string{"case-01"}, nil), cov([]string{"case-01"}, nil)},
		},
		"negative outcome tally": {
			SuiteCaseIDs: []string{"case-01"},
			Reviewers:    []scorecard.PublicRecord{rev(1)},
			Coverage:     []benchmark.ReviewerCoverage{cov([]string{"case-01"}, map[string]int{"clean": -1, "findings": 2})},
		},
		"out-of-vocabulary outcome key": {
			SuiteCaseIDs: []string{"case-01"},
			Reviewers:    []scorecard.PublicRecord{rev(1)},
			Coverage:     []benchmark.ReviewerCoverage{cov([]string{"case-01"}, map[string]int{"fabricated": 1})},
		},
		"duplicate reviewer identity": {
			SuiteCaseIDs: []string{"case-01"},
			Reviewers:    []scorecard.PublicRecord{rev(1), rev(1)},
			Coverage:     []benchmark.ReviewerCoverage{cov([]string{"case-01"}, nil)},
		},
		"runs disagrees with covered set": {
			SuiteCaseIDs: []string{"case-01"},
			Reviewers:    []scorecard.PublicRecord{rev(2)},
			Coverage:     []benchmark.ReviewerCoverage{cov([]string{"case-01"}, nil)},
		},
		"outcomes do not sum to the covered set": {
			SuiteCaseIDs: []string{"case-01"},
			Reviewers:    []scorecard.PublicRecord{rev(1)},
			Coverage:     []benchmark.ReviewerCoverage{cov([]string{"case-01"}, map[string]int{"clean": 2})},
		},
		"coverage row no reviewer consumed": {
			SuiteCaseIDs: []string{"case-01"},
			Reviewers:    []scorecard.PublicRecord{{Model: "clean-model", Persona: "clean-persona", Runs: 0}},
			Coverage:     []benchmark.ReviewerCoverage{cov([]string{"case-01"}, nil)},
		},
		"case listed twice in one row": {
			SuiteCaseIDs: []string{"case-01"},
			Reviewers:    []scorecard.PublicRecord{rev(2)},
			Coverage:     []benchmark.ReviewerCoverage{cov([]string{"case-01", "case-01"}, nil)},
		},
		"case outside the suite": {
			SuiteCaseIDs: []string{"case-01"},
			Reviewers:    []scorecard.PublicRecord{rev(1)},
			Coverage:     []benchmark.ReviewerCoverage{cov([]string{"case-99"}, nil)},
		},
	} {
		err := checkCoverage(io.Discard, rr, "rr.json", false)
		require.Error(t, err, name)
		assert.NotContains(t, err.Error(), "\x1b", "%s: the identity reaches the terminal unstripped", name)
	}
}

// CASE IDS are untrusted for exactly the same reason the identity is: they come from
// the run-result being validated, and `atcr benchmark export` is where a hand-supplied
// file first enters the tool. Every id that reaches the terminal does so through
// summarizeMissing under %s.
//
// The id sites inside validateCoveredSet are already safe and stay untouched — they use
// %q, which renders ESC as a literal \x1b escape. That asymmetry is why these five
// survived the identity sweep: the same field is safe under one verb and not the other.
func TestCoverageDiagnostics_SanitizeUntrustedCaseIDs(t *testing.T) {
	const esc = "\x1b[2K\x1b[1Gfull coverage"
	hostileID := "case-02" + esc

	shortRun := benchmark.RunResult{
		SuiteCaseIDs: []string{"case-01", hostileID},
		Reviewers:    []scorecard.PublicRecord{{Model: "m", Persona: "p", Runs: 1}},
		Coverage: []benchmark.ReviewerCoverage{{
			Model: "m", Persona: "p", CaseIDs: []string{"case-01"},
		}},
	}

	// The rejection path: the shortfall names the ids the row still owes.
	err := checkCoverage(io.Discard, shortRun, "rr.json", false)
	require.Error(t, err, "precondition: this row is short of the suite")
	assert.NotContains(t, err.Error(), "\x1b",
		"a missing case id reaches the terminal through the same fmt.Errorf as the identity")

	// The --allow-partial-coverage path writes the same list to a warning instead.
	var warn bytes.Buffer
	require.NoError(t, checkCoverage(&warn, shortRun, "rr.json", true))
	require.Contains(t, warn.String(), "partial coverage", "precondition: the warning must have fired")
	assert.NotContains(t, warn.String(), "\x1b",
		"the opt-out path prints the same ids and owes the same stripping")

	// anchorSuiteDenominator's three diagnostics read the DECLARED list, which is
	// untrusted in the same way — an id the manifest does not contain is echoed back.
	anchored := benchmark.RunResult{
		Suite:        "fixture-mini",
		SuiteVersion: "1.0.0",
		SuiteCaseIDs: []string{"case-01-nil-deref", "case-02-sql-injection", hostileID},
	}
	aerr := anchorSuiteDenominator(anchored, suiteValidPath, "rr.json")
	require.Error(t, aerr, "precondition: the declared list carries a case the manifest lacks")
	assert.NotContains(t, aerr.Error(), "\x1b",
		"an id the suite does not contain is still echoed to the operator's terminal")
}

// rr.Suite and rr.SuiteVersion have the same provenance as the case ids and the reviewer
// identities — read straight from the operator-supplied run-result — and reach the same
// terminal through the same cobra error path. The suite-identity mismatch is the FIRST
// of anchorSuiteDenominator's three checks, so it is the one an attacker-supplied file
// trips most easily, and it is the only one that does not funnel through
// summarizeMissing's stripping.
// On repo-state-v1 the suite NAME cannot distinguish two suites: LoadRepoState
// requires the manifest to declare exactly "repo-state-v1" and then stamps that
// constant onto the loaded manifest, so every repo-state run-result and every
// repo-state manifest carry the same literal. The identity check above is therefore
// a tautology on this tier, and the documented ordering guarantee — "anchoring to
// the wrong suite reports the wrong suite rather than reporting every case as
// missing" — is false here: anchoring run-result A against suite B falls through to
// the case-set check and reports "every reviewer row was scored against a shrunken
// denominator", a claim about the run that is simply not true.
//
// The gate must still REJECT (it does, via the case set). What it must stop doing is
// asserting the wrong cause.
func TestAnchorSuiteDenominator_RepoStateMismatchDoesNotBlameTheDenominator(t *testing.T) {
	// A run-result carrying the repo-state identity and a case list from some OTHER
	// repo-state suite — the shape the tautological name check cannot catch.
	err := anchorSuiteDenominator(benchmark.RunResult{
		Suite:        benchmark.FormatRepoStateV1,
		SuiteVersion: "1.0.0",
		SuiteCaseIDs: []string{"a-case-from-another-suite"},
	}, repoStateMiniPath, "rr.json")

	require.Error(t, err, "a run-result from a different repo-state suite must still be rejected")
	assert.Contains(t, err.Error(), "not author-distinguishable",
		"the message must say the suite name cannot tell two repo-state suites apart")
	assert.NotContains(t, err.Error(), "shrunken denominator",
		"blaming the denominator asserts the run was truncated, which this file gives no evidence for")
}

func TestAnchorSuiteDenominator_SanitizesTheUntrustedSuiteIdentity(t *testing.T) {
	const esc = "\x1b[2K\x1b[1Gall checks passed"

	err := anchorSuiteDenominator(benchmark.RunResult{
		Suite:        "atcr-bench" + esc,
		SuiteVersion: "9.9.9\x07",
		SuiteCaseIDs: []string{"case-01-nil-deref"},
	}, suiteValidPath, "rr.json")

	require.Error(t, err, "precondition: the run-result names a different suite than the manifest")
	assert.NotContains(t, err.Error(), "\x1b",
		"an ESC in the declared suite name can erase the mismatch report and forge a clean one")
	assert.NotContains(t, err.Error(), "\x07", "nor a BEL")
	assert.Contains(t, err.Error(), "atcr-bench", "the suite must still be identifiable")
	assert.Contains(t, err.Error(), "9.9.9", "and so must its version")
}

// Sanitizing by DELETION makes a real mismatch unreadable. stripTerminalControlRunes
// drops every unicode.IsControl rune — \r and \n included, not just ESC — while the
// identity gate is a raw != with no trimming. So a CRLF-mangled run-result whose suite
// genuinely differs from the manifest's renders as two IDENTICAL strings, and the
// operator concludes the check is spurious rather than fixing the file. The error is the
// one artifact whose whole job is to show a difference.
//
// %q is the verb this file already prescribes for untrusted identity (see the note on
// summarizeMissing): it renders a control rune as a visible escape AND keeps the two
// values distinguishable. Both halves of the comparison must use it — rendering the
// run-result side under one rule and the manifest side under another is what let the
// difference vanish.
func TestAnchorSuiteDenominator_MismatchStaysVisibleWhenTheDifferenceIsAControlRune(t *testing.T) {
	err := anchorSuiteDenominator(benchmark.RunResult{
		// Differs from the manifest's "fixture-mini"/"1.0.0" by a trailing CR only.
		Suite:        "fixture-mini\r",
		SuiteVersion: "1.0.0",
		SuiteCaseIDs: []string{"case-01-nil-deref"},
	}, suiteValidPath, "rr.json")

	require.Error(t, err, "precondition: a trailing CR is a genuine identity mismatch")
	got := err.Error()

	assert.NotContains(t, got, "\r", "the raw control rune must not reach the terminal")
	assert.Contains(t, got, `\r`,
		"the difference must remain VISIBLE as an escape — deleting it renders both sides identical "+
			"and reads as a spurious failure")
	assert.NotContains(t, got, `is for suite "fixture-mini"/"1.0.0" but the manifest at`,
		"if the two halves render identically the message contradicts itself")
}

// partialRun is a 3-case run whose middle case failed infrastructurally: the
// reviewer scored the other two, and the failure channel says why the third is
// absent. It is the shape every check below is about.
func partialRun() benchmark.RunResult {
	return benchmark.RunResult{
		SuiteCaseIDs: []string{"case-01", "case-02", "case-03"},
		Reviewers:    []scorecard.PublicRecord{{Model: "m", Persona: "p", Runs: 2}},
		Coverage: []benchmark.ReviewerCoverage{
			{Model: "m", Persona: "p", CaseIDs: []string{"case-01", "case-03"}},
		},
		CaseFailures: []benchmark.CaseFailure{{CaseID: "case-02", Reason: benchmark.CaseFailurePrepare}},
	}
}

// slotFailureRun is partialRun's slot-level sibling: the case RAN and one reviewer
// missed it, so the case is in nobody's failure list and only this reviewer's
// covered set is short by it.
func slotFailureRun() benchmark.RunResult {
	return benchmark.RunResult{
		SuiteCaseIDs: []string{"case-01", "case-02", "case-03"},
		Reviewers:    []scorecard.PublicRecord{{Model: "m", Persona: "p", Runs: 2}},
		Coverage: []benchmark.ReviewerCoverage{
			{Model: "m", Persona: "p", CaseIDs: []string{"case-01", "case-03"}},
		},
		SlotFailures: []benchmark.SlotFailure{
			{Model: "m", Persona: "p", CaseID: "case-02", Reason: benchmark.SlotFailureTimeout},
		},
	}
}

// The slot channel gets the same fail-closed treatment as the case channel, and for
// a sharper reason: the shortfall diagnostic interpolates this reason into an
// operator's terminal, so an unvalidated entry is arbitrary attacker-chosen prose
// presented as an explanation.
func TestValidateSlotFailures_RejectsAnUnknownReason(t *testing.T) {
	rr := slotFailureRun()
	rr.SlotFailures[0].Reason = "vibes"

	err := validateSlotFailures(rr, "rr.json")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "vibes")
	assert.Contains(t, err.Error(), "slot_failures")
}

// The empty reason is what omitempty and a partly-populated hand edit produce, and
// "this reviewer missed it, cause unstated" is the claim the channel exists to stop.
func TestValidateSlotFailures_RejectsAnEmptyReason(t *testing.T) {
	rr := slotFailureRun()
	rr.SlotFailures[0].Reason = ""

	require.Error(t, validateSlotFailures(rr, "rr.json"))
}

// A slot failure explains why ONE row is short. Naming an identity with no coverage
// row explains nothing, and would let a hand-assembled file attach an excuse to a
// reviewer the run never had.
func TestValidateSlotFailures_RejectsAnIdentityWithNoCoverageRow(t *testing.T) {
	rr := slotFailureRun()
	rr.SlotFailures[0].Model = "ghost"

	err := validateSlotFailures(rr, "rr.json")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost")
	assert.Contains(t, err.Error(), "no reviewer_coverage row")
}

// The contradiction arm, and it is PER IDENTITY rather than global — that is the
// whole difference from the case-level channel. A case another reviewer scored is
// perfectly consistent with this reviewer having missed it; a case THIS reviewer's
// own row claims is not.
func TestValidateSlotFailures_RejectsACaseTheSameReviewerScored(t *testing.T) {
	rr := slotFailureRun()
	rr.SlotFailures[0].CaseID = "case-01" // already in that row's covered set

	err := validateSlotFailures(rr, "rr.json")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "both scored by and slot-failed for")
}

// The mirror of the arm above: a case a DIFFERENT reviewer scored is the normal
// shape, not a contradiction. Without this the validator would reject every genuine
// partial-slot run, which is the failure mode that makes an over-strict gate worse
// than none.
func TestValidateSlotFailures_AcceptsACaseAnotherReviewerScored(t *testing.T) {
	rr := slotFailureRun()
	rr.Coverage = append(rr.Coverage, benchmark.ReviewerCoverage{
		Model: "m2", Persona: "p2", CaseIDs: []string{"case-01", "case-02", "case-03"},
	})

	require.NoError(t, validateSlotFailures(rr, "rr.json"),
		"the surviving reviewer scoring the case is exactly what a slot failure means")
}

// A case outside the declared suite explains no shortfall in it.
func TestValidateSlotFailures_RejectsACaseOutsideTheSuite(t *testing.T) {
	rr := slotFailureRun()
	rr.SlotFailures[0].CaseID = "case-99"

	require.Error(t, validateSlotFailures(rr, "rr.json"))
}

// Every comparison runs on the RAW id while every message prints the stripped one,
// so the rune arm has to fire before the membership arms — otherwise the rejection
// would report that suite_case_ids does not declare a case it visibly declares.
func TestValidateSlotFailures_RejectsANonPrintingRune(t *testing.T) {
	rr := slotFailureRun()
	rr.SlotFailures[0].CaseID = "case-02​"

	err := validateSlotFailures(rr, "rr.json")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-printing rune")
	assert.Contains(t, err.Error(), "U+200B")
	assert.NotContains(t, err.Error(), "does not declare",
		"naming the rune is the point; blaming the denominator would send the reader to the wrong file")
}

// The producer writes one record per (reviewer, case) pair.
func TestValidateSlotFailures_RejectsADuplicatePair(t *testing.T) {
	rr := slotFailureRun()
	rr.SlotFailures = append(rr.SlotFailures, rr.SlotFailures[0])

	err := validateSlotFailures(rr, "rr.json")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "more than once")
}

// Two reviewers missing the SAME case is not a duplicate — it is two independent
// slot failures. The key is the triple, not the case id.
func TestValidateSlotFailures_AcceptsTwoReviewersMissingOneCase(t *testing.T) {
	rr := slotFailureRun()
	rr.Coverage = append(rr.Coverage, benchmark.ReviewerCoverage{
		Model: "m2", Persona: "p2", CaseIDs: []string{"case-01", "case-03"},
	})
	rr.SlotFailures = append(rr.SlotFailures, benchmark.SlotFailure{
		Model: "m2", Persona: "p2", CaseID: "case-02", Reason: benchmark.SlotFailureCall,
	})

	require.NoError(t, validateSlotFailures(rr, "rr.json"))
}

// A file carrying the array but no denominator gets its own rejection rather than
// the membership arm's, which would blame the case id for an absent suite_case_ids.
func TestValidateSlotFailures_RejectsAnAbsentDenominator(t *testing.T) {
	rr := slotFailureRun()
	rr.SuiteCaseIDs = nil

	err := validateSlotFailures(rr, "rr.json")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no suite_case_ids")
}

// A clean run carries no array and must pass untouched.
func TestValidateSlotFailures_CleanRunPasses(t *testing.T) {
	require.NoError(t, validateSlotFailures(partialRun(), "rr.json"))
}

// AC3 — the failure reason is fail-closed at the EXPORT trust boundary, the only
// live one for this tier (--checkpoint is refused for repo-state-v1). A run-result
// is hand-suppliable, so an unrecognized reason must be REJECTED rather than
// carried into a published artifact that states why a case went unmeasured. Same
// rule the outcome tally key already gets one field over.
func TestValidateCaseFailures_RejectsAnUnknownReason(t *testing.T) {
	rr := partialRun()
	rr.CaseFailures[0].Reason = "vibes"

	err := validateCaseFailures(rr, "rr.json")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "vibes")
	assert.Contains(t, err.Error(), "case_failures")
}

// The empty reason is the shape omitempty and a partly-populated hand edit produce,
// and it is exactly what the vocabulary must not admit: "unmeasured, cause
// unstated" is the claim the channel exists to prevent.
func TestValidateCaseFailures_RejectsAnEmptyReason(t *testing.T) {
	rr := partialRun()
	rr.CaseFailures[0].Reason = ""

	require.Error(t, validateCaseFailures(rr, "rr.json"))
}

// A failure naming a case the suite does not contain describes nothing a reader can
// locate, and would let a hand-assembled file explain away a shortfall that has no
// relationship to the declared suite.
func TestValidateCaseFailures_RejectsACaseOutsideTheSuite(t *testing.T) {
	rr := partialRun()
	rr.CaseFailures[0].CaseID = "case-99"

	err := validateCaseFailures(rr, "rr.json")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "case-99")
}

// Scored AND unmeasured is a contradiction, and the permissive reading is the
// dangerous one: it would let a file excuse a coverage shortfall using a case every
// reviewer actually scored.
func TestValidateCaseFailures_RejectsACaseThatAlsoScored(t *testing.T) {
	rr := partialRun()
	rr.CaseFailures[0].CaseID = "case-01"

	err := validateCaseFailures(rr, "rr.json")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "case-01")
}

// The producer records each failed case once; a repeat is a hand edit, and would
// inflate the count an operator reads as "how much of this run is missing".
func TestValidateCaseFailures_RejectsARepeatedCase(t *testing.T) {
	rr := partialRun()
	rr.CaseFailures = append(rr.CaseFailures, benchmark.CaseFailure{
		CaseID: "case-02", Reason: benchmark.CaseFailureExecute,
	})

	require.Error(t, validateCaseFailures(rr, "rr.json"))
}

// The membership arm is a catch-all, so the two structurally different malformed
// shapes it used to swallow each get their own diagnostic. A file with no denominator
// is not a file with a bad case id, and neither is an entry that names no case: the
// old message blamed suite_case_ids for the first and the case id for the second,
// with the same sentence.
func TestValidateCaseFailures_RejectsAnArrayWithNoDenominator(t *testing.T) {
	rr := partialRun()
	rr.SuiteCaseIDs = nil

	err := validateCaseFailures(rr, "rr.json")

	require.Error(t, err, "the channel must not be unvalidatable on the very shape checkCoverage warns-and-exports")
	assert.Contains(t, err.Error(), "no suite_case_ids", "the diagnostic names the absent denominator")
	assert.NotContains(t, err.Error(), "does not declare",
		"and does not blame the case id for a defect one field over")
}

// An array longer than the declared suite cannot be well-formed: every entry must
// name a distinct declared case. Rejecting the shape up front is what the sibling
// validators in this file do, and it means the indexes are never built over an array
// no producer can write.
func TestValidateCaseFailures_RejectsAnArrayLongerThanTheSuite(t *testing.T) {
	rr := partialRun()
	rr.SuiteCaseIDs = []string{"case-01", "case-02"}
	rr.Coverage[0].CaseIDs = []string{"case-01"}
	rr.Reviewers[0].Runs = 1
	rr.CaseFailures = []benchmark.CaseFailure{
		{CaseID: "case-02", Reason: benchmark.CaseFailurePrepare},
		{CaseID: "case-02", Reason: benchmark.CaseFailureExecute},
		{CaseID: "case-02", Reason: benchmark.CaseFailureWorkDir},
	}

	err := validateCaseFailures(rr, "rr.json")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "3 case_failures entries over a 2-case suite",
		"the shape is rejected on its own terms, not reported as a repeated case")
}

// Every comparison in validateCaseFailures is on the RAW case id while every message
// prints the stripped one, so an id carrying a zero-width rune can never match a
// suite id (validateScrubbedCaseIDs guarantees those are clean) and the membership arm
// would report `an entry for "case-02", which suite_case_ids does not declare` about a
// file whose suite_case_ids visibly contains case-02. The rune gets named instead.
func TestValidateCaseFailures_NamesANonPrintingRuneInsteadOfContradictingItself(t *testing.T) {
	rr := partialRun()
	rr.CaseFailures[0].CaseID = "case-02​"

	err := validateCaseFailures(rr, "rr.json")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "U+200B", "the message names the rune that made the id unmatchable")
	assert.NotContains(t, err.Error(), "does not declare",
		"a file whose suite_case_ids contains case-02 must not be told it does not")
}

func TestValidateCaseFailures_RejectsABlankCaseID(t *testing.T) {
	rr := partialRun()
	rr.CaseFailures[0].CaseID = "  "

	err := validateCaseFailures(rr, "rr.json")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "blank case_id", "an entry that identifies no case says so")
	assert.NotContains(t, err.Error(), "does not declare")
}

// The honest partial run passes every arm.
func TestValidateCaseFailures_AcceptsAPartialRun(t *testing.T) {
	assert.NoError(t, validateCaseFailures(partialRun(), "rr.json"))
	assert.NoError(t, validateCaseFailures(benchmark.RunResult{SuiteCaseIDs: []string{"case-01"}}, "rr.json"),
		"a run with no failures has nothing to validate")
}

// T4 — the gate still REJECTS a partial run without --allow-partial-coverage: an
// infra-failed case is unmeasured, which is exactly why its rows are not comparable
// to fully-covered ones. What changes is the diagnosis. Told only to "re-run the
// missing cases", an operator re-runs a case that never ran for a reason a re-run
// may not fix; the gate names the failure instead.
func TestCheckCoverage_ExplainsAnInfrastructureShortfall(t *testing.T) {
	err := checkCoverage(io.Discard, partialRun(), "rr.json", false)

	require.Error(t, err, "an unmeasured case still makes the row incomparable")
	// Composed substrings, not loose words. "missing" alone is satisfied by the static
	// remedy sentence the gate always prints, and benchmark.CaseFailurePrepare alone
	// says nothing about which LABEL the case was filed under — so labelling both
	// halves "unmeasured" (or both "missing") passed the old assertions while
	// collapsing the exact distinction describeMissing exists to draw.
	assert.Contains(t, err.Error(), "unmeasured case-02 ("+benchmark.CaseFailurePrepare+")",
		"an explained shortfall is labelled unmeasured and carries its reason")
	assert.NotContains(t, err.Error(), "missing case-02",
		"and is not also reported as a case the operator forgot to run")
}

// A shortfall the failure channel does NOT explain keeps the original diagnosis. The
// two must stay distinguishable: one is a run that lost a case, the other is a row
// that was never scored over the suite it claims.
func TestCheckCoverage_UnexplainedShortfallStillReadsAsMissing(t *testing.T) {
	rr := partialRun()
	rr.CaseFailures = nil
	rr.Coverage[0].CaseIDs = []string{"case-01"}
	rr.Reviewers[0].Runs = 1

	err := checkCoverage(io.Discard, rr, "rr.json", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing case-02, case-03",
		"an unexplained shortfall names its cases under the missing label")
	assert.NotContains(t, err.Error(), "unmeasured case-",
		"and no case is labelled unmeasured when the failure channel explains none of them")
}

// A SLOT-level shortfall is a third thing, and it must not borrow either of the other
// two labels.
//
// "missing" means unaccounted-for — a row claiming a suite it was not scored over,
// which before the slot skip existed was reachable only by tampering. "unmeasured"
// means the case never ran for anyone. This case ran, the rest of the panel scored
// it, and one reviewer was not shown it; calling that "missing" told the operator to
// re-run cases that ran perfectly, and made one flaky provider slot read as a
// hand-assembled file.
func TestCheckCoverage_ExplainsASlotShortfall(t *testing.T) {
	err := checkCoverage(io.Discard, slotFailureRun(), "rr.json", false)

	require.Error(t, err, "a reviewer short of the suite is still not comparable")
	assert.Contains(t, err.Error(), "unshown case-02 ("+benchmark.SlotFailureTimeout+")",
		"a slot shortfall is labelled unshown and carries the reason the slot failed")
	assert.NotContains(t, err.Error(), "missing case-02",
		"and must not read as a case nobody ran — the rest of the panel scored it")
	assert.NotContains(t, err.Error(), "unmeasured case-02",
		"nor as a case-level failure, which is a different channel with a different remedy")
}

// The three halves are independent and coexist on one row. A row can be short for all
// three reasons at once, and collapsing any pair would hide one behind another.
func TestCheckCoverage_SeparatesAllThreeShortfallKinds(t *testing.T) {
	rr := benchmark.RunResult{
		SuiteCaseIDs: []string{"case-01", "case-02", "case-03", "case-04"},
		Reviewers:    []scorecard.PublicRecord{{Model: "m", Persona: "p", Runs: 1}},
		Coverage: []benchmark.ReviewerCoverage{
			{Model: "m", Persona: "p", CaseIDs: []string{"case-01"}},
		},
		CaseFailures: []benchmark.CaseFailure{
			{CaseID: "case-02", Reason: benchmark.CaseFailurePrepare},
		},
		SlotFailures: []benchmark.SlotFailure{
			{Model: "m", Persona: "p", CaseID: "case-03", Reason: benchmark.SlotFailureCall},
		},
		// case-04 is accounted for by nothing — the genuine "missing" shape.
	}

	err := checkCoverage(io.Discard, rr, "rr.json", false)

	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "missing case-04", "the unaccounted-for case keeps the original label")
	assert.Contains(t, msg, "unmeasured case-02 ("+benchmark.CaseFailurePrepare+")",
		"the case-level failure keeps its label and reason")
	assert.Contains(t, msg, "unshown case-03 ("+benchmark.SlotFailureCall+")",
		"the slot-level failure gets its own")
}

// A slot failure belonging to ANOTHER reviewer must not explain THIS reviewer's
// shortfall. The index is per identity, and getting that wrong would attach one
// reviewer's excuse to another's row — the "excuse a row never earned" shape the
// export validator exists to prevent, reproduced one layer up.
func TestCheckCoverage_SlotShortfallIsScopedToItsOwnReviewer(t *testing.T) {
	rr := benchmark.RunResult{
		SuiteCaseIDs: []string{"case-01", "case-02"},
		Reviewers: []scorecard.PublicRecord{
			{Model: "m1", Persona: "p1", Runs: 1},
			{Model: "m2", Persona: "p2", Runs: 1},
		},
		Coverage: []benchmark.ReviewerCoverage{
			{Model: "m1", Persona: "p1", CaseIDs: []string{"case-01"}},
			{Model: "m2", Persona: "p2", CaseIDs: []string{"case-01"}},
		},
		SlotFailures: []benchmark.SlotFailure{
			{Model: "m1", Persona: "p1", CaseID: "case-02", Reason: benchmark.SlotFailureCall},
		},
	}

	err := checkCoverage(io.Discard, rr, "rr.json", false)

	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "m1/p1 (1/2 cases, unshown case-02 ("+benchmark.SlotFailureCall+"))",
		"the reviewer that lost the slot carries the explanation")
	assert.Contains(t, msg, "m2/p2 (1/2 cases, missing case-02)",
		"the reviewer that did not lose a slot is still unexplained — it borrowed nobody's reason")
}

// The defence-in-depth drop is pinned through checkCoverage itself, not only through
// validateCaseFailures. checkCoverage is callable without the export command's gate
// in front of it — this test is such a caller — and the point of the drop is that the
// diagnostic's safety must not rest on the order two functions happen to be called
// in. An out-of-vocabulary reason is ignored, so the case falls back to reading as
// plainly missing and neither the escape sequence nor the injected prose reaches the
// message. Verify by removing the `continue` in checkCoverage's failure-index loop:
// the case then reads as unmeasured and carries the injected text.
func TestCheckCoverage_DropsAnOutOfVocabularyFailureReason(t *testing.T) {
	rr := partialRun()
	rr.CaseFailures[0].Reason = "prepare\x1b[2Kinjected"

	err := checkCoverage(io.Discard, rr, "rr.json", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing case-02",
		"a case whose reason is outside the vocabulary explains nothing, so it reads as plainly missing")
	assert.NotContains(t, err.Error(), "\x1b", "no control rune reaches the operator's terminal")
	assert.NotContains(t, err.Error(), "injected", "and neither does the arbitrary prose it was carrying")
}

// mixedShortfallRun is a 4-case run in which EVERY reviewer row is short in both
// ways at once: case-03 was recorded as an infrastructure failure (unmeasured) and
// case-04 is unaccounted for (missing). Two reviewer rows, so the message nests a
// row list around a two-half description — the shape the separators must keep
// distinguishable.
func mixedShortfallRun() benchmark.RunResult {
	rr := benchmark.RunResult{
		SuiteCaseIDs: []string{"case-01", "case-02", "case-03", "case-04"},
		CaseFailures: []benchmark.CaseFailure{{CaseID: "case-03", Reason: benchmark.CaseFailurePrepare}},
	}
	for _, model := range []string{"m-a", "m-b"} {
		rr.Reviewers = append(rr.Reviewers, scorecard.PublicRecord{Model: model, Persona: "p", Runs: 2})
		rr.Coverage = append(rr.Coverage, benchmark.ReviewerCoverage{
			Model: model, Persona: "p", CaseIDs: []string{"case-01", "case-02"},
		})
	}
	return rr
}

// The two halves of one row's description and the list of distinct short ROWS are
// different nesting levels, so they must not share a delimiter. With both at "; " a
// reader — or anything downstream that splits on it — reads one reviewer row as two.
func TestCheckCoverage_HalfSeparatorDoesNotCollideWithTheRowSeparator(t *testing.T) {
	err := checkCoverage(io.Discard, mixedShortfallRun(), "rr.json", false)

	require.Error(t, err)
	msg := err.Error()
	assert.Equal(t, 2, strings.Count(msg, halfSeparator),
		"each of the two short rows splits its own description in half: %s", msg)
	// One boundary between the two rows, plus the one the remedy clause is appended
	// after. Both are row-level, which is the property "; " now exclusively carries.
	assert.Equal(t, 2, strings.Count(msg, "; "),
		"\"; \" stays reserved for the outer row list: %s", msg)
}

// Dropping the entry must not also hide it. A caller with no export gate in front of
// it would otherwise be told to re-run a case the file claims was unmeasured, with
// nothing saying the file made a claim at all. The warning names the case id and
// reports the reason stripped and quoted, so the entry is visible without its prose
// being interpolated as an explanation.
func TestCheckCoverage_WarnsWhenItDropsAnOutOfVocabularyFailureReason(t *testing.T) {
	rr := partialRun()
	rr.CaseFailures[0].Reason = "prepare\x1b[2Kinjected"
	var warn bytes.Buffer

	require.Error(t, checkCoverage(&warn, rr, "rr.json", false))

	assert.Contains(t, warn.String(), "case-02", "the dropped entry is named")
	assert.Contains(t, warn.String(), "outside the failure vocabulary", "and so is why it was dropped")
	assert.NotContains(t, warn.String(), "\x1b", "the reason is stripped before it reaches the terminal")
}

// The opt-out still works on a partial run, and still says the rows are not
// comparable — the failure channel explains a shortfall, it does not excuse one.
func TestCheckCoverage_AllowPartialStillWarnsOnAnInfrastructureShortfall(t *testing.T) {
	var warn bytes.Buffer

	require.NoError(t, checkCoverage(&warn, partialRun(), "rr.json", true))
	assert.Contains(t, warn.String(), "unmeasured case-02 ("+benchmark.CaseFailurePrepare+")",
		"the reason reaches the warn path too, not only the rejection path")
	assert.Contains(t, warn.String(), "not comparable")
}

// A row short in BOTH ways at once is the shape neither sibling test reaches, and the
// one the per-half cap is about: each half names up to maxNamedMissingCases ids and
// carries its OWN overflow count, so the two segments appear side by side each ending
// in "and N more". A joint cap, or a collapsed label, changes this message.
func TestCheckCoverage_MixedShortfallNamesBothHalvesWithTheirOwnOverflow(t *testing.T) {
	rr := benchmark.RunResult{
		Reviewers: []scorecard.PublicRecord{{Model: "m", Persona: "p"}},
		Coverage:  []benchmark.ReviewerCoverage{{Model: "m", Persona: "p"}},
	}
	// Five cases recorded as infrastructure failures, five unaccounted for.
	for i := 1; i <= 10; i++ {
		id := fmt.Sprintf("case-%02d", i)
		rr.SuiteCaseIDs = append(rr.SuiteCaseIDs, id)
		if i <= 5 {
			rr.CaseFailures = append(rr.CaseFailures,
				benchmark.CaseFailure{CaseID: id, Reason: benchmark.CaseFailurePrepare})
		}
	}

	err := checkCoverage(io.Discard, rr, "rr.json", false)

	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "missing case-06, case-07, case-08 and 2 more",
		"the unexplained half names maxNamedMissingCases ids and counts the rest: %s", msg)
	assert.Contains(t, msg,
		fmt.Sprintf("unmeasured case-01 (%[1]s), case-02 (%[1]s), case-03 (%[1]s) and 2 more", benchmark.CaseFailurePrepare),
		"and the unmeasured half does the same, independently: %s", msg)
}

// The rejection's remedy is TIER-AWARE, because on repo-state-v1 it was false.
// "Re-run the missing or unmeasured cases" presumes per-case re-running, and
// checkRepoStateFlags refuses --checkpoint for that tier — so the only re-run
// available is the entire paid suite, which the operator should know before choosing
// it over --allow-partial-coverage.
func TestCheckCoverage_RemedyNamesTheRepoStateCost(t *testing.T) {
	rr := slotFailureRun()
	rr.Suite = benchmark.FormatRepoStateV1

	err := checkCoverage(io.Discard, rr, "rr.json", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "re-running re-pays the whole suite",
		"the tier has no --checkpoint, so per-case re-running is not on offer")
}

// The standard tier keeps the plain remedy: it supports --checkpoint, so re-running
// the missing cases really is what an operator should do.
func TestCheckCoverage_RemedyStaysPlainOnStandardV1(t *testing.T) {
	rr := partialRun()
	rr.Suite = "mini"

	err := checkCoverage(io.Discard, rr, "rr.json", false)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "re-running re-pays the whole suite",
		"standard-v1 supports --checkpoint, so the repo-state caveat would be false here")
}

// --allow-partial-coverage must say what it is about to publish. A row short for
// SLOT reasons has its corroboration_rate averaged over only the cases that reviewer
// was shown (score.go's ratedCases counts r.Cases, which the skip already excluded
// them from), so the published figure is not penalised for the cases it missed and
// is not comparable to a peer averaged over the full suite. Overriding the gate
// without being told that is the "silently" half of the defect.
func TestCheckCoverage_AllowPartialWarnsAboutTheShrunkenDenominator(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, checkCoverage(&buf, slotFailureRun(), "rr.json", true))

	out := buf.String()
	assert.Contains(t, out, "m/p",
		"the warning names which reviewer's rate was computed over fewer cases")
	assert.Contains(t, out, "averaged over",
		"and states that the rate's denominator is that reviewer's own shown cases")
	assert.Contains(t, out, "not penalised",
		"and that the missed cases cost it nothing, which is why the figure reads high")
}

// A run short only for CASE-level reasons does not get the slot caveat: every
// reviewer lost the same cases, so the rows remain comparable to each other and the
// extra sentence would be noise.
func TestCheckCoverage_AllowPartialOmitsTheSlotCaveatWithoutSlotFailures(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, checkCoverage(&buf, partialRun(), "rr.json", true))

	assert.NotContains(t, buf.String(), "not penalised",
		"a case-level shortfall hits every row equally, so the comparability caveat does not apply")
}

// The repo-state caveat has THREE arms and only two were exercised. The existing
// test declares a case the suite does not contain, which produces both a missing and
// an extra and lands on the combined arm; the missing-ONLY arm — a run-result
// declaring a strict subset of the suite — substitutes the caveat for the
// shrunken-denominator claim on its own line, and that substitution was an uncovered
// added line that survived mutation.
//
// It is the arm most worth pinning: it is the one where the two possible causes
// (a truncated run, or an anchor against a different repo-state suite of the same
// version) are genuinely indistinguishable, so it is the one where asserting the
// denominator is wrong.
func TestAnchorSuiteDenominator_RepoStateSubsetDoesNotBlameTheDenominator(t *testing.T) {
	suite := writeCaseSuite(t, "first-case", "second-case")

	err := anchorSuiteDenominator(benchmark.RunResult{
		Suite:        benchmark.FormatRepoStateV1,
		SuiteVersion: "1.0.0",
		SuiteCaseIDs: []string{"first-case"}, // a strict SUBSET: missing, no extras
	}, suite, "rr.json")

	require.Error(t, err, "a short denominator must still be rejected")
	assert.Contains(t, err.Error(), "missing second-case",
		"the difference is still shown")
	assert.Contains(t, err.Error(), "not author-distinguishable",
		"the caveat must replace the denominator claim on this arm too")
	assert.NotContains(t, err.Error(), "shrunken denominator",
		"asserting the run was truncated is exactly the claim this tier cannot support")
}

// The mirror that keeps the caveat from swallowing the real diagnosis. standard-v1
// suite names ARE author-chosen, so the identity check above already proved the two
// files describe the same suite — and there the shrunken-denominator claim is
// warranted and must survive.
func TestAnchorSuiteDenominator_StandardV1SubsetStillBlamesTheDenominator(t *testing.T) {
	err := anchorSuiteDenominator(benchmark.RunResult{
		// The fixture's OWN identity, so the identity check passes and the case-set
		// check is what rejects — otherwise this asserts nothing about the arm.
		Suite:        "fixture-mini",
		SuiteVersion: "1.0.0",
		SuiteCaseIDs: []string{"case-01-nil-deref"},
	}, suiteValidPath, "rr.json")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "shrunken denominator",
		"on a tier whose suite name is distinguishing, the denominator claim is warranted")
	assert.NotContains(t, err.Error(), "not author-distinguishable",
		"and the repo-state caveat would be false here")
}

// The defence-in-depth drop for the SLOT channel, pinned through checkCoverage
// itself. Its case-level sibling has this test; without it a refactor folding the
// drop into the validator would reinstate arbitrary-prose interpolation on any
// caller that reaches checkCoverage without the export command's gate in front.
func TestCheckCoverage_DropsAnOutOfVocabularySlotReason(t *testing.T) {
	rr := slotFailureRun()
	rr.SlotFailures[0].Reason = "\x1b[2Kinjected prose"

	var buf bytes.Buffer
	err := checkCoverage(&buf, rr, "rr.json", false)

	require.Error(t, err)
	assert.Contains(t, buf.String(), "outside the failure vocabulary",
		"the drop is announced, not silent")
	assert.NotContains(t, err.Error(), "unshown case-02",
		"an unusable reason must not label the case as explained")
	assert.Contains(t, err.Error(), "missing case-02",
		"it falls back to plainly missing, which is the honest reading")
	assert.NotContains(t, buf.String(), "\x1b[2K",
		"and the injected prose is stripped before it reaches the terminal")
}

// The two blank-field arms. A partly-populated hand edit is the shape that produces
// them, and each needs its own message: the membership arm would otherwise blame the
// denominator for an entry that never identified a case or a reviewer.
func TestValidateSlotFailures_RejectsBlankFields(t *testing.T) {
	blankCase := slotFailureRun()
	blankCase.SlotFailures[0].CaseID = "   "
	err := validateSlotFailures(blankCase, "rr.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blank case_id")

	for _, tc := range []struct{ name, model, persona string }{
		{"model", "", "p"},
		{"persona", "m", ""},
	} {
		rr := slotFailureRun()
		rr.SlotFailures[0].Model, rr.SlotFailures[0].Persona = tc.model, tc.persona
		err := validateSlotFailures(rr, "rr.json")
		require.Errorf(t, err, "a blank %s names no reviewer", tc.name)
		assert.Contains(t, err.Error(), "blank model or persona")
	}
}
