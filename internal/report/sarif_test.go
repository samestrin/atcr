// Package report SARIF-formatter tests. Covers format registration (AC 01-01),
// base document structure (AC 01-02), and rules-array/category linkage (AC 01-03).
// Severity mapping (AC 02-01) and line/file anchoring (AC 03-01/03-02) tests are
// added in Phase 2 below their respective helpers.
package report

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

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
