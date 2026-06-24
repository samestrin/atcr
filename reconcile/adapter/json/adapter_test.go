package json

import (
	"bytes"
	stdjson "encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/reconcile"
)

const schemaVersion = "reconcile-json/v1"

// fixedTime is a deterministic timestamp used to remove clock variance from
// encode assertions.
var fixedTime = time.Date(2026, 6, 23, 17, 39, 52, 0, time.UTC)

// sampleResult builds a fully-populated Result covering a finding with reviewers,
// confidence, disagreement, and a populated *Verification, plus a non-empty
// summary and one ambiguous cluster.
func sampleResult() reconcile.Result {
	return reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{
				Severity:     "high",
				File:         "main.go",
				Line:         42,
				Problem:      "nil deref",
				Fix:          "add nil check",
				Category:     "bug",
				EstMinutes:   15,
				Evidence:     "reviewer-a: stack trace",
				Reviewers:    []string{"reviewer-a", "reviewer-b"},
				Confidence:   "HIGH",
				Disagreement: "medium vs high",
				Verification: &reconcile.Verification{
					Verdict: reconcile.VerdictConfirmed,
					Skeptic: "skeptic-1",
					Notes:   "reproduced locally",
				},
			}},
		},
		Ambiguous: []reconcile.AmbiguousCluster{
			{
				ID:         "amb-1",
				File:       "main.go",
				Line:       50,
				Similarity: 0.55,
				Findings: []reconcile.Finding{
					{Severity: "low", File: "main.go", Line: 50, Problem: "maybe dup", Reviewer: "reviewer-a"},
				},
			},
		},
		Summary: reconcile.Summary{
			SourcesScanned: []string{"reviewer-a", "reviewer-b"},
			TotalFindings:  1,
		},
	}
}

func TestDecode_SingleSourceObject(t *testing.T) {
	in := []byte(`{"version":"reconcile-json/v1","source":"reviewer-a","findings":[{"severity":"high","file":"main.go","line":42,"problem":"nil deref","fix":"add nil check","category":"bug","est_minutes":15,"evidence":"...","reviewer":"reviewer-a"}]}`)

	got, err := Decode(in)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(sources) = %d, want 1", len(got))
	}
	if got[0].Name != "reviewer-a" {
		t.Errorf("Name = %q, want reviewer-a", got[0].Name)
	}
	if len(got[0].Findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1", len(got[0].Findings))
	}
	f := got[0].Findings[0]
	if f.Severity != "high" || f.File != "main.go" || f.Line != 42 ||
		f.Problem != "nil deref" || f.Fix != "add nil check" || f.Category != "bug" ||
		f.EstMinutes != 15 || f.Evidence != "..." || f.Reviewer != "reviewer-a" {
		t.Errorf("finding fields not mapped from wire tags: %+v", f)
	}
}

func TestDecode_ArrayOfSources(t *testing.T) {
	in := []byte(`[{"version":"reconcile-json/v1","source":"a","findings":[{"severity":"high","file":"a.go","line":1,"problem":"p","tool":"semgrep","rule_id":"G123"}]},{"version":"reconcile-json/v1","source":"b","findings":[{"severity":"low","file":"b.go","line":2,"problem":"q"}]}]`)

	got, err := Decode(in)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(sources) = %d, want 2", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "b" {
		t.Errorf("array order not preserved: [0]=%q [1]=%q, want a,b", got[0].Name, got[1].Name)
	}
	// Unknown producer fields (tool, rule_id) are ignored, not errors.
	if got[0].Findings[0].Severity != "high" {
		t.Errorf("unknown-field tolerance broke mapping: %+v", got[0].Findings[0])
	}
}

func TestDecode_EmptyFindingsNonNil(t *testing.T) {
	in := []byte(`{"version":"reconcile-json/v1","source":"empty","findings":[]}`)
	got, err := Decode(in)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(sources) = %d, want 1", len(got))
	}
	if got[0].Findings == nil {
		t.Errorf("Findings is nil, want non-nil empty slice")
	}
	if len(got[0].Findings) != 0 {
		t.Errorf("len(Findings) = %d, want 0", len(got[0].Findings))
	}
}

func TestDecode_RejectsMissingVersion(t *testing.T) {
	in := []byte(`{"source":"a","findings":[]}`)
	if _, err := Decode(in); err == nil {
		t.Fatalf("Decode accepted input with missing version; want error")
	}
}

func TestDecode_RejectsWrongSchema(t *testing.T) {
	in := []byte(`{"version":"atcr-findings/v1","source":"a","findings":[]}`)
	if _, err := Decode(in); err == nil {
		t.Fatalf("Decode accepted atcr-findings/v1 input; want error")
	}
}

