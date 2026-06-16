package scorecard

import "testing"

func TestIsRunID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid RFC3339-prefixed", "2026-06-14T10:00:00Z-abc123", true},
		{"T-bearing offset form", "2026-01-01T00:00:00-07:00", true},
		{"bare month", "2026-06", false},
		{"bad month too high", "2026-13-14T10:00:00Z", false},
		{"bad month zero", "2026-00-14T10:00:00Z", false},
		{"short", "2026-0", false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsRunID(tc.input)
			if got != tc.want {
				t.Errorf("IsRunID(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestIsRunIDTraversalSuffix confirms a traversal-suffixed run_id is accepted as
// a valid run_id shape but that monthFromRunID resolves only the month prefix,
// never the trailing traversal segment. The actual path-escape guard lives in
// monthFromRunID, not in IsRunID itself.
func TestIsRunIDTraversalSuffix(t *testing.T) {
	runID := "2026-06-14T10:00:00Z-../../../etc/passwd"
	if !IsRunID(runID) {
		t.Errorf("IsRunID(%q) = false, want true (traversal suffix does not invalidate run_id shape)", runID)
	}
	month, err := monthFromRunID(runID)
	if err != nil {
		t.Fatalf("monthFromRunID(%q) unexpected error: %v", runID, err)
	}
	const want = "2026-06"
	if month != want {
		t.Errorf("monthFromRunID(%q) = %q, want %q (traversal suffix must not bleed into month)", runID, month, want)
	}
}
