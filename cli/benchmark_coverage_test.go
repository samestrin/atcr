package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	for name, outcomes := range map[string]map[string]int{
		"negative count":        {"clean": -1, "findings": 2}, // sums to 1: the sum check passes it
		"out-of-vocabulary key": {"fabricated": 1},            // sums to 1: also passes
	} {
		err := checkCoverage(io.Discard, base(outcomes), "rr.json", false)
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "malformed", name)
	}

	// The legitimate vocabulary — including the "unknown" tally label — stays legal.
	err := checkCoverage(io.Discard, base(map[string]int{"unknown": 1}), "rr.json", false)
	require.NoError(t, err, "the unknown tally label is a legitimate key")
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
