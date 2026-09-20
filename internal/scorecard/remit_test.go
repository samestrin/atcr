package scorecard

import (
	"os"
	"strings"
	"testing"

	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inRepoPersonas is the closed set personas/personas.go registers and panics at
// init unless the embedded .md files match exactly. RemitCategories is grounded
// in these nine files and ONLY these nine (sprint-plan.md C9, AC 03-03 Option
// A): the five registry-only lenses (vera, pace, brad, archer, ronin) have no
// in-repo definition to ground against, and resolving them at runtime would
// read _base.md's generic fallback text as if it were their declared focus.
var inRepoPersonas = []string{
	"bruce", "dax", "greta", "ingrid", "kai", "mira", "otto", "penny", "sasha",
}

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

		body, err := os.ReadFile("../../personas/" + p + ".md")
		require.NoError(t, err, "persona %q must ship an in-repo definition to be grounded against", p)
		assert.Contains(t, string(body), "## Focus",
			"persona %q's file must carry the ## Focus list its remit is grounded in", p)
	}
}
