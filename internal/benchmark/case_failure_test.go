package benchmark

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deriveCaseFailureReasons parses case_failure.go and returns every CaseFailure*
// constant's wire value, so the tests below assert against what the vocabulary
// ACTUALLY declares rather than against a hand-written copy of it. A hand-written
// list restates the switch in case_failure.go and can only drift with it: adding a
// seventh CaseFailure* constant for a seventh record-and-continue site without
// touching ValidCaseFailureReason's switch used to pass every test unchanged, and
// the runner would then write a reason validateCaseFailures rejects — turning a
// legitimate partial run into a permanently unexportable artifact after the panel
// was paid for. Deriving the list closes that: a new constant appears here whether
// or not anyone remembered the switch, and the validity assertion below fails
// until the switch admits it.
//
// The file is located relative to this test file (runtime.Caller), not the
// process working directory, so the derivation holds under any test runner cwd.
// Parsing rather than grepping means a comment mentioning "CaseFailure" or a
// string elsewhere in the file cannot invent a vocabulary member.
func deriveCaseFailureReasons(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	src, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "case_failure.go"))
	require.NoError(t, err)
	file, err := parser.ParseFile(token.NewFileSet(), "case_failure.go", src, parser.ParseComments)
	require.NoError(t, err)

	var reasons []string
	lastValue := ""
	ast.Inspect(file, func(n ast.Node) bool {
		decl, ok := n.(*ast.GenDecl)
		if !ok || decl.Tok != token.CONST {
			return true
		}
		for _, spec := range decl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "CaseFailure") {
					continue
				}
				if i < len(vs.Values) {
					lit, ok := vs.Values[i].(*ast.BasicLit)
					require.True(t, ok,
						"CaseFailure* constant %s must be declared as a string literal", name.Name)
					v, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					lastValue = v
				} else if lastValue == "" {
					// A CaseFailure* name with no own value and no preceding value
					// has no wire spelling to validate — fail closed rather than
					// silently skipping a declared vocabulary member.
					require.FailNow(t, "CaseFailure* constant %s has no string value", name.Name)
				}
				// No own literal: the name repeats the preceding constant's value
				// (Go const-block semantics), which lastValue already carries.
				reasons = append(reasons, lastValue)
			}
		}
		return true
	})
	require.NotEmpty(t, reasons,
		"derived an empty failure vocabulary — the parser stopped seeing the const block")
	return reasons
}

// ValidCaseFailureReason is the EXPORT trust boundary for the per-case failure
// channel, the same role ValidOutcome plays for the per-slot outcome vocabulary. It
// admits exactly the CaseFailure* wire values and nothing else.
//
// The empty string is REJECTED here, which is the one place this vocabulary
// deliberately parts company with ValidOutcome. OutcomeUnknown is the empty string
// because a checkpoint written before that field existed must decode into a
// representable absence; case_failures is a new array whose every element is written
// by executeRepoStateBenchmarkRun with a reason, so an element carrying no reason
// cannot have come from the producer at all.
func TestValidCaseFailureReason(t *testing.T) {
	// The positive arm is DERIVED, not restated: every CaseFailure* constant the
	// source declares must be admitted by the switch. A constant added without a
	// matching switch arm surfaces here as a failure instead of passing silently.
	for _, v := range deriveCaseFailureReasons(t) {
		assert.True(t, ValidCaseFailureReason(v), "wire value %q must be valid", v)
	}
	for _, v := range []string{
		"",        // a failure record always carries a reason; absence is not representable
		"unknown", // no tally label exists for this vocabulary
		"MATERIALIZE", "materialize ", "fabricated", "failed",
	} {
		assert.False(t, ValidCaseFailureReason(v), "%q is not a storable failure reason", v)
	}
}

// The failure vocabulary must not collide with the OUTCOME vocabulary. Both appear
// in the same run-result document, one describing a CASE and the other describing a
// reviewer SLOT, and a shared spelling would invite a reader to fold two axes that
// mean different things — the exact conflation Strategy item 1 rejected when it
// declined to add a new Outcome* value for this.
func TestCaseFailureReasonsDoNotCollideWithOutcomes(t *testing.T) {
	// Same derivation as TestValidCaseFailureReason: the collision check must see
	// every declared failure reason, not the six somebody happened to copy here.
	for _, r := range deriveCaseFailureReasons(t) {
		assert.False(t, ValidOutcome(r), "failure reason %q must not also be an outcome value", r)
		assert.NotEqual(t, OutcomeUnknownLabel, r, "failure reason %q must not be the outcome tally label", r)
	}
}

// The collision check runs in BOTH directions. TestCaseFailureReasonsDoNotCollide
// WithOutcomes above walks the failure vocabulary and asks whether each reason is
// also an outcome; this test walks the OUTCOME vocabulary and asks whether any of
// its values would be accepted as a failure reason. The two directions are not
// equivalent: the existing direction routes through ValidOutcome, so an Outcome*
// constant added WITHOUT a place in ValidOutcome's vocabulary (AllOutcomes, which
// it ranges over) could share a
// spelling with a CaseFailure* value and no existing assertion would notice. This
// test asks ValidCaseFailureReason directly, so a colliding spelling fails here
// regardless of whether the outcome side admits it.
func TestOutcomesDoNotCollideWithCaseFailureReasons(t *testing.T) {
	for _, o := range []string{
		OutcomeUnknown, OutcomeFindings, OutcomeClean, OutcomeUnparseable,
		OutcomeTruncated, OutcomeIncomplete, OutcomeUngrounded, OutcomeFiltered, OutcomeFailed,
		OutcomeUnknownLabel,
	} {
		assert.False(t, ValidCaseFailureReason(o),
			"outcome value %q must not also be a storable failure reason", o)
	}
}

