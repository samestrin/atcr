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