func TestDecode_MalformedJSON(t *testing.T) {
	in := []byte(`{"version":"reconcile-json/v1",`)
	if _, err := Decode(in); err == nil {
		t.Fatalf("Decode accepted malformed JSON; want error")
	}
}

func TestEncode_VersionedEnvelope(t *testing.T) {
	out, err := Encode(sampleResult(), reconcile.Options{ReconciledAt: fixedTime})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	var env struct {
		Version      string               `json:"version"`
		ReconciledAt string               `json:"reconciled_at"`
		Findings     []stdjson.RawMessage `json:"findings"`
		Summary      stdjson.RawMessage   `json:"summary"`
		Ambiguous    []stdjson.RawMessage `json:"ambiguous"`
	}
	if err := stdjson.Unmarshal(out, &env); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if env.Version != schemaVersion {
		t.Errorf("version = %q, want %q", env.Version, schemaVersion)
	}
	if env.ReconciledAt != fixedTime.Format(time.RFC3339) {
		t.Errorf("reconciled_at = %q, want %q", env.ReconciledAt, fixedTime.Format(time.RFC3339))
	}
	if len(env.Findings) != 1 {
		t.Errorf("findings len = %d, want 1", len(env.Findings))
	}
	if len(env.Summary) == 0 {
		t.Errorf("summary missing from envelope")
	}
	if len(env.Ambiguous) != 1 {
		t.Errorf("ambiguous len = %d, want 1", len(env.Ambiguous))
	}
}

func TestEncode_ReconciledAtFallsBackToNow(t *testing.T) {
	out, err := Encode(sampleResult(), reconcile.Options{}) // zero ReconciledAt
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	var env struct {
		ReconciledAt string `json:"reconciled_at"`
	}
	if err := stdjson.Unmarshal(out, &env); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if _, perr := time.Parse(time.RFC3339, env.ReconciledAt); perr != nil {
		t.Errorf("reconciled_at %q is not RFC3339: %v", env.ReconciledAt, perr)
	}
}

func TestEncode_EmptyArraysPresent(t *testing.T) {
	out, err := Encode(reconcile.Result{}, reconcile.Options{ReconciledAt: fixedTime})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if !bytes.Contains(out, []byte(`"findings":[]`)) {
		t.Errorf("zero-value Result must encode findings:[]; got %s", out)
	}
	if !bytes.Contains(out, []byte(`"ambiguous":[]`)) {
		t.Errorf("zero-value Result must encode ambiguous:[]; got %s", out)
	}
}

func TestByteStability_IdenticalOutput(t *testing.T) {
	r := sampleResult()
	opts := reconcile.Options{ReconciledAt: fixedTime}
	a, err := Encode(r, opts)
	if err != nil {
		t.Fatalf("Encode #1 error: %v", err)
	}
	b, err := Encode(r, opts)
	if err != nil {
		t.Fatalf("Encode #2 error: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("encode not byte-stable:\n#1 %s\n#2 %s", a, b)
	}

	// Absent disagreement / verification produce no keys.
	bare := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{Severity: "low", File: "x.go", Line: 1, Problem: "p", Reviewers: []string{"r"}, Confidence: "LOW"}},
		},
	}
	out, err := Encode(bare, opts)
	if err != nil {
		t.Fatalf("Encode bare error: %v", err)
	}
	if bytes.Contains(out, []byte(`"disagreement"`)) {
		t.Errorf("disagreement key leaked into output; check Finding struct tag for omitempty: %s", out)
	}
	if bytes.Contains(out, []byte(`"verification"`)) {
		t.Errorf("verification key leaked into output; check Finding struct tag for omitempty: %s", out)
	}
}

func TestEncode_GoldenFixture(t *testing.T) {
	out, err := Encode(sampleResult(), reconcile.Options{ReconciledAt: fixedTime})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "encode_golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	want := bytes.TrimRight(golden, "\n")
	if !bytes.Equal(out, want) {
		t.Errorf("encode drifted from golden fixture:\n got: %s\nwant: %s", out, want)
	}
}

func TestNoPathValidationFieldsInOutput(t *testing.T) {
	out, err := Encode(sampleResult(), reconcile.Options{ReconciledAt: fixedTime})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	for _, name := range []string{"PathValid", "PathWarning", "PathSuggestion", "ClusterMerged"} {
		if bytes.Contains(out, []byte(name)) {
			t.Errorf("path-validation field leaked into external schema: %s", name)
		}
	}
	if strings.Contains(string(out), "atcr-findings/v1") {
		t.Errorf(`output must never carry "atcr-findings/v1": %s`, out)
	}
}
