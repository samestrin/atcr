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

	"github.com/samestrin/atcr/internal/fanout"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deriveConstValues parses the named file in this package and returns the wire value
// of every constant whose name carries the given prefix.
//
// Derivation rather than a hand-written list, for the reason deriveCaseFailureReasons
// states at length: a restated list can only drift from the switch it mirrors, and a
// new constant added without a matching validator arm then passes every test while
// the producer writes a reason the export boundary rejects — turning a legitimate
// paid run into a permanently unexportable artifact.
//
// Located via runtime.Caller so it is independent of the test runner's cwd, and
// PARSED rather than grepped so a comment mentioning the prefix cannot invent a
// vocabulary member.
func deriveConstValues(t *testing.T, filename, prefix string) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	src, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), filename))
	require.NoError(t, err)
	file, err := parser.ParseFile(token.NewFileSet(), filename, src, parser.ParseComments)
	require.NoError(t, err)

	var values []string
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
				if !strings.HasPrefix(name.Name, prefix) {
					continue
				}
				if i < len(vs.Values) {
					lit, ok := vs.Values[i].(*ast.BasicLit)
					require.True(t, ok,
						"%s constant %s must be declared as a string literal", prefix, name.Name)
					v, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					lastValue = v
				} else if lastValue == "" {
					require.FailNow(t, "constant with no string value", "%s has no value", name.Name)
				}
				values = append(values, lastValue)
			}
		}
		return true
	})
	require.NotEmpty(t, values,
		"derived an empty vocabulary from %s — the parser stopped seeing the const block", filename)
	return values
}

// ValidSlotFailureReason is the export trust boundary for the per-slot channel. It
// admits exactly the SlotFailure* wire values and nothing else.
func TestValidSlotFailureReason(t *testing.T) {
	// DERIVED: every SlotFailure* constant the source declares must be admitted by
	// the switch. A constant added without a matching arm fails here instead of
	// passing silently and reaching the runner.
	for _, v := range deriveConstValues(t, "slot_failure.go", "SlotFailure") {
		assert.Truef(t, ValidSlotFailureReason(v),
			"declared SlotFailure* value %q must satisfy ValidSlotFailureReason — "+
				"a constant without a switch arm makes the runner write a reason export rejects", v)
	}

	for _, bad := range []string{"", "ok", "failed", "timeout", "unknown", "prepare", "nonsense"} {
		assert.Falsef(t, ValidSlotFailureReason(bad),
			"%q must be rejected: the empty string cannot have come from the producer, and every "+
				"other value here belongs to a different vocabulary", bad)
	}
}

// THE THREE VOCABULARIES MUST BE PAIRWISE DISJOINT.
//
// All three appear in one run-result document and describe different things — what a
// slot produced (Outcome*), a case no reviewer was shown (CaseFailure*), and one
// reviewer missing one case (SlotFailure*). A shared spelling would invite exactly
// the conflation the separation exists to prevent, and is the reason SlotFailure's
// values are namespaced ("call_failed") rather than mirroring fanout's bare status
// strings, one of which is already OutcomeFailed.
//
// Checked in BOTH directions per vocabulary, so a constant added to either side
// without a validator arm cannot slip a collision through on the unwalked side.
func TestSlotFailureReasonsDoNotCollide(t *testing.T) {
	slotReasons := deriveConstValues(t, "slot_failure.go", "SlotFailure")
	caseReasons := deriveConstValues(t, "case_failure.go", "CaseFailure")

	for _, v := range slotReasons {
		assert.Falsef(t, ValidOutcome(v),
			"SlotFailure* value %q collides with the outcome vocabulary", v)
		assert.NotEqualf(t, OutcomeUnknownLabel, v,
			"SlotFailure* value %q collides with the outcome tally label", v)
		assert.Falsef(t, ValidCaseFailureReason(v),
			"SlotFailure* value %q collides with the case-failure vocabulary", v)
	}

	// The reverse directions: ask ValidSlotFailureReason about every value the other
	// two vocabularies declare.
	for _, v := range caseReasons {
		assert.Falsef(t, ValidSlotFailureReason(v),
			"CaseFailure* value %q collides with the slot-failure vocabulary", v)
	}
	for _, v := range []string{
		OutcomeUnknown, OutcomeFindings, OutcomeClean, OutcomeUnparseable,
		OutcomeTruncated, OutcomeIncomplete, OutcomeUngrounded, OutcomeFiltered,
		OutcomeFailed, OutcomeUnknownLabel,
	} {
		assert.Falsef(t, ValidSlotFailureReason(v),
			"Outcome value %q collides with the slot-failure vocabulary", v)
	}
}

