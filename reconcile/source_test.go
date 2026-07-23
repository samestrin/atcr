package reconcile

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSource_JSONTagsUseSnakeCase(t *testing.T) {
	s := Source{Name: "claude", Findings: []Finding{{File: "a.go", Line: 1, Problem: "p"}}}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"name"`) {
		t.Errorf("expected snake_case 'name' key, got %s", got)
	}
	if !strings.Contains(got, `"findings"`) {
		t.Errorf("expected snake_case 'findings' key, got %s", got)
	}
}

// TestSource_RoundTrip verifies the full marshaled structure is valid by
// marshaling a Source, unmarshaling it, and comparing the result. This catches
// structural issues (missing fields, incorrect types) that key-name checks miss.
func TestSource_RoundTrip(t *testing.T) {
	original := Source{
		Name: "claude",
		Findings: []Finding{
			{File: "a.go", Line: 1, Problem: "p1"},
			{File: "b.go", Line: 2, Problem: "p2"},
		},
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Unmarshal
	var decoded Source
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Compare
	if decoded.Name != original.Name {
		t.Errorf("Name mismatch: got %q, want %q", decoded.Name, original.Name)
	}
	if len(decoded.Findings) != len(original.Findings) {
		t.Fatalf("Findings count mismatch: got %d, want %d", len(decoded.Findings), len(original.Findings))
	}
	for i, f := range decoded.Findings {
		of := original.Findings[i]
		if f.File != of.File || f.Line != of.Line || f.Problem != of.Problem {
			t.Errorf("Finding[%d] mismatch: got %+v, want %+v", i, f, of)
		}
	}
}