// The epic Clarifications for 35.16.10.1 exclude carrying case_failures into the
// public Submission — the field is run-result-only, like Vocabulary. This arm locks
// that DECISION at the same seam the run-result-side round-trip above locks the wire
// format: a Submission marshalled from a run-result WITH recorded failures must not
// grow a case_failures key.
//
// It exists because the advisory text and the field comment both promise the
// published artifact's shape, and a future "while we're here" field addition would
// silently make both promises false: a submission published with
// --allow-partial-coverage would then carry the full suite_case_ids and short
// reviewer_coverage rows WITH failure reasons attached, distinguishing an
// infrastructure failure from a truncated or cherry-picked run — exactly the
// provenance claim the Clarifications declined to publish. If that decision is ever
// reversed, it must reverse HERE first (this test fails), then in the comment and
// the advisory, with a submission_schema bump — never as a silent schema change.
func TestBuildSubmission_DoesNotPublishCaseFailures(t *testing.T) {
	data, err := json.Marshal(BuildSubmission(RunResult{
		Suite:        "s",
		SuiteVersion: "1",
		CaseFailures: []CaseFailure{{CaseID: "case-02", Reason: CaseFailurePrepare}},
	}, time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, err)

	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))
	assert.NotContains(t, back, "case_failures",
		"the public submission schema must not carry failure reasons — a published "+
			"partial-coverage shortfall states HOW MUCH was skipped, never WHY")
}

// The channel serializes under its own key, and an unfailed run omits it entirely —
// so a clean run-result is byte-identical to one written before the field existed,
// the same omitempty contract SuiteCaseIDs and Vocabulary carry.
func TestRunResultCaseFailuresRoundTrip(t *testing.T) {
	clean, err := json.Marshal(RunResult{Suite: "s", SuiteVersion: "1"})
	require.NoError(t, err)
	assert.NotContains(t, string(clean), "case_failures", "a run with no failures must omit the key")

	partial, err := json.Marshal(RunResult{
		Suite:        "s",
		SuiteVersion: "1",
		CaseFailures: []CaseFailure{{CaseID: "case-02", Reason: CaseFailurePrepare}},
	})
	require.NoError(t, err)
	assert.Contains(t, string(partial), `"case_failures":[{"case_id":"case-02","reason":"prepare"}]`)

	var back RunResult
	require.NoError(t, json.Unmarshal(partial, &back))
	require.Len(t, back.CaseFailures, 1)
	assert.Equal(t, "case-02", back.CaseFailures[0].CaseID)
	assert.Equal(t, CaseFailurePrepare, back.CaseFailures[0].Reason)
}

// caseFailureConstDoc returns the doc comment text of the named CaseFailure*
// constant, parsed from case_failure.go rather than hand-copied, so the assertion
// below tracks the comment the vocabulary ACTUALLY ships.
func caseFailureConstDoc(t *testing.T, constName string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	src, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "case_failure.go"))
	require.NoError(t, err)
	file, err := parser.ParseFile(token.NewFileSet(), "case_failure.go", src, parser.ParseComments)
	require.NoError(t, err)

	var doc string
	ast.Inspect(file, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) == 0 || vs.Names[0].Name != constName {
			return true
		}
		if vs.Doc != nil {
			doc = vs.Doc.Text()
		}
		return false
	})
	require.NotEmpty(t, doc, "no doc comment found for %s — the guard asserts the comment, so it must exist", constName)
	return doc
}

// CaseFailureExecute's doc promises a one-to-one mapping from reason to failure
// site, but the empty-roster abort beside that site was named only in an inline
// comment in cli/benchmark_repostate.go and a docs table row — the vocabulary doc
// itself never names fanout.ErrEmptyRoster, and never says WHERE the empty-roster
// abort lands. That matters because the runner aborts on ErrEmptyRoster at PREPARE
// (validateReviewRequest raises the sentinel inside PrepareReview) and keeps its
// execute-side arm only defensively, so a reader trusting the doc alone would look
// for the abort in the wrong stage and could "fix" the split backwards.
//
// The guard is parsed-not-grepped, like deriveCaseFailureReasons above: the doc must
// name the sentinel and state the prepare-side abort, so the vocabulary doc and the
// runner's split cannot drift apart silently.
func TestCaseFailureExecuteDocNamesEmptyRoster(t *testing.T) {
	doc := caseFailureConstDoc(t, "CaseFailureExecute")

	assert.Contains(t, doc, "ErrEmptyRoster",
		"CaseFailureExecute's doc must name fanout.ErrEmptyRoster explicitly — an "+
			"unnamed sentinel is the drift this guard exists to prevent")
	assert.Contains(t, doc, "prepare",
		"CaseFailureExecute's doc must state that an empty roster aborts at PREPARE "+
			"rather than being recorded at execute")
}
