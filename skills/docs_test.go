package skills

import (
	"os"
	"os/exec"
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
		// AC13 words the threshold as "100 MB"; the constant is
		// DefaultAutoCompactMaxBytes = 100 << 20, so the doc says MiB and this
		// assertion follows the code rather than the rounded prose.
		{"AC13", "auto-compaction size threshold", "100 MiB"},
		{"AC13", "seam: excluded workflow field", "source_label"},
		// TD: `debt list --group` existed on main and was removed with the store
		// split, but the breaking-changes table recorded --group only "on add", so a
		// script running `atcr debt list --group U` hit an unknown-flag error the
		// documented retirement list did not cover.
		{"TD", "retired list --group filter", "`--group` on `list`"},
		// TD internal/payload/manifest.go:59: manifest.json durably records the
		// reviewer's ABSOLUTE repo root, which embeds a username on a developer
		// machine, and the review tree is the artifact people attach to PRs and bug
		// reports. Nothing renders the field, so the residual risk is disclosure by
		// sharing — which only a documented warning can address.
		{"TD", "manifest root disclosure", "absolute path of the repository it was reviewed in"},
	}
	for _, tc := range required {
		if !strings.Contains(doc, tc.substr) {
			t.Errorf("%s (%s): docs/technical-debt.md missing required content %q",
				tc.ac, tc.name, tc.substr)
		}
	}

	// The negative half, scoped to the LIVE documentation — everything above the
	// breaking-changes heading (TD skills/docs_test.go:104). Applied page-wide,
	// this gate and the migration table it sits beside were mutually
	// unsatisfiable: the table exists so an upgrader can find the flag their
	// script passes, and forbidding those literals everywhere forced it into
	// paraphrase ("the store-selection flags"), which nobody can grep for. What
	// AC13 protects — no live instruction pointing at a retired surface —
	// survives: a migration table is documentation OF a retirement, not an
	// instruction to use it.
	const migrationHeading = "## Breaking changes from the private-scope tooling"
	live, migration, found := strings.Cut(doc, migrationHeading)
	if !found {
		t.Fatalf("AC13: docs/technical-debt.md is missing the %q section, so the retired-surface assertions have no scope", migrationHeading)
	}

	// The private-scope store stays absent from the WHOLE page. Unlike a flag
	// name it is not something an upgrader greps for — the legacy file is left
	// untouched by design — and the bare ".planning/" prefix is the stricter
	// assertion that also keeps this file out of the AC11 grep gate, which scans
	// Go source for the full literal.
	if strings.Contains(doc, ".planning/") {
		t.Errorf("AC13 (private-scoped tree): docs/technical-debt.md still references retired surface %q", ".planning/")
	}

	retired := []struct {
		name   string
		substr string
	}{
		{"retired shard flag", "--items"},
		{"retired README flag", "--readme"},
		{"retired sync flag", "--sync"},
		{"retired migration command", "td-migrate"},
		{"retired dashboard default", "DASHBOARD.md"},
	}
	for _, tc := range retired {
		if strings.Contains(live, tc.substr) {
			t.Errorf("AC13 (%s): the live documentation still references retired surface %q",
				tc.name, tc.substr)
		}
		// The complementary half of the same rule: the migration table MUST name
		// it, because that table is the only place a reader lands after grepping
		// the page for what their script passes today.
		if !strings.Contains(migration, tc.substr) {
			t.Errorf("AC13 (%s): the breaking-changes table must name %q so an upgrader can grep for it",
				tc.name, tc.substr)
		}
	}
}

// TestAC11_PrivateStoreLiteralHasExactlyTwoReferences enforces AC11 — "no Go
// source references the private store" — which until now was attested by a
// one-off manual grep recorded in a DoD report (TD skills/docs_test.go:113).
// Nothing ran, so nothing stopped it regressing: the next file to reach for the
// private tree would have shipped with a green suite.
//
// The gate is an exact SET comparison, not a ceiling. An unexpected hit is a new
// reference to a store atcr no longer reads; a missing hit means an allowlisted
// exception went away and the allowlist should shrink with it, which is how an
// allowlist stops accumulating permanently-stale entries.
//
// Both survivors are references in PROSE — a comment pointing at where this
// repo's own tracked debt lives — not code that reads the store. That is the
// distinction AC11 draws, and it is why an allowlist exists at all.
func TestAC11_PrivateStoreLiteralHasExactlyTwoReferences(t *testing.T) {
	root := repoRoot(t)

	// Assembled at runtime from parts: spelled out, this file would match its own
	// gate and the test would report itself as the violation.
	literal := ".planning" + "/" + "technical-debt"

	cmd := exec.Command("git", "ls-files", "*.go")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v (AC11 is enforced against tracked files, so an untracked working tree cannot be checked)", err)
	}

	allowed := map[string]bool{
		// Prose reference: names this repo's own tracked-debt file in a comment
		// explaining a deliberately-still-open item.
		"internal/reconcile/adapter/adapter.go": false,
		// Prose reference: quotes the debt entry the test was written against.
		"internal/localdebt/lock_test.go": false,
	}

	for _, rel := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if rel == "" {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if readErr != nil {
			// A tracked-but-absent file is a staging artifact, not an AC11 breach.
			continue
		}
		if !strings.Contains(string(content), literal) {
			continue
		}
		if _, ok := allowed[rel]; !ok {
			t.Errorf("AC11: %s references the private store %q; no Go source may read or name it outside the allowlist in this test",
				rel, literal)
			continue
		}
		allowed[rel] = true
	}

	for rel, hit := range allowed {
		if !hit {
			t.Errorf("AC11: allowlisted file %s no longer references %q — remove it from the allowlist so the gate keeps tightening",
				rel, literal)
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
