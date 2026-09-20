package scorecard

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The outcome vocabulary is spelled as plain string literals throughout this
// file rather than as benchmark.Outcome* constants, and that is forced rather
// than chosen: internal/benchmark imports internal/scorecard (benchmark.go:37,
// score.go:8, score_repostate.go:6, vocabulary.go:6), so importing it back —
// from the package OR from an in-package test file, and every *_test.go here is
// in-package — closes an import cycle. See sprint 36.0 clarification C5.
//
// The agreement between these literals and internal/benchmark/outcome.go's
// constants is therefore NOT pinned here. It is pinned in cli/, which legally
// imports both. If that test is ever deleted, this file silently stops
// describing the real vocabulary.
const (
	testOutcomeUnknown     = ""
	testOutcomeFindings    = "findings"
	testOutcomeClean       = "clean"
	testOutcomeUnparseable = "unparseable"
	testOutcomeTruncated   = "truncated"
	testOutcomeIncomplete  = "incomplete"
	testOutcomeUngrounded  = "ungrounded"
	testOutcomeFiltered    = "filtered"
	testOutcomeFailed      = "failed"
)

// allTestOutcomes is the full nine-value vocabulary, unknown first.
var allTestOutcomes = []string{
	testOutcomeUnknown,
	testOutcomeFindings,
	testOutcomeClean,
	testOutcomeUnparseable,
	testOutcomeTruncated,
	testOutcomeIncomplete,
	testOutcomeUngrounded,
	testOutcomeFiltered,
	testOutcomeFailed,
}

// TestSchemaVersion_IsTwo pins the ONE bump sprint 36.0 makes. D1 gives Phase 2
// sole ownership of it and D8 states outright that Phase 4's additive
// weighted_credit field does NOT bump again, so a third value here means some
// later phase incremented it a second time.
func TestSchemaVersion_IsTwo(t *testing.T) {
	assert.Equal(t, 2, SchemaVersion,
		"Phase 2 owns the single 1->2 bump carrying outcome, category and categories_raised")
}

// TestRecord_OutcomeRoundTrips covers every value in the vocabulary, not just
// the four eligible ones: an ineligible outcome still has to survive the store
// intact, because operational health reporting reads it even though trust
// scoring skips it.
func TestRecord_OutcomeRoundTrips(t *testing.T) {
	for _, want := range allTestOutcomes {
		t.Run("outcome="+want, func(t *testing.T) {
			b, err := json.Marshal(Record{SchemaVersion: SchemaVersion, Outcome: want})
			require.NoError(t, err)

			var got Record
			require.NoError(t, json.Unmarshal(b, &got))
			assert.Equal(t, want, got.Outcome)
		})
	}
}

// TestRecord_SchemaOneFixtureDecodesToUnknownOutcome is the backward-direction
// half of AC 02-01 Edge Case 1b. store.go's read gate is strictly-greater with
// no lower bound, so a v1 record still passes it; this pins that it decodes to
// unknown rather than to any inferred classification. "clean" is the dangerous
// one — it would assert a successful review that nobody ever classified.
func TestRecord_SchemaOneFixtureDecodesToUnknownOutcome(t *testing.T) {
	// Captured verbatim from a pre-sprint-36.0 store: no outcome key at all.
	const v1Line = `{"schema_version":1,"record_type":"reviewer","run_id":"r1",` +
		`"reviewer":"vera","model":"opus","role":"reviewer","findings_raised":3,` +
		`"findings_corroborated":2,"findings_solo":1,"corroboration_rate":0.6667,` +
		`"cost_usd":0,"tokens_in":0,"tokens_out":0,"latency_ms":0}`

	var got Record
	require.NoError(t, json.Unmarshal([]byte(v1Line), &got))

	assert.Equal(t, 1, got.SchemaVersion)
	assert.Equal(t, testOutcomeUnknown, got.Outcome)
	assert.NotEqual(t, testOutcomeClean, got.Outcome,
		"an unclassified record must never read as a successful empty review")
	assert.Empty(t, got.CategoriesRaised, "absent means not measured, not measured-empty")
}

// TestRecord_OutcomeIsOmittedWhenUnknown pins C7. An unknown outcome writes no
// key, so a v2 record with nothing to say is byte-identical on that field to
// the v1 records already in the store — which is what keeps the two eras
// readable by one decoder.
func TestRecord_OutcomeIsOmittedWhenUnknown(t *testing.T) {
	b, err := json.Marshal(Record{SchemaVersion: SchemaVersion, Outcome: testOutcomeUnknown})
	require.NoError(t, err)
	assert.NotContains(t, string(b), `"outcome"`)

	b, err = json.Marshal(Record{SchemaVersion: SchemaVersion, Outcome: testOutcomeClean})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"outcome":"clean"`)
}

// TestRecord_CategoriesRaisedRoundTrips covers the third field the single bump
// carries. Phase 2 only declares it; Phase 3 threads its value.
func TestRecord_CategoriesRaisedRoundTrips(t *testing.T) {
	b, err := json.Marshal(Record{
		SchemaVersion:    SchemaVersion,
		CategoriesRaised: []string{"api-contract", "contract"},
	})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"categories_raised":["api-contract","contract"]`)

	var got Record
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, []string{"api-contract", "contract"}, got.CategoriesRaised)
}

// TestRecord_OutOfVocabularyOutcomeDecodesWithoutError is AC 02-01's Security
// note: a hand-edited or corrupted store line must not panic or error the read
// path. It decodes as a plain string and is excluded downstream by the
// eligibility filter's allowlist, so there is no crash route for garbage.
func TestRecord_OutOfVocabularyOutcomeDecodesWithoutError(t *testing.T) {
	var got Record
	require.NoError(t, json.Unmarshal(
		[]byte(`{"schema_version":2,"record_type":"reviewer","outcome":"banana"}`), &got))
	assert.Equal(t, "banana", got.Outcome)
}