// SlotFailureReasonForStatus is TOTAL: every fanout status maps to a reason, and an
// unrecognized one maps to its own value rather than being misattributed as a
// transport failure.
func TestSlotFailureReasonForStatus(t *testing.T) {
	assert.Equal(t, SlotFailureCall, SlotFailureReasonForStatus("failed"))
	assert.Equal(t, SlotFailureTimeout, SlotFailureReasonForStatus("timeout"))
	assert.Equal(t, SlotFailureUnknownStatus, SlotFailureReasonForStatus("some_future_status"),
		"a status this build does not know must not be asserted as a failed CALL — "+
			"the reason would be legal at the export gate and wrong")
	assert.Equal(t, SlotFailureUnknownStatus, SlotFailureReasonForStatus(""),
		"an empty status is not a known one")

	// Whatever it returns is storable, so the runner can never write a reason the
	// export boundary rejects.
	for _, s := range []string{"failed", "timeout", "some_future_status", "", "ok"} {
		assert.Truef(t, ValidSlotFailureReason(SlotFailureReasonForStatus(s)),
			"the mapping must only ever produce a storable reason (status %q)", s)
	}
}

// The mapping above matches fanout's statuses by STRING LITERAL, so nothing in
// this package alone would notice a rename of fanout.StatusFailed/StatusTimeout:
// the switch would silently fall through to SlotFailureUnknownStatus — a legal
// value at the export gate — and every failed or timed-out slot would be recorded
// as an unknown status. Asserting through fanout's CONSTANTS pins the two
// spellings together: a rename on either side breaks this test instead of the
// export. (fanout does not import benchmark, so the test-only import is
// cycle-free; the runner-side end-to-end pin lives in cli.)
func TestSlotFailureReasonForStatus_TracksFanoutStatusConstants(t *testing.T) {
	assert.Equal(t, SlotFailureCall, SlotFailureReasonForStatus(fanout.StatusFailed),
		"the literal in SlotFailureReasonForStatus must track fanout.StatusFailed")
	assert.Equal(t, SlotFailureTimeout, SlotFailureReasonForStatus(fanout.StatusTimeout),
		"the literal in SlotFailureReasonForStatus must track fanout.StatusTimeout")
}

// The public submission must not carry slot failures — the same exclusion
// case_failures carries, for the same reason. A published shortfall states how much
// was skipped, never why.
//
// Reverse the decision HERE first, with a submission_schema bump, never as a silent
// schema change.
func TestBuildSubmission_DoesNotPublishSlotFailures(t *testing.T) {
	data, err := json.Marshal(BuildSubmission(RunResult{
		Suite:        "s",
		SuiteVersion: "1",
		SlotFailures: []SlotFailure{
			{Model: "m", Persona: "p", CaseID: "case-02", Reason: SlotFailureTimeout},
		},
	}, time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, err)

	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))
	assert.NotContains(t, back, "slot_failures",
		"the public submission schema must not carry slot-failure reasons — they explain "+
			"a shortfall to the operator and answer no question the board scores")
}

// The channel serializes under its own key, and a run with no slot failure omits it
// entirely — so such a run-result is byte-identical to one written before the field
// existed, the omitempty contract every sibling diagnostic carries.
func TestRunResultSlotFailuresRoundTrip(t *testing.T) {
	clean, err := json.Marshal(RunResult{Suite: "s", SuiteVersion: "1"})
	require.NoError(t, err)
	assert.NotContains(t, string(clean), "slot_failures",
		"a run with no slot failure must omit the key")

	partial, err := json.Marshal(RunResult{
		Suite:        "s",
		SuiteVersion: "1",
		SlotFailures: []SlotFailure{
			{Model: "m-otto", Persona: "otto", CaseID: "case-02", Reason: SlotFailureCall},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, string(partial),
		`"slot_failures":[{"model":"m-otto","persona":"otto","case_id":"case-02","reason":"call_failed"}]`)

	var back RunResult
	require.NoError(t, json.Unmarshal(partial, &back))
	require.Len(t, back.SlotFailures, 1)
	assert.Equal(t, "m-otto", back.SlotFailures[0].Model)
	assert.Equal(t, "otto", back.SlotFailures[0].Persona)
	assert.Equal(t, "case-02", back.SlotFailures[0].CaseID)
	assert.Equal(t, SlotFailureCall, back.SlotFailures[0].Reason)
}
