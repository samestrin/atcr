package scorecard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	builtins "github.com/samestrin/atcr/personas"
	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inRepoPersonas is the closed set personas/personas.go registers and panics at
// init unless the embedded .md files match exactly. RemitCategories is grounded
// in these files and ONLY these (sprint-plan.md C9, AC 03-03 Option A): the five
// registry-only lenses (vera, pace, brad, archer, ronin) have no in-repo
// definition to ground against, and resolving them at runtime would read
// _base.md's generic fallback text as if it were their declared focus.
//
// It is builtins.Names(), NOT a literal copy of it. A literal would let a tenth
// persona ship with no remit entry and every test in this file still green —
// which is the exact failure the coverage test below claims to prevent.
var inRepoPersonas = builtins.Names()

// registryOnlyLenses are on the live 13-lens roster but ship no personas/<n>.md.
// C11 records that they are unmapped BY DESIGN, so this pins the decision rather
// than leaving the gap to be "fixed" later by someone who did not read C10.
var registryOnlyLenses = []string{"vera", "pace", "brad", "archer", "ronin"}

// TestRemitCategories_EveryValueIsARealVocabularyMember is AC 03-03 Edge Case 1:
// the guard against a hand-typed near-miss spelling. Membership is checked
// against the REAL reclib.Categories() slice, never a copied literal.
func TestRemitCategories_EveryValueIsARealVocabularyMember(t *testing.T) {
	vocab := map[string]struct{}{}
	for _, c := range reclib.Categories() {
		vocab[c] = struct{}{}
	}
	require.NotEmpty(t, vocab, "reclib.Categories() must not be empty")

	for _, p := range inRepoPersonas {
		cats, ok := RemitCategories(p)
		require.True(t, ok, "in-repo persona %q must be mapped", p)
		require.NotEmpty(t, cats, "persona %q must map to at least one category", p)
		for _, c := range cats {
			_, member := vocab[c]
			assert.True(t, member, "persona %q maps to %q which is not a reclib.Categories() member", p, c)
		}
	}
}

// TestRemitCategories_AllNineInRepoPersonasAreMapped pins the table's coverage to
// the embedded persona set itself, so adding a tenth persona file fails here
// instead of silently shipping an unmapped lens.
func TestRemitCategories_AllNineInRepoPersonasAreMapped(t *testing.T) {
	for _, p := range inRepoPersonas {
		_, ok := RemitCategories(p)
		assert.True(t, ok, "persona %q must resolve", p)
	}
}

// TestRemitCategories_RegistryOnlyLensesAreUnmapped is C11: vera and its four
// siblings return (nil, false) by design under Option A, taking AC 03-04 Edge
// Case 2's unmapped path rather than a fabricated remit read out of _base.md.
func TestRemitCategories_RegistryOnlyLensesAreUnmapped(t *testing.T) {
	for _, p := range registryOnlyLenses {
		cats, ok := RemitCategories(p)
		assert.False(t, ok, "registry-only lens %q must be unmapped (C9/C11)", p)
		assert.Nil(t, cats, "unmapped lens %q must return a nil slice, never an empty non-nil one", p)
	}
}

// TestRemitCategories_UnmappedPersonaReturnsNilFalse is AC 03-03 Error Scenario
// 1. A non-nil empty slice is explicitly wrong: a caller must be able to tell
// "unmapped" from "mapped to zero categories".
func TestRemitCategories_UnmappedPersonaReturnsNilFalse(t *testing.T) {
	for _, name := range []string{"", "   ", "nosuchpersona", "bruce2", strings.Repeat("x", 4096), "\x00bruce"} {
		cats, ok := RemitCategories(name)
		assert.False(t, ok, "name %q must not resolve", name)
		assert.Nil(t, cats, "name %q must return nil, not an empty slice", name)
	}
}

