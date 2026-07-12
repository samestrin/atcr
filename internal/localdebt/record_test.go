package localdebt

import (
	"testing"

	"github.com/samestrin/atcr/internal/history"
	"github.com/stretchr/testify/assert"
)

// TestRecord_StampID_MatchesHistoryFindingID locks AC 01-02 Scenario 2: the
// package must reuse history.FindingID verbatim, not reimplement or diverge from
// the shared hash construction.
func TestRecord_StampID_MatchesHistoryFindingID(t *testing.T) {
	const (
		file    = "internal/scorecard/store.go"
		line    = 89
		problem = "(Append) Concurrent writers may tear JSONL lines if writes are batched"
	)
	rec := Record{File: file, Line: line, Problem: problem}
	rec.StampID()

	assert.Equal(t, history.FindingID(file, line, problem), rec.ID,
		"Record.ID must equal history.FindingID for the same inputs (no reimplementation)")
	assert.Len(t, rec.ID, 16, "FindingID yields a 16-hex-char (8-byte) digest")
}

// TestRecord_StampID_IdenticalTripleSharesID locks AC 01-02 Scenario 1: two
// records for the same file/line/problem from different runs share one ID.
func TestRecord_StampID_IdenticalTripleSharesID(t *testing.T) {
	a := Record{File: "a.go", Line: 10, Problem: "boom", RunID: "2026-06-14T10:00:00Z-r1"}
	b := Record{File: "a.go", Line: 10, Problem: "boom", RunID: "2026-07-01T00:00:00Z-r2"}
	a.StampID()
	b.StampID()

	assert.Equal(t, a.ID, b.ID, "same file/line/problem must yield the same ID across runs")
}

// TestRecord_StampID_SeverityExcluded locks AC 01-02 Edge Case 1: severity is
// deliberately not part of the ID, so a re-settled severity keeps the same ID.
func TestRecord_StampID_SeverityExcluded(t *testing.T) {
	med := Record{File: "a.go", Line: 10, Problem: "boom", Severity: "MEDIUM"}
	high := Record{File: "a.go", Line: 10, Problem: "boom", Severity: "HIGH"}
	med.StampID()
	high.StampID()

	assert.Equal(t, med.ID, high.ID, "severity change must not change the ID")
}

// TestRecord_StampID_SymbolAnchorHashedVerbatim locks AC 01-02 Edge Case 2: the
// full problem string including a (symbolName) anchor is hashed verbatim.
func TestRecord_StampID_SymbolAnchorHashedVerbatim(t *testing.T) {
	anchored := Record{File: "a.go", Line: 10, Problem: "(Append) boom"}
	bare := Record{File: "a.go", Line: 10, Problem: "boom"}
	anchored.StampID()
	bare.StampID()

	assert.NotEqual(t, bare.ID, anchored.ID,
		"the anchor is part of problem and must be hashed verbatim (no stripping)")
	assert.Equal(t, history.FindingID("a.go", 10, "(Append) boom"), anchored.ID)
}

// TestRecord_StampID_EmptyProblemDeterministic locks AC 01-02 Error Scenario 1: an
// empty problem still yields a deterministic (non-panicking) ID.
func TestRecord_StampID_EmptyProblemDeterministic(t *testing.T) {
	rec := Record{File: "a.go", Line: 10, Problem: ""}
	assert.NotPanics(t, func() { rec.StampID() })
	assert.Equal(t, history.FindingID("a.go", 10, ""), rec.ID)
}
