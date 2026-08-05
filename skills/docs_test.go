package skills

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
// landed behavior in cli/debt_resolve.go, cli/reconcile.go, and
// skills/atcr/debt-resolve.md so the doc cannot silently drift from the code.
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
		// AC 05-03: one-store statement + cross-link. The former private-scope
		// contrast described the two-store split Plan 35.13 removed; asserting it
		// now would pin a doc to a store atcr no longer reads.
		{"05-03", "unified-store statement", "one store"},
		{"05-03", "cross-link to technical-debt.md", "(technical-debt.md)"},
	}
	for _, tc := range cases {
		if !strings.Contains(doc, tc.substr) {
			t.Errorf("AC %s (%s): docs/skill-usage.md missing required content %q",
				tc.ac, tc.name, tc.substr)
		}
	}
}

// TestDocs_TechnicalDebtDocumentsUnifiedStore asserts docs/technical-debt.md
// documents the store Plan 35.13 unified (AC13): all five subcommands over
// .atcr/debt/, the id-join contract, the v3 schema, the status lifetimes, the
// auto-compaction thresholds, and the atcr<->consumer seam. It is a
// doc-presence test in the same style as the skill-usage one above — the prior
// page went stale precisely because nothing asserted against it.
//
// The negative half matters as much as the positive half: a rewrite that leaves
// a retired flag or the old store path behind is the exact failure this test
// exists to catch.
func TestDocs_TechnicalDebtDocumentsUnifiedStore(t *testing.T) {
	root := repoRoot(t)
	docPath := filepath.Join(root, "docs", "technical-debt.md")
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("docs/technical-debt.md not found at %s: %v", docPath, err)
	}
	doc := string(raw)

	required := []struct {
		ac     string
		name   string
		substr string
	}{
		{"AC13", "store path", ".atcr/debt/"},
		{"AC13", "month shard", "YYYY-MM.jsonl"},
		{"AC13", "list subcommand", "atcr debt list"},
		{"AC13", "add subcommand", "atcr debt add"},
		{"AC13", "dashboard subcommand", "atcr debt dashboard"},
		{"AC13", "resolve subcommand", "atcr debt resolve"},
		{"AC13", "compact subcommand", "atcr debt compact"},
		{"AC13", "dashboard redirect flag", "--output"},
		{"AC13", "machine-readable flag", "--json"},
		{"AC13", "id contract", "SHA-256"},
		{"AC13", "schema v3 origin", "origin"},
		{"AC13", "schema v3 occurrences", "occurrences"},
		{"AC13", "schema v3 first_seen", "first_seen"},
		{"AC13", "terminal status", "wontfix"},
		{"AC13", "re-surfacing status", "deferred"},
		{"AC13", "auto-compaction record threshold", "100k"},
		{"AC13", "auto-compaction size threshold", "100 MB"},
		{"AC13", "seam: excluded workflow field", "source_label"},
	}
	for _, tc := range required {
		if !strings.Contains(doc, tc.substr) {
			t.Errorf("%s (%s): docs/technical-debt.md missing required content %q",
				tc.ac, tc.name, tc.substr)
		}
	}

	// The negative half. The private-scope store is asserted absent as the bare
	// ".planning/" prefix rather than the full former path: it is the stricter
	// assertion (no private-tree reference of ANY kind survives in this page), and
	// it keeps this file out of the AC11 grep gate, which scans Go source for the
	// full literal.
	retired := []struct {
		name   string
		substr string
	}{
		{"private-scoped tree", ".planning/"},
		{"retired shard flag", "--items"},
		{"retired README flag", "--readme"},
		{"retired sync flag", "--sync"},
		{"retired migration command", "td-migrate"},
		{"retired dashboard default", "DASHBOARD.md"},
	}
	for _, tc := range retired {
		if strings.Contains(doc, tc.substr) {
			t.Errorf("AC13 (%s): docs/technical-debt.md still references retired surface %q",
				tc.name, tc.substr)
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
