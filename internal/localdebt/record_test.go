package localdebt

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/samestrin/atcr/internal/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// --- Sprint 30.0 Story 1: Model field schema bump (AC 01-02) ---------------

// TestRecord_SchemaVersionIsTwo locks AC 01-02: the package schema constant is
// bumped 1 -> 2 to accommodate the new Model attribution field. This is the
// versioning half of the forward/backward-compat contract — v2 records become
// readable (r.SchemaVersion <= SchemaVersion), v3+ stay forward-incompatible.
func TestRecord_SchemaVersionIsTwo(t *testing.T) {
	assert.Equal(t, 2, SchemaVersion, "SchemaVersion must be 2 after the Model-field bump")
}

// TestRecord_ModelFieldOmitempty locks AC 01-02: Record carries a Model string
// field tagged json:"model,omitempty" so pre-existing v1 records (no model key)
// stay backward-compatible on read and an empty model never serializes.
func TestRecord_ModelFieldOmitempty(t *testing.T) {
	f, ok := reflect.TypeOf(Record{}).FieldByName("Model")
	require.True(t, ok, "Record must have a Model field")
	assert.Equal(t, "string", f.Type.String(), "Model must be a string")
	assert.Equal(t, "model,omitempty", f.Tag.Get("json"),
		`Model must be tagged json:"model,omitempty"`)
}

// TestReadAll_SchemaV1RecordNoModelKey locks AC 01-02 Edge Case 1: a JSONL line
// with schema_version:1 and no model key decodes cleanly with Model=="" (Go zero
// value) — no error and no diagnostic warning.
func TestReadAll_SchemaV1RecordNoModelKey(t *testing.T) {
	dir := t.TempDir()
	line := `{"schema_version":1,"id":"id1","run_id":"r1","ts":"2026-06-14T10:00:00Z","severity":"HIGH","file":"a.go","line":1,"problem":"p","reviewers":["security-reviewer"],"status":"wontfix"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-06.jsonl"), []byte(line+"\n"), 0o644))

	var diag bytes.Buffer
	recs, err := ReadAll(dir, ReadOpts{Writer: &diag})
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "", recs[0].Model, `a v1 record with no model key decodes with Model==""`)
	assert.Empty(t, diag.String(), "a valid v1 record must not emit any diagnostic")
}

// TestReadAll_SchemaV3ForwardIncompatibleSkip is a regression guard: a record
// from a newer, forward-incompatible schema (v3) is still skipped with a warning
// after the v1->v2 bump — the bump adds v2 comprehension without loosening the
// forward-incompatible-skip contract on SchemaVersion.
func TestReadAll_SchemaV3ForwardIncompatibleSkip(t *testing.T) {
	dir := t.TempDir()
	line := `{"schema_version":3,"id":"id3","run_id":"r3","ts":"2026-06-14T10:00:00Z","severity":"HIGH","file":"a.go","line":1,"problem":"p","reviewers":["security-reviewer"],"status":"wontfix"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-06.jsonl"), []byte(line+"\n"), 0o644))

	var diag bytes.Buffer
	recs, err := ReadAll(dir, ReadOpts{Writer: &diag})
	require.NoError(t, err)
	assert.Empty(t, recs, "a forward-incompatible v3 record is skipped, not returned")
	assert.Contains(t, diag.String(), "unsupported schema_version 3", "the skip must be logged")
}
