package localdebt

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// --- Schema version negotiation (Sprint 30.0 v1->v2, Plan 35.13 v2->v3) ----

// TestRecord_SchemaVersionIsThree locks Plan 35.13 AC2/AC4: the package schema
// constant is bumped 2 -> 3 to accommodate the origin, occurrences, and first_seen
// fields. This is the versioning half of the forward/backward-compat contract — v3
// records become readable (r.SchemaVersion <= SchemaVersion), v4+ stay
// forward-incompatible.
func TestRecord_SchemaVersionIsThree(t *testing.T) {
	assert.Equal(t, 3, SchemaVersion,
		"SchemaVersion must be 3 after the origin/occurrences/first_seen bump")
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

// TestReadAll_SchemaV4ForwardIncompatibleSkip is a regression guard: a record
// from a newer, forward-incompatible schema (v4) is still skipped with a warning
// after the v2->v3 bump — the bump adds v3 comprehension without loosening the
// forward-incompatible-skip contract on SchemaVersion.
func TestReadAll_SchemaV4ForwardIncompatibleSkip(t *testing.T) {
	dir := t.TempDir()
	line := `{"schema_version":4,"id":"id4","run_id":"r4","ts":"2026-06-14T10:00:00Z","severity":"HIGH","file":"a.go","line":1,"problem":"p","reviewers":["security-reviewer"],"status":"wontfix"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-06.jsonl"), []byte(line+"\n"), 0o644))

	var diag bytes.Buffer
	recs, err := ReadAll(dir, ReadOpts{Writer: &diag})
	require.NoError(t, err)
	assert.Empty(t, recs, "a forward-incompatible v4 record is skipped, not returned")
	assert.Contains(t, diag.String(), "unsupported schema_version 4", "the skip must be logged")
}

// --- Plan 35.13 T1: schema v3 — origin / occurrences / first_seen ----------

// TestRecord_OriginFieldOmitempty locks AC2 (schema half): Record carries an
// Origin string tagged json:"origin,omitempty" so v1/v2 records (no origin key)
// stay backward-compatible on read and an empty origin never serializes.
func TestRecord_OriginFieldOmitempty(t *testing.T) {
	f, ok := reflect.TypeOf(Record{}).FieldByName("Origin")
	require.True(t, ok, "Record must have an Origin field")
	assert.Equal(t, "string", f.Type.String(), "Origin must be a string")
	assert.Equal(t, "origin,omitempty", f.Tag.Get("json"),
		`Origin must be tagged json:"origin,omitempty"`)
}

// TestRecord_OccurrencesAndFirstSeenFields locks AC4 (field definitions): the two
// compaction-carried fields exist with the exact types and tags T5's fold writes
// into.
func TestRecord_OccurrencesAndFirstSeenFields(t *testing.T) {
	occ, ok := reflect.TypeOf(Record{}).FieldByName("Occurrences")
	require.True(t, ok, "Record must have an Occurrences field")
	assert.Equal(t, "int", occ.Type.String(), "Occurrences must be an int")
	assert.Equal(t, "occurrences,omitempty", occ.Tag.Get("json"),
		`Occurrences must be tagged json:"occurrences,omitempty"`)

	first, ok := reflect.TypeOf(Record{}).FieldByName("FirstSeen")
	require.True(t, ok, "Record must have a FirstSeen field")
	assert.Equal(t, "string", first.Type.String(), "FirstSeen must be a string")
	assert.Equal(t, "first_seen,omitempty", first.Tag.Get("json"),
		`FirstSeen must be tagged json:"first_seen,omitempty"`)
}

// TestRecord_NoCadenceWorkflowFields is the atcr/cadence seam guard AC2 names
// explicitly. atcr's store holds review facts; sprint/epic/group workflow state is
// cadence's, keyed by Record.ID in its own overlay. This fails loudly if a later
// task smuggles cadence vocabulary into the schema.
func TestRecord_NoCadenceWorkflowFields(t *testing.T) {
	rt := reflect.TypeOf(Record{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		assert.NotEqual(t, "Group", f.Name, "Group is cadence workflow vocabulary, not a review fact")
		assert.NotEqual(t, "SourceLabel", f.Name, "SourceLabel is cadence workflow vocabulary, not a review fact")

		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		assert.NotEqual(t, "group", tag, `no field may carry the json tag "group"`)
		assert.NotEqual(t, "source_label", tag, `no field may carry the json tag "source_label"`)
	}
}

// TestRecord_OriginConstants locks AC2's closed set: origin is review|manual and
// nothing else, spelled once so no call site uses a bare literal.
func TestRecord_OriginConstants(t *testing.T) {
	assert.Equal(t, "review", OriginReview)
	assert.Equal(t, "manual", OriginManual)
}

// TestRecord_EffectiveOrigin_DefaultsToReview locks the v1/v2 default: an absent
// origin means review (reconcile was the only writer before v3). Critically, the
// accessor must NOT mutate the receiver — Compact re-marshals exactly what ReadAll
// returned, so a backfilled Origin would emit an origin key on a record whose
// schema_version still reads 1 or 2.
func TestRecord_EffectiveOrigin_DefaultsToReview(t *testing.T) {
	legacy := Record{SchemaVersion: 2}
	assert.Equal(t, OriginReview, legacy.EffectiveOrigin(),
		"an absent origin on a v1/v2 record reports review")
	assert.Equal(t, "", legacy.Origin,
		"EffectiveOrigin must not backfill the receiver's Origin field")

	manual := Record{SchemaVersion: SchemaVersion, Origin: OriginManual}
	assert.Equal(t, OriginManual, manual.EffectiveOrigin())

	messy := Record{SchemaVersion: SchemaVersion, Origin: "  MANUAL "}
	assert.Equal(t, OriginManual, messy.EffectiveOrigin(),
		"EffectiveOrigin normalizes case and surrounding whitespace")
	assert.Equal(t, "  MANUAL ", messy.Origin, "normalization must not mutate the receiver")

	blank := Record{SchemaVersion: SchemaVersion, Origin: "   "}
	assert.Equal(t, OriginReview, blank.EffectiveOrigin(),
		"a whitespace-only origin is treated as absent, not as a distinct value")
}

// TestRecord_EmptyOriginOmittedFromJSON locks the omitempty half of backward
// compatibility: a record that sets none of the v3 fields serializes without any
// of the three new keys, so the bump adds no wire-format weight to records that
// carry no v3 data.
func TestRecord_EmptyOriginOmittedFromJSON(t *testing.T) {
	b, err := json.Marshal(Record{SchemaVersion: SchemaVersion, ID: "id1", RunID: "2026-06-14T10:00:00Z-r1"})
	require.NoError(t, err)

	assert.NotContains(t, string(b), `"origin"`, "an empty Origin must be omitted")
	assert.NotContains(t, string(b), `"occurrences"`, "a zero Occurrences must be omitted")
	assert.NotContains(t, string(b), `"first_seen"`, "an empty FirstSeen must be omitted")
}

// TestReadAll_SchemaV2RecordNoV3Keys is the v2 sibling of
// TestReadAll_SchemaV1RecordNoModelKey: a v2 line carrying a model key but none of
// the three v3 keys decodes cleanly, with the new fields at their Go zero values
// and no diagnostic.
func TestReadAll_SchemaV2RecordNoV3Keys(t *testing.T) {
	dir := t.TempDir()
	line := `{"schema_version":2,"id":"id2","run_id":"r2","ts":"2026-06-14T10:00:00Z","severity":"HIGH","file":"a.go","line":1,"problem":"p","reviewers":["security-reviewer"],"model":"claude-sonnet-4-6"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-06.jsonl"), []byte(line+"\n"), 0o644))

	var diag bytes.Buffer
	recs, err := ReadAll(dir, ReadOpts{Writer: &diag})
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "claude-sonnet-4-6", recs[0].Model, "the v2 model key still decodes")
	assert.Equal(t, "", recs[0].Origin, `a v2 record decodes with Origin==""`)
	assert.Equal(t, 0, recs[0].Occurrences, "a v2 record decodes with Occurrences==0")
	assert.Equal(t, "", recs[0].FirstSeen, `a v2 record decodes with FirstSeen==""`)
	assert.Empty(t, diag.String(), "a valid v2 record must not emit any diagnostic")
}
