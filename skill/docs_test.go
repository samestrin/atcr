package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDocs_SkillUsageDocumentsDebtResolve asserts docs/skill-usage.md documents the
// public /atcr debt resolve route, the local .atcr/-scoped TD store, and the
// public/private debt disambiguation (Story 5, ACs 05-01/05-02/05-03). Like
// internal/scorecard/docs_test.go it is a doc-presence/content test: it verifies
// required facts are present as literal substrings, not prose quality. The store
// path, flag name, rotation shard, and cycle-stage names are checked against the
// landed behavior in cmd/atcr/debt_resolve.go, cmd/atcr/reconcile.go, and
// skill/debt-resolve/SKILL.md so the doc cannot silently drift from the code.
func TestDocs_SkillUsageDocumentsDebtResolve(t *testing.T) {
	root := repoRoot(t)
	docPath := filepath.Join(root, "docs", "skill-usage.md")
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("docs/skill-usage.md not found at %s: %v", docPath, err)
	}
	doc := string(raw)

	cases := []struct {
		ac     string
		name   string
		substr string
	}{
		// AC 05-01: /atcr debt resolve route documentation (purpose, invocation, behavior).
		{"05-01", "route section heading", "## Technical Debt Resolution"},
		{"05-01", "route invocation", "atcr debt resolve"},
		{"05-01", "resolution cycle behavior", "RED→GREEN→ADVERSARIAL→REFACTOR"},
		{"05-01", "empty-store behavior", "empty or absent"},
		// AC 05-02: local .atcr/-scoped TD store storage section.
		{"05-02", "store path", ".atcr/debt/"},
		{"05-02", "monthly rotation shard", "YYYY-MM.jsonl"},
		{"05-02", "opt-out flag", "--no-local-debt"},
		{"05-02", "population trigger", "atcr reconcile"},
		{"05-02", "cross-run dedup key", "FindingID"},
		// AC 05-03: public/local vs private disambiguation + cross-link.
		{"05-03", "private-scope contrast", ".planning/technical-debt/"},
		{"05-03", "cross-link to technical-debt.md", "(technical-debt.md)"},
	}
	for _, tc := range cases {
		if !strings.Contains(doc, tc.substr) {
			t.Errorf("AC %s (%s): docs/skill-usage.md missing required content %q",
				tc.ac, tc.name, tc.substr)
		}
	}
}

// repoRoot walks up from the current working directory until it finds the
// directory containing go.mod (the module root), so the doc-presence test is
// independent of where it runs.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found walking up from working directory")
		}
		dir = parent
	}
}
