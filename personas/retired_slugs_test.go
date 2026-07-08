package personas

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bareRetiredRe matches the retired persona slugs sentinel/tracer as bare words.
// In the persona-identifier scan scope these are never legitimate English words
// (unlike "idiomatic"), so a bare match is a stale persona reference.
var bareRetiredRe = regexp.MustCompile(`\b(sentinel|tracer)\b`)

// idiomaticIdentRe matches "idiomatic" only in a PERSONA-IDENTIFIER context —
// a doc code span, a file reference, a fixture stem, or a quoted literal — never
// the ordinary adjective "idiomatic" that ingrid's own prompt legitimately uses.
var idiomaticIdentRe = regexp.MustCompile("`idiomatic`|idiomatic\\.(md|yaml)|idiomatic_fixture|\"idiomatic\"")

// retiredSlugScanFiles is the AC 05-03 verification scope for the persona-slug
// scan: the built-in templates, the registration source, the community index, and
// the persona docs + README. internal/personas/*_test.go is deliberately EXCLUDED
// — its only remaining retired-slug occurrences are AC 05-03 Edge-Case-2 arbitrary
// placeholders (list_test.go sort fixtures, the "performance/tracer" namespaced
// community fixture) and the intentional retiredRoleSlugs denylist, none of which
// are stale built-in-persona identifiers.
func retiredSlugScanFiles(t *testing.T) []string {
	t.Helper()
	mds, err := filepath.Glob("*.md")
	require.NoError(t, err)
	files := append([]string{}, mds...)
	files = append(files,
		"personas.go",
		filepath.Join("community", "index.json"),
		filepath.Join("..", "docs", "personas-authoring.md"),
		filepath.Join("..", "docs", "personas-install.md"),
		filepath.Join("..", "README.md"),
	)
	return files
}

// TestNoRetiredSlugs covers AC 05-03 Scenario 2: a scan of the persona-identifier
// scope finds zero references to the retired slugs sentinel/tracer/idiomatic as
// persona identifiers.
func TestNoRetiredSlugs(t *testing.T) {
	for _, f := range retiredSlugScanFiles(t) {
		data, err := os.ReadFile(f)
		require.NoErrorf(t, err, "read %s", f)
		text := string(data)
		if m := bareRetiredRe.FindString(text); m != "" {
			assert.Failf(t, "retired slug found", "%s still references retired persona slug %q", f, m)
		}
		if m := idiomaticIdentRe.FindString(text); m != "" {
			assert.Failf(t, "retired slug found", "%s references retired persona identifier %q", f, m)
		}
	}

	// Fixture filenames must carry no retired stem.
	patches, err := filepath.Glob(filepath.Join("testdata", "*.patch"))
	require.NoError(t, err)
	for _, p := range patches {
		base := filepath.Base(p)
		assert.NotRegexpf(t, `^(sentinel|tracer|idiomatic)_`, base, "fixture %s carries a retired slug stem", p)
	}
}

// TestRetiredSlugs_NewResolveOldFail covers AC 05-03 Scenario 1 / Error 1: the new
// slugs resolve (template + fixture) and the old slugs no longer resolve (not
// aliased).
func TestRetiredSlugs_NewResolveOldFail(t *testing.T) {
	for _, n := range []string{"sasha", "penny", "ingrid"} {
		_, err := Get(n)
		require.NoErrorf(t, err, "new slug %q must resolve", n)
		_, err = Fixture(n)
		require.NoErrorf(t, err, "new slug %q must have a fixture", n)
	}
	for _, n := range []string{"sentinel", "tracer", "idiomatic"} {
		_, err := Get(n)
		require.Errorf(t, err, "retired slug %q must not resolve (no alias)", n)
	}
}
