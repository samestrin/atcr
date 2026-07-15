// Package report SARIF-formatter tests. Covers format registration (AC 01-01),
// base document structure (AC 01-02), and rules-array/category linkage (AC 01-03).
// Severity mapping (AC 02-01) and line/file anchoring (AC 03-01/03-02) tests are
// added in Phase 2 below their respective helpers.
package report

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderSarifString is a small helper: render findings to SARIF and return the
// raw string, failing the test on any render error.
func renderSarifString(t *testing.T, findings []reconcile.JSONFinding) string {
	t.Helper()
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatSarif))
	return b.String()
}

// unmarshalSarif renders findings and unmarshals the output into the local
// struct tree, asserting the bytes are syntactically valid JSON.
func unmarshalSarif(t *testing.T, findings []reconcile.JSONFinding) sarifLog {
	t.Helper()
	out := renderSarifString(t, findings)
	require.True(t, json.Valid([]byte(out)), "renderSarif output must be valid JSON")
	var doc sarifLog
	require.NoError(t, json.Unmarshal([]byte(out), &doc))
	return doc
}

// sampleSarif extends sample() with a file-level finding (Line == 0) in a third
// distinct category, so the SARIF golden exercises the synthesized 1,1,1,1 fallback
// region and a 3-entry rules[] end-to-end. Kept separate from sample() (which is
// pinned to "Total findings: 2" by the md/json/checklist goldens).
func sampleSarif() []reconcile.JSONFinding {
	return append(sample(), reconcile.JSONFinding{
		Severity: "MEDIUM", File: "internal/report/render.go", Line: 0,
		Problem: "package-level concern with no specific line", Category: "architecture",
		Reviewers: []string{"greta"}, Confidence: "HIGH",
	})
}

// --- AC 01-01: Format Constant Registration ---

func TestSarif_FormatRegistration(t *testing.T) {
	// Scenario 1 + Edge Case 1: sarif is a valid, case-sensitive format token.
	assert.True(t, ValidFormat("sarif"))
	assert.False(t, ValidFormat("SARIF"))

	// Scenario 3: Formats() enumerates sarif alongside the other three.
	formats := Formats()
	for _, want := range []string{"md", "json", "checklist", "sarif"} {
		assert.Containsf(t, formats, want, "Formats() must list %q", want)
	}

	// Scenario 2: Render dispatches FormatSarif to renderSarif without error and
	// does not fall through to the unknown-format backstop.
	var b strings.Builder
	require.NoError(t, Render(&b, sample(), FormatSarif))
	assert.Contains(t, b.String(), `"version": "2.1.0"`)

	// Edge Case 2 / Error Scenario 1: an unknown format lists sarif in the error.
	err := Render(&strings.Builder{}, sample(), "bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sarif")
}

// --- AC 01-02: Base Document Structure ---

func TestSarif_DocumentShape(t *testing.T) {
	// Scenario 1: a non-empty findings slice produces a valid top-level document.
	doc := unmarshalSarif(t, sample())
	assert.Equal(t, sarifSchemaURI, doc.Schema)
	assert.Equal(t, "2.1.0", doc.Version)
	require.Len(t, doc.Runs, 1)
	assert.Equal(t, "atcr", doc.Runs[0].Tool.Driver.Name)
	assert.Len(t, doc.Runs[0].Results, 2)
}

func TestSarif_EmptyAndNilFindings(t *testing.T) {
	// Scenario 2 + Edge Case 1: empty/nil findings still produce a structurally
	// valid document whose results[] and rules[] serialize as [] — never null.
	for _, name := range []string{"nil", "empty"} {
		t.Run(name, func(t *testing.T) {
			var findings []reconcile.JSONFinding
			if name == "empty" {
				findings = []reconcile.JSONFinding{}
			}
			out := renderSarifString(t, findings)
			assert.Contains(t, out, `"results": []`, "results must serialize as [] not null")
			assert.Contains(t, out, `"rules": []`, "rules must serialize as [] not null")
			assert.NotContains(t, out, "null")

			doc := unmarshalSarif(t, findings)
			assert.Equal(t, "2.1.0", doc.Version)
			assert.Equal(t, "atcr", doc.Runs[0].Tool.Driver.Name)
			assert.NotNil(t, doc.Runs[0].Results)
			assert.Empty(t, doc.Runs[0].Results)
		})
	}
}

func TestSarif_MessageTextNeverEmpty(t *testing.T) {
	// Edge Case 3: a single finding with all optional fields empty still renders
	// exactly one result whose message.text is a non-empty fallback string.
	findings := []reconcile.JSONFinding{{Severity: "LOW", File: "x.go", Line: 1, Category: "misc"}}
	doc := unmarshalSarif(t, findings)
	require.Len(t, doc.Runs[0].Results, 1)
	assert.NotEmpty(t, doc.Runs[0].Results[0].Message.Text)
}

