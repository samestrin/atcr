package stream

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Tier 3: case-only correction (CaseCorrection) ---

// TestCaseCorrection_CaseTypo: a path differing only by case from a tracked file
// is reported as a mismatch and the correctly-cased path is suggested (AC3).
func TestCaseCorrection_CaseTypo(t *testing.T) {
	root := gitRepo(t, "internal/auth/parser.go")
	idx := BuildFileIndex(context.Background(), root)
	require.NotNil(t, idx)

	sug, mismatch := idx.CaseCorrection("internal/auth/Parser.go")
	assert.True(t, mismatch)
	assert.Equal(t, "internal/auth/parser.go", sug)
}

// TestCaseCorrection_ExactIsValid: a byte-exact citation is not a mismatch and
// yields no suggestion.
func TestCaseCorrection_ExactIsValid(t *testing.T) {
	root := gitRepo(t, "internal/auth/parser.go")
	idx := BuildFileIndex(context.Background(), root)
	require.NotNil(t, idx)

	sug, mismatch := idx.CaseCorrection("internal/auth/parser.go")
	assert.False(t, mismatch)
	assert.Empty(t, sug)
}

// TestCaseCorrection_NoFoldedMatch: a path with no case-folded tracked match is
// not a case mismatch (it is a genuine miss, handled by the missing tiers).
func TestCaseCorrection_NoFoldedMatch(t *testing.T) {
	root := gitRepo(t, "internal/auth/parser.go")
	idx := BuildFileIndex(context.Background(), root)
	require.NotNil(t, idx)

	sug, mismatch := idx.CaseCorrection("internal/auth/nope.go")
	assert.False(t, mismatch)
	assert.Empty(t, sug)
}

// TestCaseCorrection_AmbiguousNoSuggestion: when two tracked files differ only
// by case, a differently-cased citation is a mismatch but ambiguous — no
// suggestion.
func TestCaseCorrection_AmbiguousNoSuggestion(t *testing.T) {
	// Two files differing only by case cannot coexist on a case-insensitive
	// filesystem, so this scenario is built from a synthetic path set rather
	// than a real repo.
	idx := indexFromPaths([]string{"internal/auth/parser.go", "internal/auth/Parser.go"})
	require.NotNil(t, idx)

	sug, mismatch := idx.CaseCorrection("internal/auth/PARSER.go")
	assert.True(t, mismatch)
	assert.Empty(t, sug)
}

// --- Tier 1: exact basename elsewhere (MissingSuggestion) ---

// TestMissingSuggestion_Tier1WrongDir: the cited dir is wrong but the exact
// basename exists in exactly one other directory — suggest it, no threshold (AC2).
func TestMissingSuggestion_Tier1WrongDir(t *testing.T) {
	root := gitRepo(t, "pkg/auth/validator.go")
	idx := BuildFileIndex(context.Background(), root)
	require.NotNil(t, idx)

	assert.Equal(t, "pkg/auth/validator.go", idx.MissingSuggestion("internal/auth/validator.go"))
}

// TestMissingSuggestion_Tier1Ranked: the basename exists in two directories; the
// one sharing more path segments with the cited path wins.
func TestMissingSuggestion_Tier1Ranked(t *testing.T) {
	root := gitRepo(t, "internal/auth/handler.go", "web/ui/handler.go")
	idx := BuildFileIndex(context.Background(), root)
	require.NotNil(t, idx)

	assert.Equal(t, "internal/auth/handler.go", idx.MissingSuggestion("internal/auth/sub/handler.go"))
}

// TestMissingSuggestion_Tier1AmbiguousNoSuggestion: two equally-good basename
// matches (no segment overlap to break the tie) yield no suggestion.
func TestMissingSuggestion_Tier1AmbiguousNoSuggestion(t *testing.T) {
	root := gitRepo(t, "alpha/handler.go", "beta/handler.go")
	idx := BuildFileIndex(context.Background(), root)
	require.NotNil(t, idx)

	assert.Empty(t, idx.MissingSuggestion("gamma/handler.go"))
}

// --- Tier 2: basename typo in an existing directory (MissingSuggestion) ---

// TestMissingSuggestion_Tier2Typo: the directory exists, the basename does not,
// and the closest real file in that directory clears the threshold (AC4).
func TestMissingSuggestion_Tier2Typo(t *testing.T) {
	root := gitRepo(t, "internal/auth/validate.go")
	idx := BuildFileIndex(context.Background(), root)
	require.NotNil(t, idx)

	assert.Equal(t, "internal/auth/validate.go", idx.MissingSuggestion("internal/auth/validator.go"))
}

