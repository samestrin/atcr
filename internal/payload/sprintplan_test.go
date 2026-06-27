package payload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// ReadSprintPlan must distinguish "no plan" (empty path or missing file → "", nil,
// review proceeds diff-wide) from "unreadable" (a path that exists but cannot be
// read → error, caller warns) and "present" (valid file → its content).
func TestReadSprintPlan(t *testing.T) {
	// Empty path = flag unset = no plan. Silent, no error (AC2).
	if got, err := ReadSprintPlan(""); err != nil || got != "" {
		t.Fatalf("ReadSprintPlan(\"\") = (%q, %v), want (\"\", nil)", got, err)
	}

	// Missing file is ignored silently, defaulting to a diff-wide review (AC2).
	missing := filepath.Join(t.TempDir(), "does-not-exist.md")
	if got, err := ReadSprintPlan(missing); err != nil || got != "" {
		t.Fatalf("ReadSprintPlan(missing) = (%q, %v), want (\"\", nil)", got, err)
	}

	// A valid file returns its content verbatim.
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	const body = "# Sprint 1\n- Task A: refactor auth\n"
	if err := os.WriteFile(planPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadSprintPlan(planPath); err != nil || got != body {
		t.Fatalf("ReadSprintPlan(valid) = (%q, %v), want (%q, nil)", got, err, body)
	}

	// An unreadable path (a directory) returns an error so the caller can warn on
	// stderr without crashing the review (AC3).
	if _, err := ReadSprintPlan(dir); err == nil {
		t.Fatalf("ReadSprintPlan(directory) = nil error, want error")
	}
}

// ScopeConstraint formats the SCOPE CONSTRAINT block. Empty/whitespace-only
// content yields "" (no block, review proceeds unconstrained); valid content is
// wrapped with the constraint instructions; oversized content is capped at
// MaxSprintPlanBytes and reports truncated=true.
func TestScopeConstraint(t *testing.T) {
	// Empty and whitespace-only content produce no block.
	if block, trunc := ScopeConstraint(""); block != "" || trunc {
		t.Fatalf("ScopeConstraint(\"\") = (%q, %v), want (\"\", false)", block, trunc)
	}
	if block, trunc := ScopeConstraint("   \n\t  "); block != "" || trunc {
		t.Fatalf("ScopeConstraint(whitespace) = (%q, %v), want (\"\", false)", block, trunc)
	}

	// Valid content is wrapped: the block must name the constraint, embed the plan
	// content, and preserve the soft-critical escape hatch so a genuinely critical
	// out-of-scope issue is still reportable.
	const body = "## Active Tasks\n- Implement sprint-plan scoping\n"
	block, trunc := ScopeConstraint(body)
	if trunc {
		t.Fatalf("ScopeConstraint(small) truncated=true, want false")
	}
	if !strings.Contains(block, "SCOPE CONSTRAINT") {
		t.Fatalf("ScopeConstraint block missing header: %q", block)
	}
	if !strings.Contains(block, body) {
		t.Fatalf("ScopeConstraint block missing plan content: %q", block)
	}
	if !strings.Contains(strings.ToLower(block), "critical") {
		t.Fatalf("ScopeConstraint block missing critical-issue escape hatch: %q", block)
	}

	// Oversized content is capped at MaxSprintPlanBytes and flagged truncated. The
	// embedded plan content must not exceed the ceiling, so the block cannot inflate
	// every agent prompt past payload_byte_budget (AC6).
	huge := strings.Repeat("x", int(MaxSprintPlanBytes)+5000)
	capped, truncated := ScopeConstraint(huge)
	if !truncated {
		t.Fatalf("ScopeConstraint(oversized) truncated=false, want true")
	}
	// The plan content embedded between the markers must be capped to the ceiling,
	// so the block cannot inflate every agent prompt past payload_byte_budget (AC6).
	// Measure the embedded segment directly rather than counting a character that
	// also appears in the wrapper prose.
	if n := int64(len(embeddedPlan(t, capped))); n > MaxSprintPlanBytes {
		t.Fatalf("ScopeConstraint embedded %d plan bytes, want <= %d", n, MaxSprintPlanBytes)
	}
}

// embeddedPlan extracts the plan content a ScopeConstraint block wraps between
// its BEGIN/END markers, so a test can measure the embedded segment precisely
// without being fooled by characters in the wrapper prose.
func embeddedPlan(t *testing.T, block string) string {
	t.Helper()
	const begin = "----- BEGIN SPRINT PLAN -----\n"
	const end = "\n----- END SPRINT PLAN -----"
	i := strings.Index(block, begin)
	if i < 0 {
		t.Fatalf("block missing BEGIN marker: %q", block)
	}
	rest := block[i+len(begin):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("block missing END marker: %q", block)
	}
	return rest[:j]
}

// The byte cap must not split a multibyte UTF-8 rune: truncating mid-rune would
// emit invalid UTF-8 into every agent prompt.
func TestScopeConstraint_TruncatesOnRuneBoundary(t *testing.T) {
	// "世" is 3 bytes; fill just past the ceiling with multibyte runes.
	runes := strings.Repeat("世", int(MaxSprintPlanBytes)/3+100)
	block, truncated := ScopeConstraint(runes)
	if !truncated {
		t.Fatalf("expected truncation of oversized multibyte content")
	}
	if !utf8.ValidString(block) {
		t.Fatalf("ScopeConstraint produced invalid UTF-8 after truncation")
	}
}