func TestSarif_Deterministic(t *testing.T) {
	// Edge Case 2: repeated calls with identical input are byte-identical.
	assert.Equal(t, renderSarifString(t, sample()), renderSarifString(t, sample()))
}

// errWriter always fails on Write, for the write-error propagation path.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

func TestSarif_WriteErrorPropagates(t *testing.T) {
	// Error Scenario 1: a failing writer surfaces the error, no panic.
	err := Render(errWriter{}, sample(), FormatSarif)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

// --- AC 01-03: Rules Array / Category Linkage ---

func TestSarif_RulesDedupFirstSeenOrder(t *testing.T) {
	// Scenario 1: one rule per distinct Category, first-seen (not alphabetical) order.
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Category: "security"},
		{Severity: "LOW", File: "b.go", Line: 2, Problem: "p", Category: "style"},
		{Severity: "HIGH", File: "c.go", Line: 3, Problem: "p", Category: "security"},
	}
	doc := unmarshalSarif(t, findings)
	rules := doc.Runs[0].Tool.Driver.Rules
	require.Len(t, rules, 2)
	assert.Equal(t, "security", rules[0].ID)
	assert.Equal(t, "style", rules[1].ID)

	// Scenario 2: description content is category-generic, never finding-specific.
	assert.Equal(t, "security", rules[0].ShortDescription.Text)
	assert.Contains(t, rules[0].FullDescription.Text, "security")
	assert.NotContains(t, rules[0].FullDescription.Text, "p") // no Problem/Fix leakage

	// Scenario 3: referential integrity — every result.ruleId matches a declared rule id.
	ids := map[string]bool{rules[0].ID: true, rules[1].ID: true}
	for _, r := range doc.Runs[0].Results {
		assert.Truef(t, ids[r.RuleID], "result ruleId %q has no matching rule", r.RuleID)
	}
}

func TestSarif_RulesEmptyCategory(t *testing.T) {
	// Edge Case 2: an empty Category is one distinct value → one rule with id "".
	findings := []reconcile.JSONFinding{{Severity: "LOW", File: "x.go", Line: 1, Problem: "p", Category: ""}}
	doc := unmarshalSarif(t, findings)
	require.Len(t, doc.Runs[0].Tool.Driver.Rules, 1)
	assert.Equal(t, "", doc.Runs[0].Tool.Driver.Rules[0].ID)
	require.Len(t, doc.Runs[0].Results, 1)
	assert.Equal(t, "", doc.Runs[0].Results[0].RuleID)
}

func TestSarif_RulesSingleCategoryRepeated(t *testing.T) {
	// Edge Case 3: five findings sharing a category produce exactly one rule.
	findings := make([]reconcile.JSONFinding, 5)
	for i := range findings {
		findings[i] = reconcile.JSONFinding{Severity: "HIGH", File: "a.go", Line: i + 1, Problem: "p", Category: "security"}
	}
	doc := unmarshalSarif(t, findings)
	require.Len(t, doc.Runs[0].Tool.Driver.Rules, 1)
	assert.Len(t, doc.Runs[0].Results, 5)
	for _, r := range doc.Runs[0].Results {
		assert.Equal(t, "security", r.RuleID)
	}
}

// --- AC 02-01: Severity-to-SARIF-Level Mapping (Phase 2) ---

func TestSarifLevel(t *testing.T) {
	cases := []struct {
		name     string
		severity string
		want     string
	}{
		{"critical", "CRITICAL", "error"},
		{"high", "HIGH", "error"},
		{"medium", "MEDIUM", "warning"},
		{"low", "LOW", "note"},
		{"lowercase-critical", "critical", "error"},
		{"mixedcase-high", "High", "error"},
		{"mixedcase-medium", "mEdIuM", "warning"},
		{"lowercase-low", "low", "note"},
		{"padded-high", "  HIGH  ", "error"},
		{"padded-low", "\tLOW\n", "note"},
		{"empty", "", "warning"},
		{"bogus", "BOGUS", "warning"},
		{"unknown", "UNKNOWN", "warning"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sarifLevel(tc.severity)
			assert.Equal(t, tc.want, got)
			// Edge Case 5: only the three GitHub-supported levels are ever returned.
			assert.Contains(t, []string{"error", "warning", "note"}, got)
			assert.NotEqual(t, "none", got)
			assert.NotEmpty(t, got)
		})
	}
}

// Scenario 5: renderSarif populates every result.level via sarifLevel — no other
// severity comparison exists in sarif.go (that single-call-site property is also
// checked by inspection in task 2.3).
func TestSarif_ResultLevelMatchesSarifLevel(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "CRITICAL", File: "a.go", Line: 1, Problem: "p", Category: "c1"},
		{Severity: "MEDIUM", File: "b.go", Line: 2, Problem: "p", Category: "c2"},
		{Severity: "LOW", File: "c.go", Line: 3, Problem: "p", Category: "c3"},
		{Severity: "weird", File: "d.go", Line: 4, Problem: "p", Category: "c4"},
	}
	doc := unmarshalSarif(t, findings)
	require.Len(t, doc.Runs[0].Results, len(findings))
	for i, f := range findings {
		assert.Equal(t, sarifLevel(f.Severity), doc.Runs[0].Results[i].Level)
	}
}

