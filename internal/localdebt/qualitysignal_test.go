package localdebt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Sprint 30.0 Story 1: fold-by-ID before aggregation (AC 01-04) ---------

// TestFoldByID_OpenPlusTerminalCountsOnce locks AC 01-04 Scenario 1: an open
// record and its later terminal resolution sharing one ID fold to exactly one
// terminal entry — the open record never contributes and the terminal is not
// double-counted.
func TestFoldByID_OpenPlusTerminalCountsOnce(t *testing.T) {
	open := Record{ID: "x", RunID: "r1", Timestamp: "2026-07-01T00:00:00Z",
		Reviewers: []string{"security-reviewer"}, Model: "claude-sonnet-4-6", Status: ""}
	terminal := Record{ID: "x", RunID: "r2", Timestamp: "2026-07-02T00:00:00Z",
		Reviewers: []string{"security-reviewer"}, Model: "claude-sonnet-4-6", Status: "wontfix"}

	folded := foldTerminalByID([]Record{open, terminal})
	require.Len(t, folded, 1, "open+terminal for one id folds to a single terminal entry")
	assert.Equal(t, "wontfix", folded[0].Status, "the terminal record is the one kept")
}

// TestFoldByID_DivergentTerminalRecordsResolveByPrecedence locks AC 01-04 Edge
// Case 1: two divergent terminal records for one id (resolved + wontfix) resolve
// to the higher-precedence status (wontfix outranks resolved), deterministically
// regardless of read order.
func TestFoldByID_DivergentTerminalRecordsResolveByPrecedence(t *testing.T) {
	resolved := Record{ID: "x", RunID: "r1", Timestamp: "2026-07-01T00:00:00Z",
		Reviewers: []string{"security-reviewer"}, Model: "m", Status: "resolved"}
	wontfix := Record{ID: "x", RunID: "r2", Timestamp: "2026-07-01T00:00:00Z",
		Reviewers: []string{"security-reviewer"}, Model: "m", Status: "wontfix"}

	for _, order := range [][]Record{{resolved, wontfix}, {wontfix, resolved}} {
		folded := foldTerminalByID(order)
		require.Len(t, folded, 1)
		assert.Equal(t, "wontfix", folded[0].Status,
			"wontfix must outrank resolved regardless of read order")
	}
}

// TestFoldByID_OpenOnlyContributesNothing locks AC 01-04 Edge Case 2: an id with
// only an open record (never resolved) folds to nothing — it is still-open debt,
// not yet a quality signal.
func TestFoldByID_OpenOnlyContributesNothing(t *testing.T) {
	open := Record{ID: "x", RunID: "r1", Timestamp: "2026-07-01T00:00:00Z",
		Reviewers: []string{"security-reviewer"}, Model: "m", Status: ""}

	folded := foldTerminalByID([]Record{open})
	assert.Empty(t, folded, "an id with no terminal record contributes nothing")
}

// TestFoldByID_TerminalRecordAttributionFieldsWin locks AC 01-04 Edge Case 3:
// when the open and terminal records diverge in Reviewers/Model, the terminal
// record's attribution values are the ones the fold keeps.
func TestFoldByID_TerminalRecordAttributionFieldsWin(t *testing.T) {
	open := Record{ID: "x", RunID: "r1", Timestamp: "2026-07-01T00:00:00Z",
		Reviewers: []string{"old-reviewer"}, Model: "old-model", Status: ""}
	terminal := Record{ID: "x", RunID: "r2", Timestamp: "2026-07-02T00:00:00Z",
		Reviewers: []string{"security-reviewer"}, Model: "claude-sonnet-4-6", Status: "resolved"}

	folded := foldTerminalByID([]Record{open, terminal})
	require.Len(t, folded, 1)
	assert.Equal(t, []string{"security-reviewer"}, folded[0].Reviewers,
		"the terminal record's Reviewers win")
	assert.Equal(t, "claude-sonnet-4-6", folded[0].Model,
		"the terminal record's Model wins")
}

// --- Sprint 30.0 Story 1: per-(persona, model) aggregation (AC 01-01, 01-03) --