// TestRemitCategories_LookupIsCaseInsensitive is AC 03-03 Edge Case 2, matching
// trust.go's strings.ToLower(row.Reviewer) key convention.
func TestRemitCategories_LookupIsCaseInsensitive(t *testing.T) {
	lower, ok := RemitCategories("dax")
	require.True(t, ok)
	for _, variant := range []string{"Dax", "DAX", "dAx", " dax ", "\tDax\n"} {
		got, gotOK := RemitCategories(variant)
		assert.True(t, gotOK, "variant %q must resolve", variant)
		assert.ElementsMatch(t, lower, got, "variant %q must resolve identically to the lowercase key", variant)
	}
}

// TestRemitCategories_SpecialistsAreNarrowerThanTheGeneralist is AC 03-03 Happy
// Path Scenario 2 with C11's substitution applied: vera is unmapped under Option
// A, so the specialists measured here are the in-repo dax, otto and sasha.
func TestRemitCategories_SpecialistsAreNarrowerThanTheGeneralist(t *testing.T) {
	bruce, ok := RemitCategories("bruce")
	require.True(t, ok)
	for _, specialist := range []string{"dax", "otto", "sasha"} {
		cats, sok := RemitCategories(specialist)
		require.True(t, sok, "specialist %q must be mapped", specialist)
		assert.Greater(t, len(bruce), len(cats),
			"generalist bruce must have a strictly wider remit than specialist %q", specialist)
	}
}

// TestRemitCategories_RequiredMembersPerACAreePresent pins the specific members
// AC 03-03's DoD names, plus bruce's sixth Focus item (C12) — `invariant`, the
// member most likely to be dropped by reading only the first five Focus lines.
func TestRemitCategories_RequiredMembersPerACAreePresent(t *testing.T) {
	cases := []struct {
		persona string
		want    []string
	}{
		{"dax", []string{"error-handling", "testing"}},
		{"bruce", []string{"correctness", "error-handling", "contract", "state", "resource-leak", "invariant"}},
		{"sasha", []string{"security", "secret"}},
		{"otto", []string{"naming", "style"}},
	}
	for _, tc := range cases {
		got, ok := RemitCategories(tc.persona)
		require.True(t, ok, "persona %q must resolve", tc.persona)
		for _, w := range tc.want {
			assert.Contains(t, got, w, "persona %q remit must contain %q", tc.persona, w)
		}
	}
}

// TestRemitCategories_ReturnedSliceIsNotAliased proves a caller cannot corrupt
// the table for every later lookup in the same process — the same defence
// reclib.Categories() documents for its own returned slice.
func TestRemitCategories_ReturnedSliceIsNotAliased(t *testing.T) {
	first, ok := RemitCategories("bruce")
	require.True(t, ok)
	require.NotEmpty(t, first)
	first[0] = "corrupted"

	second, ok := RemitCategories("bruce")
	require.True(t, ok)
	assert.NotContains(t, second, "corrupted", "RemitCategories must return a copy, not the backing array")
}

// TestRemitCategories_TableIsGroundedInTheEmbeddedPersonaFiles is C12's
// verifiability requirement made mechanical: every mapped persona must have a
// personas/<name>.md file on disk with a ## Focus section. It does not parse the
// prose into categories (that judgement is the code review's, per AC 03-03 Edge
// Case 3) — it pins that the grounding SOURCE exists for each entry, so an entry
// invented for a persona that ships no file fails here.
func TestRemitCategories_TableIsGroundedInTheEmbeddedPersonaFiles(t *testing.T) {
	for _, p := range inRepoPersonas {
		_, ok := RemitCategories(p)
		require.True(t, ok, "persona %q must be mapped", p)

		// repoRoot, not "../../": the package already provides it (docs_test.go)
		// precisely so a test does not encode its own directory depth and break
		// on a package move for a reason unrelated to what it asserts.
		body, err := os.ReadFile(filepath.Join(repoRoot(t), "personas", p+".md"))
		require.NoError(t, err, "persona %q must ship an in-repo definition to be grounded against", p)
		assert.Contains(t, string(body), "## Focus",
			"persona %q's file must carry the ## Focus list its remit is grounded in", p)
	}
}

