package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/stream"
)

func f(sev string) stream.Finding {
	return stream.Finding{Severity: sev, File: "a.go", Line: 1, Problem: "p", Category: "c"}
}

func intp(n int) *int { return &n }

func sevs(fs []stream.Finding) []string {
	out := make([]string, len(fs))
	for i, x := range fs {
		out[i] = x.Severity
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestEnforceConstraints(t *testing.T) {
	tests := []struct {
		name        string
		in          []stream.Finding
		minSeverity string
		maxFindings *int
		want        []string // expected severities in order
	}{
		{
			name: "no constraints is a no-op preserving order",
			in:   []stream.Finding{f("LOW"), f("HIGH"), f("MEDIUM")},
			want: []string{"LOW", "HIGH", "MEDIUM"},
		},
		{
			name:        "min_severity drops findings below the floor",
			in:          []stream.Finding{f("LOW"), f("MEDIUM"), f("HIGH"), f("LOW")},
			minSeverity: "MEDIUM",
			want:        []string{"MEDIUM", "HIGH"},
		},
		{
			name:        "min_severity keeps CRITICAL and HIGH at a HIGH floor",
			in:          []stream.Finding{f("CRITICAL"), f("HIGH"), f("MEDIUM"), f("LOW")},
			minSeverity: "HIGH",
			want:        []string{"CRITICAL", "HIGH"},
		},
		{
			name:        "max_findings truncates keeping the most severe",
			in:          []stream.Finding{f("LOW"), f("LOW"), f("HIGH"), f("LOW"), f("CRITICAL")},
			maxFindings: intp(2),
			want:        []string{"CRITICAL", "HIGH"},
		},
		{
			name:        "max_findings no-op when under the cap (order preserved)",
			in:          []stream.Finding{f("LOW"), f("HIGH")},
			maxFindings: intp(5),
			want:        []string{"LOW", "HIGH"},
		},
		{
			name:        "min_severity then max_findings compose",
			in:          []stream.Finding{f("LOW"), f("MEDIUM"), f("HIGH"), f("CRITICAL"), f("MEDIUM")},
			minSeverity: "MEDIUM",
			maxFindings: intp(2),
			want:        []string{"CRITICAL", "HIGH"},
		},
		{
			name:        "min_severity accepts lower-case (defensive normalization)",
			in:          []stream.Finding{f("LOW"), f("HIGH")},
			minSeverity: "high",
			want:        []string{"HIGH"},
		},
		{
			name:        "min_severity unknown level is a no-op (fail-open with warning)",
			in:          []stream.Finding{f("LOW"), f("HIGH"), f("MEDIUM")},
			minSeverity: "BOGUS",
			want:        []string{"LOW", "HIGH", "MEDIUM"},
		},
		{
			name:        "finding severity with surrounding whitespace is not silently dropped",
			in:          []stream.Finding{f(" HIGH "), f("LOW")},
			minSeverity: "HIGH",
			want:        []string{" HIGH "},
		},
		{
			name: "empty input is a no-op",
			in:   nil,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, _ := enforceConstraints(tt.in, "bruce-backup", tt.minSeverity, tt.maxFindings)
			if !eq(sevs(got), tt.want) {
				t.Fatalf("enforceConstraints severities = %v, want %v", sevs(got), tt.want)
			}
		})
	}
}

// findingsFor must apply the per-agent constraints after stamping REVIEWER, so a
// capped agent's per-agent findings.txt and the merged stream both respect it.
func TestFindingsFor_AppliesConstraints(t *testing.T) {
	content := "HIGH|a.go:1|bug|fix|correctness|5|ev\n" +
		"LOW|a.go:2|nit|fix|style|5|ev\n" +
		"MEDIUM|a.go:3|gap|fix|correctness|5|ev\n"
	r := Result{Agent: "bruce-backup", Content: content, Status: StatusOK, MinSeverity: "MEDIUM", MaxFindings: intp(1)}
	fr := findingsFor(r)
	if len(fr.Findings) != 1 {
		t.Fatalf("got %d findings, want 1 (LOW dropped, capped to 1)", len(fr.Findings))
	}
	if fr.Findings[0].Severity != "HIGH" {
		t.Fatalf("survivor severity = %s, want HIGH (most severe kept)", fr.Findings[0].Severity)
	}
	if fr.Findings[0].Reviewer != "bruce-backup" {
		t.Fatalf("reviewer = %q, want bruce-backup (stamped before enforcement)", fr.Findings[0].Reviewer)
	}
}

// enforceConstraints must not mutate the caller's backing array. A future
// caller holding the pre-enforcement slice must not see its data silently
// clobbered by the in-place filter and sort.
func TestEnforceConstraints_DoesNotMutateInput(t *testing.T) {
	original := []stream.Finding{f("LOW"), f("HIGH"), f("MEDIUM")}
	snapshot := make([]stream.Finding, len(original))
	copy(snapshot, original)
	got, _, _ := enforceConstraints(original, "bruce-backup", "HIGH", intp(1))
	if !eq(sevs(original), sevs(snapshot)) {
		t.Fatalf("enforceConstraints mutated input: got %v, snapshot was %v", sevs(original), sevs(snapshot))
	}
	if len(got) != 1 || got[0].Severity != "HIGH" {
		t.Fatalf("enforceConstraints result = %v, want [HIGH]", sevs(got))
	}
}

// enforceConstraints treats *maxFindings <= 0 as "no cap" rather than a silent
// total drop. Without this self-defense, a direct caller bypassing registry
// validation could pass maxFindings=0 and lose every finding.
func TestEnforceConstraints_MaxFindingsZeroIsNoop(t *testing.T) {
	in := []stream.Finding{f("LOW"), f("HIGH"), f("MEDIUM")}
	got, dropped, truncated := enforceConstraints(in, "bruce-backup", "", intp(0))
	if len(got) != len(in) {
		t.Fatalf("max_findings=0 dropped findings: got %d, want %d", len(got), len(in))
	}
	if truncated != 0 {
		t.Fatalf("truncated = %d, want 0 (no cap applied)", truncated)
	}
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
}