// term builds a distinct terminal record for aggregation tests. Each call needs a
// unique id (fold is by ID) so callers pass one explicitly.
func term(id, reviewer, model, status string) Record {
	return Record{ID: id, RunID: id, Timestamp: "2026-07-01T00:00:00Z",
		Reviewers: []string{reviewer}, Model: model, Status: status}
}

// TestAggregateQualitySignal_SinglePersonaModelMixedStatuses locks AC 01-01
// Scenario 1: 2 dismissed (wontfix) + 1 confirmed (resolved) for one
// (persona, model) pair fold to a single correct QualityRow.
func TestAggregateQualitySignal_SinglePersonaModelMixedStatuses(t *testing.T) {
	recs := []Record{
		term("a", "security-reviewer", "claude-sonnet-4-6", "wontfix"),
		term("b", "security-reviewer", "claude-sonnet-4-6", "wontfix"),
		term("c", "security-reviewer", "claude-sonnet-4-6", "resolved"),
	}
	got := AggregateQualitySignal(recs)
	want := []QualityRow{{Persona: "security-reviewer", Model: "claude-sonnet-4-6", DismissedCount: 2, ConfirmedCount: 1}}
	assert.Equal(t, want, got)
}

// TestAggregateQualitySignal_MultiplePersonasAndModels locks AC 01-01 Scenario 2:
// one row per distinct (persona, model) pair, sorted persona ascending then model
// ascending.
func TestAggregateQualitySignal_MultiplePersonasAndModels(t *testing.T) {
	recs := []Record{
		term("a", "security-reviewer", "gpt-5.1", "resolved"),
		term("b", "security-reviewer", "claude-sonnet-4-6", "wontfix"),
		term("c", "perf-reviewer", "claude-sonnet-4-6", "wontfix"),
	}
	got := AggregateQualitySignal(recs)
	want := []QualityRow{
		{Persona: "perf-reviewer", Model: "claude-sonnet-4-6", DismissedCount: 1, ConfirmedCount: 0},
		{Persona: "security-reviewer", Model: "claude-sonnet-4-6", DismissedCount: 1, ConfirmedCount: 0},
		{Persona: "security-reviewer", Model: "gpt-5.1", DismissedCount: 0, ConfirmedCount: 1},
	}
	assert.Equal(t, want, got, "rows sorted persona asc, then model asc")
}

// TestAggregateQualitySignal_EmptyInputReturnsNonNilEmptySlice locks AC 01-01
// Edge Case 1: empty input returns a non-nil empty slice (never null on encode).
func TestAggregateQualitySignal_EmptyInputReturnsNonNilEmptySlice(t *testing.T) {
	got := AggregateQualitySignal(nil)
	assert.NotNil(t, got, "must be a non-nil slice for downstream JSON encoding")
	assert.Empty(t, got)
}

// TestAggregateQualitySignal_NonTerminalStatusExcluded locks AC 01-01 Edge Case
// 2: a still-open (non-terminal) record contributes to neither counter and emits
// no row.
func TestAggregateQualitySignal_NonTerminalStatusExcluded(t *testing.T) {
	recs := []Record{term("a", "security-reviewer", "claude-sonnet-4-6", "")}
	assert.Empty(t, AggregateQualitySignal(recs), "open records emit no row")
}

// TestAggregateQualitySignal_DeferredContributesNothingNoRow locks AC 01-01 Edge
// Case 2 (deferred half): a `deferred` record is terminal but is neither a
// dismissal nor a confirmation, so a (persona, model) pair whose only record is
// deferred emits no row at all. (Resolves task 1.5.A LOW deferred-contract gap.)
func TestAggregateQualitySignal_DeferredContributesNothingNoRow(t *testing.T) {
	recs := []Record{term("a", "security-reviewer", "claude-sonnet-4-6", "deferred")}
	assert.Empty(t, AggregateQualitySignal(recs),
		"a deferred-only pair contributes to neither count and emits no row")
}

// TestAggregateQualitySignal_ExcludesEmptyModelRecords locks AC 01-02: a record
// with an empty Model (a v1 record, or a v2 record whose model could not be
// resolved) is excluded from every per-model row rather than bucketed under an
// empty-string model.
func TestAggregateQualitySignal_ExcludesEmptyModelRecords(t *testing.T) {
	recs := []Record{
		term("a", "security-reviewer", "", "wontfix"),                  // empty model → excluded
		term("b", "security-reviewer", "claude-sonnet-4-6", "wontfix"), // kept
	}
	got := AggregateQualitySignal(recs)
	want := []QualityRow{{Persona: "security-reviewer", Model: "claude-sonnet-4-6", DismissedCount: 1}}
	assert.Equal(t, want, got, "empty-model records are excluded, never an empty-model bucket")
}