// TestRemitCategories_GoldenTable pins every entry exactly.
//
// The membership and required-member tests above are both satisfiable by a table
// in which most values were swapped for arbitrary other vocabulary members — the
// first only checks that a value IS a member, the second covers four personas
// and a third of the values. This one makes any edit to the mapping a
// deliberate, reviewable diff instead of a silent one, which is the whole basis
// of the "grounded in its own ## Focus list" claim in remit.go.
func TestRemitCategories_GoldenTable(t *testing.T) {
	golden := map[string][]string{
		"bruce":  {"correctness", "logic", "error-handling", "contract", "state", "resource-leak", "invariant"},
		"dax":    {"testing", "error-handling", "invariant"},
		"greta":  {"correctness", "logic", "type", "state", "complexity", "performance", "invariant"},
		"ingrid": {"error-handling", "resource-leak", "type", "bloat", "concurrency", "race", "duplication", "style", "invariant"},
		"kai":    {"coupling", "dependency", "api-contract", "contract", "duplication", "extensibility", "invariant"},
		"mira":   {"error-handling", "resource-leak", "observability", "concurrency", "race", "configuration", "invariant"},
		"otto":   {"naming", "style", "maintainability", "complexity", "docs", "invariant"},
		"penny":  {"performance", "leak", "complexity", "resource-leak", "invariant"},
		"sasha":  {"security", "input-validation", "secret", "validation", "leak", "invariant"},
	}
	require.Len(t, golden, len(inRepoPersonas), "the golden table must cover every registered persona")
	for _, p := range inRepoPersonas {
		want, declared := golden[p]
		require.True(t, declared, "persona %q has no golden entry — add one when adding a persona", p)
		got, ok := RemitCategories(p)
		require.True(t, ok, "persona %q must be mapped", p)
		assert.ElementsMatch(t, want, got, "persona %q's remit changed", p)
	}
}

// TestPersonaRemit_EveryDiscriminatingCategoryHasALens is the guard against a
// nine-lens blackout.
//
// A vocabulary member in NO persona's remit is not a gap in coverage, it is a
// trap: a run whose union is exactly that member is non-empty, so it skips
// opportunitySetRuns' pass-through, matches nobody, and deletes every mapped
// lens's record for that run. `race` was such a member before this test existed,
// despite being named verbatim in two personas' Focus lists.
func TestPersonaRemit_EveryDiscriminatingCategoryHasALens(t *testing.T) {
	covered := map[string]bool{}
	for _, p := range inRepoPersonas {
		cats, ok := RemitCategories(p)
		require.True(t, ok)
		for _, c := range cats {
			covered[c] = true
		}
	}
	for _, c := range reclib.Categories() {
		if !discriminating(c) {
			continue // carries no topic; excluded from the union by design
		}
		assert.True(t, covered[c],
			"category %q is in no persona's remit, so a run raising only %q blacks out every mapped lens", c, c)
	}
}

// TestPersonaRemit_InvariantIsInEveryRemit pins the premise nonDiscriminating
// relies on: `invariant` is excluded from the opportunity union BECAUSE every
// persona's Focus item 6 instructs it, making it a shared filing convention
// rather than a distinguishing remit. If that ever stops being true the
// exclusion is over-applying and must be revisited here.
func TestPersonaRemit_InvariantIsInEveryRemit(t *testing.T) {
	for _, p := range inRepoPersonas {
		cats, ok := RemitCategories(p)
		require.True(t, ok)
		assert.Contains(t, cats, reclib.CategoryInvariant,
			"persona %q must carry invariant, or nonDiscriminating's rationale no longer holds", p)
	}
}

// TestNonDiscriminating_AreAllRealVocabularyMembers proves the exclusion set is
// load-bearing rather than decorative: if these were NOT members they would be
// dropped by the vocabulary gate at write time and never reach the union at all.
func TestNonDiscriminating_AreAllRealVocabularyMembers(t *testing.T) {
	for c := range nonDiscriminating {
		assert.True(t, inVocabulary(c),
			"%q must be a real vocabulary member, or excluding it here is dead code", c)
	}
}