// TestMissingSuggestion_Tier2BelowThreshold: a basename too dissimilar from any
// file in the existing directory yields no suggestion (AC4 below-threshold).
func TestMissingSuggestion_Tier2BelowThreshold(t *testing.T) {
	root := gitRepo(t, "internal/auth/validate.go")
	idx := BuildFileIndex(context.Background(), root)
	require.NotNil(t, idx)

	assert.Empty(t, idx.MissingSuggestion("internal/auth/xyz.go"))
}

// TestMissingSuggestion_Tier2ExtensionGuard: a same-stem file with a different
// extension is not suggested (Tier 2 requires matching extension).
func TestMissingSuggestion_Tier2ExtensionGuard(t *testing.T) {
	root := gitRepo(t, "internal/auth/validate.md")
	idx := BuildFileIndex(context.Background(), root)
	require.NotNil(t, idx)

	assert.Empty(t, idx.MissingSuggestion("internal/auth/validator.go"))
}

// TestMissingSuggestion_Tier2RejectsPluralDerivation: a missing path whose stem
// is a strict prefix/suffix derivation of a real sibling (pluralization,
// agent-noun) must NOT be suggested — these are commonly distinct coexisting
// files and score higher than a real typo. Independent-review MEDIUM.
func TestMissingSuggestion_Tier2RejectsPluralDerivation(t *testing.T) {
	cases := []struct{ tracked, cited string }{
		{"internal/store/user.go", "internal/store/users.go"},
		{"internal/http/handler.go", "internal/http/handlers.go"},
		{"internal/parse/parse.go", "internal/parse/parser.go"},
		{"internal/net/route.go", "internal/net/router.go"},
	}
	for _, c := range cases {
		idx := indexFromPaths([]string{c.tracked})
		assert.Emptyf(t, idx.MissingSuggestion(c.cited),
			"derivation %q should not be suggested toward %q", c.cited, c.tracked)
	}
}

// TestMissingSuggestion_Tier2KeepsTrueTypo: the canonical validator->validate
// typo survives the derivation guard (neither stem is a prefix of the other).
func TestMissingSuggestion_Tier2KeepsTrueTypo(t *testing.T) {
	idx := indexFromPaths([]string{"internal/auth/validate.go"})
	assert.Equal(t, "internal/auth/validate.go", idx.MissingSuggestion("internal/auth/validator.go"))
}

// TestMissingSuggestion_Tier1BeatsTier2: when the exact basename exists
// elsewhere, Tier 1 wins over any same-dir typo candidate.
func TestMissingSuggestion_Tier1BeatsTier2(t *testing.T) {
	root := gitRepo(t, "pkg/util/validator.go", "internal/auth/validate.go")
	idx := BuildFileIndex(context.Background(), root)
	require.NotNil(t, idx)

	assert.Equal(t, "pkg/util/validator.go", idx.MissingSuggestion("internal/auth/validator.go"))
}

// TestMissingSuggestion_NeverSuggestsSelf: a path tracked in git but absent on
// disk (so the missing tiers run) must not be suggested back to itself by Tier 2.
func TestMissingSuggestion_NeverSuggestsSelf(t *testing.T) {
	idx := indexFromPaths([]string{"internal/auth/validate.go"})
	require.NotNil(t, idx)

	assert.Empty(t, idx.MissingSuggestion("internal/auth/validate.go"))
}

// TestMissingSuggestion_NeverSuggestsTrackedSelfMulti: a tracked path that is
// absent on disk must not be "corrected" to a same-basename sibling when it is
// itself one of multiple Tier 1 candidates.
func TestMissingSuggestion_NeverSuggestsTrackedSelfMulti(t *testing.T) {
	idx := indexFromPaths([]string{"a/validate.go", "b/validate.go"})
	require.NotNil(t, idx)

	assert.Empty(t, idx.MissingSuggestion("a/validate.go"))
}

// TestMissingSuggestion_NoCandidates: an unknown basename in an unknown
// directory yields no suggestion.
func TestMissingSuggestion_NoCandidates(t *testing.T) {
	root := gitRepo(t, "internal/auth/validate.go")
	idx := BuildFileIndex(context.Background(), root)
	require.NotNil(t, idx)

	assert.Empty(t, idx.MissingSuggestion("totally/different/thing.go"))
}
