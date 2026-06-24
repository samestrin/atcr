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