// --- AC 03-01 / 03-02: Line-Level and File-Level Fallback Anchoring (Phase 2) ---

func TestSarifLocation(t *testing.T) {
	cases := []struct {
		name                                         string
		file                                         string
		line                                         int
		wantURI                                      string
		wantStart, wantStartCol, wantEnd, wantEndCol int
	}{
		// AC 03-01: Line > 0 anchors to the real line, columns synthesized to 1,2
		// (endColumn is exclusive in SARIF 2.1.0, so startColumn==endColumn would be
		// a zero-length region).
		{"line-1-boundary", "internal/report/sarif.go", 1, "internal/report/sarif.go", 1, 1, 1, 2},
		{"line-42", "internal/foo/bar.go", 42, "internal/foo/bar.go", 42, 1, 42, 2},
		{"line-large", "big.go", 999999, "big.go", 999999, 1, 999999, 2},
		// AC 03-02: Line <= 0 synthesizes the 1,1,1,1 fallback region. Line == 0 and
		// Line < 0 are DISTINCT rows so a future <=→< off-by-one regression is caught.
		{"line-zero", "internal/foo/bar.go", 0, "internal/foo/bar.go", 1, 1, 1, 1},
		{"line-negative-one", "internal/foo/bar.go", -1, "internal/foo/bar.go", 1, 1, 1, 1},
		{"line-negative-large", "internal/foo/bar.go", -999, "internal/foo/bar.go", 1, 1, 1, 1},
		// AC 03-01 Edge Case 3: empty File passes through unmodified (no defaulting).
		{"empty-file", "", 5, "", 5, 1, 5, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loc := sarifLocation(reconcile.JSONFinding{File: tc.file, Line: tc.line})
			assert.Equal(t, tc.wantURI, loc.PhysicalLocation.ArtifactLocation.URI)
			r := loc.PhysicalLocation.Region
			assert.Equal(t, tc.wantStart, r.StartLine)
			assert.Equal(t, tc.wantStartCol, r.StartColumn)
			assert.Equal(t, tc.wantEnd, r.EndLine)
			assert.Equal(t, tc.wantEndCol, r.EndColumn)
			// Error Scenario 2 (03-01): region.startLine is never <= 0 for any input.
			assert.Greater(t, r.StartLine, 0)
			assert.Greater(t, r.EndLine, 0)
		})
	}
}

// --- Final Phase 4.1: Schema Conformance Validation ---

// TestSarif_SchemaConformance validates renderSarif output against the canonical
// SARIF 2.1.0 JSON Schema (SchemaStore's sarif-2.1.0-rtm.5.json, the variant
// GitHub Code Scanning validates against). This is a stricter, structural check
// than the field-by-field golden/unit tests: a missing required property, wrong
// enum value, or mis-shaped nested object fails here even if it passed the golden.
// Test-only — google/jsonschema-go is already a go.mod dependency (used by
// internal/mcp); no production dependency is added.
func TestSarif_SchemaConformance(t *testing.T) {
	schemaBytes, err := os.ReadFile(filepath.Join("testdata", "sarif-schema-2.1.0.json"))
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(schemaBytes, &schema))
	// The schema's $refs are internal (#/definitions/...), so Resolve needs only a
	// BaseURI and no external Loader.
	resolved, err := schema.Resolve(&jsonschema.ResolveOptions{
		BaseURI: "https://json.schemastore.org/sarif-2.1.0-rtm.5.json",
	})
	require.NoError(t, err)

	cases := map[string][]reconcile.JSONFinding{
		"sample":      sample(),
		"sampleSarif": sampleSarif(), // includes a file-level (Line<=0) finding
		"empty":       {},
		"nil":         nil,
		"file-level":  {{Severity: "MEDIUM", File: "x.go", Line: 0, Problem: "p", Category: "c"}},
		"empty-cat":   {{Severity: "LOW", File: "y.go", Line: 3, Problem: "p", Category: ""}},
	}
	for name, findings := range cases {
		t.Run(name, func(t *testing.T) {
			out := renderSarifString(t, findings)
			// Validate expects a value shaped like the result of unmarshaling JSON
			// into any (map/slice/scalar), not raw bytes or a typed struct.
			var data any
			require.NoError(t, json.Unmarshal([]byte(out), &data))
			require.NoError(t, resolved.Validate(data),
				"renderSarif output must conform to the SARIF 2.1.0 schema")
		})
	}
}
