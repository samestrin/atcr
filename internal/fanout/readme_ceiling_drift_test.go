package fanout

// README drift guard for the baseline bin limit (Epic 35.16.13 T7). The README's
// "Baseline review" section tells operators how many bins one persona's plan may
// hold before the run is refused — since Epic 35.16.14 T5 the per-persona unit
// budget, registry.DefaultMaxUnits, which replaced the 64-bin ceiling there. This
// test reads the constant and README.md directly, following the
// cli/docs_audit_test.go precedent, so a change to it that forgets the docs fails
// CI.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
)

// readmeRepoRoot ascends from the package directory until it finds go.mod.
func readmeRepoRoot(t *testing.T) string {
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
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// baselineReviewSection returns the README's "## Baseline review" section, up to
// the next level-2 heading.
func baselineReviewSection(t *testing.T, readme string) string {
	t.Helper()
	const heading = "\n## Baseline review"
	start := strings.Index(readme, heading)
	if start < 0 {
		t.Fatalf("README.md has no %q section", strings.TrimSpace(heading))
	}
	section := readme[start+len(heading):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	return section
}

// ceilingPhrase matches every place the section states the limit as a number:
// "512 bins", "512-bin budget", "per 512 bins".
var ceilingPhrase = regexp.MustCompile(`\b(\d+)[- ]bins?\b`)

func TestREADMEBaselineUnitBudgetMatchesDefaultMaxUnits(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(readmeRepoRoot(t), "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	section := baselineReviewSection(t, string(b))
	matches := ceilingPhrase.FindAllStringSubmatch(section, -1)
	if len(matches) == 0 {
		t.Fatalf("README.md Baseline review section states no bin limit; it must document the unit budget (%d)", registry.DefaultMaxUnits)
	}
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("parse %q: %v", m[0], err)
		}
		if n != registry.DefaultMaxUnits {
			t.Errorf("README.md Baseline review section says %q, but registry.DefaultMaxUnits is %d", m[0], registry.DefaultMaxUnits)
		}
	}
}