// TestAggregateQualitySignal_WhitespaceModelAndPersonaExcluded locks the task
// 1.9 refactor (adversarial 1.8.A): a whitespace-only Model is excluded like an
// empty one, and a whitespace-only persona entry is skipped like an empty one —
// neither forms a spurious group.
func TestAggregateQualitySignal_WhitespaceModelAndPersonaExcluded(t *testing.T) {
	recs := []Record{
		term("a", "security-reviewer", "   ", "wontfix"), // whitespace model → excluded
		{ID: "b", RunID: "b", Timestamp: "2026-07-01T00:00:00Z",
			Reviewers: []string{"  ", "security-reviewer"}, Model: "m", Status: "wontfix"},
	}
	got := AggregateQualitySignal(recs)
	want := []QualityRow{{Persona: "security-reviewer", Model: "m", DismissedCount: 1}}
	assert.Equal(t, want, got, "whitespace model excluded; whitespace persona skipped")
}

// TestAggregateQualitySignal_MultiReviewerAttributesToEveryPersona locks AC
// 01-03 Scenario 2: a multi-reviewer merged record attributes its outcome to
// every listed persona's (persona, model) group, not just Reviewers[0].
func TestAggregateQualitySignal_MultiReviewerAttributesToEveryPersona(t *testing.T) {
	recs := []Record{{ID: "a", RunID: "a", Timestamp: "2026-07-01T00:00:00Z",
		Reviewers: []string{"security-reviewer", "perf-reviewer"}, Model: "claude-sonnet-4-6", Status: "wontfix"}}
	got := AggregateQualitySignal(recs)
	want := []QualityRow{
		{Persona: "perf-reviewer", Model: "claude-sonnet-4-6", DismissedCount: 1},
		{Persona: "security-reviewer", Model: "claude-sonnet-4-6", DismissedCount: 1},
	}
	assert.Equal(t, want, got, "both listed personas receive the increment")
}

// TestAggregateQualitySignal_EmptyReviewersContributesNothing locks AC 01-03
// Edge Case 1: a record with an empty Reviewers slice cannot be attributed and
// contributes to no group.
func TestAggregateQualitySignal_EmptyReviewersContributesNothing(t *testing.T) {
	recs := []Record{{ID: "a", RunID: "a", Timestamp: "2026-07-01T00:00:00Z",
		Reviewers: nil, Model: "claude-sonnet-4-6", Status: "wontfix"}}
	assert.Empty(t, AggregateQualitySignal(recs), "empty Reviewers → no group, no empty-persona bucket")
}

// TestAggregateQualitySignal_DuplicateReviewerEntryDedupedPerRecord locks AC
// 01-03 Edge Case 2: duplicate and empty-string entries within one record's
// Reviewers are deduplicated per-record (no double-count, empty skipped).
func TestAggregateQualitySignal_DuplicateReviewerEntryDedupedPerRecord(t *testing.T) {
	recs := []Record{{ID: "a", RunID: "a", Timestamp: "2026-07-01T00:00:00Z",
		Reviewers: []string{"security-reviewer", "", "security-reviewer"}, Model: "m", Status: "wontfix"}}
	got := AggregateQualitySignal(recs)
	want := []QualityRow{{Persona: "security-reviewer", Model: "m", DismissedCount: 1}}
	assert.Equal(t, want, got, "duplicate reviewer counts once, empty entry skipped")
}

// TestAggregateQualitySignal_Idempotent locks AC 01-01 Edge Case 3: the same
// input twice produces byte-for-byte identical (order-stable) output.
func TestAggregateQualitySignal_Idempotent(t *testing.T) {
	recs := []Record{
		term("a", "security-reviewer", "gpt-5.1", "resolved"),
		term("b", "perf-reviewer", "claude-sonnet-4-6", "wontfix"),
	}
	assert.Equal(t, AggregateQualitySignal(recs), AggregateQualitySignal(recs))
}
