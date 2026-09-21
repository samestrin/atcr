package personas

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ratePtr(f float64) *float64 { return &f }

// --- AC 01-05: ListTiers three-tier source labeling -------------------------

// TestListTiers_ThreeSourcesInPrecedence covers AC 01-05 Scenario 2: `personas
// list` distinguishes project > community > built-in, with a project override
// shadowing the built-in of the same name and the community pin version shown.
func TestListTiers_ThreeSourcesInPrecedence(t *testing.T) {
	projectDir := t.TempDir()
	communityDir := t.TempDir()

	// A hand-authored project override for a built-in name.
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "bruce.md"), []byte("# project bruce\n"), 0o644))
	// A community persona (namespaced, disjoint from built-in names) with a pin.
	require.NoError(t, os.MkdirAll(filepath.Join(communityDir, "security"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(communityDir, "security", "owasp.yaml"), []byte(validPersonaYAML), 0o644))

	metas, err := ListTiers(projectDir, communityDir)
	require.NoError(t, err)

	byName := map[string]PersonaMeta{}
	for _, m := range metas {
		byName[m.Name] = m
	}
	require.Contains(t, byName, "bruce")
	assert.Equal(t, "project", byName["bruce"].Source, "project override shadows the built-in")
	require.Contains(t, byName, "security/owasp")
	assert.Equal(t, "community", byName["security/owasp"].Source)
	assert.Equal(t, "1.0.0", byName["security/owasp"].Version, "community pin version shown")
	require.Contains(t, byName, "greta")
	assert.Equal(t, "built-in", byName["greta"].Source, "un-overridden persona stays built-in")
}

func TestListTiers_CaseInsensitiveBuiltinDedup(t *testing.T) {
	projectDir := t.TempDir()
	communityDir := t.TempDir()

	// Namespaced community persona that does not collide.
	require.NoError(t, os.MkdirAll(filepath.Join(communityDir, "security"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(communityDir, "security", "Owasp.yaml"), []byte(validPersonaYAML), 0o644))
	// Mixed-case name that matches a built-in when compared case-insensitively.
	require.NoError(t, os.WriteFile(filepath.Join(communityDir, "Bruce.yaml"), []byte(validPersonaYAML), 0o644))

	metas, err := ListTiers(projectDir, communityDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collides with built-in")

	byName := map[string]PersonaMeta{}
	for _, m := range metas {
		byName[strings.ToLower(m.Name)] = m
	}
	require.Contains(t, byName, "security/owasp")
	require.Contains(t, byName, "bruce")
	assert.Equal(t, "built-in", byName["bruce"].Source, "mixed-case community file must be treated as built-in collision")
	assert.NotContains(t, byName, "Bruce", "Bruce must not appear separately from bruce")
}

func TestListTiersWithScores_IncludesProjectOverride(t *testing.T) {
	projectDir := t.TempDir()
	communityDir := t.TempDir()

	// Project override for a built-in name.
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "bruce.md"), []byte("# project bruce\n"), 0o644))
	// Community pin.
	require.NoError(t, os.MkdirAll(filepath.Join(communityDir, "security"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(communityDir, "security", "owasp.yaml"), []byte(validPersonaYAML), 0o644))

	scores := map[string]float64{"bruce": 0.9, "security/owasp": 0.6}
	scored, err := ListTiersWithScores(projectDir, communityDir, scores, nil)
	require.NoError(t, err)

	bruce := scoredByName(scored, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, "project", bruce.Source, "project override must appear in scored list")
	require.NotNil(t, bruce.Rate)
	assert.InDelta(t, 0.9, *bruce.Rate, 1e-9)

	owasp := scoredByName(scored, "security/owasp")
	require.NotNil(t, owasp)
	assert.Equal(t, "community", owasp.Source)
	require.NotNil(t, owasp.Rate)
	assert.InDelta(t, 0.6, *owasp.Rate, 1e-9)
}

// --- FormatRate -------------------------------------------------------------

func TestFormatRate(t *testing.T) {
	assert.Equal(t, "n/a", FormatRate(nil))
	assert.Equal(t, "0.0%", FormatRate(ratePtr(0.0)))
	assert.Equal(t, "50.0%", FormatRate(ratePtr(0.5)))
	assert.Equal(t, "72.5%", FormatRate(ratePtr(0.725)))
	assert.Equal(t, "100.0%", FormatRate(ratePtr(1.0)))
	// Out-of-range rates clamp to [0,100]% rather than render a nonsense value.
	assert.Equal(t, "0.0%", FormatRate(ratePtr(-0.5)))
	assert.Equal(t, "100.0%", FormatRate(ratePtr(1.5)))
}

// --- ListWithScores join ----------------------------------------------------

func scoredByName(scored []ScoredPersona, name string) *ScoredPersona {
	for i := range scored {
		if scored[i].Name == name {
			return &scored[i]
		}
	}
	return nil
}

func TestListWithScores_HasRateAndNa(t *testing.T) {
	scores := map[string]float64{"sasha": 0.72} // penny absent
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores, nil)
	require.NoError(t, err)

	sasha := scoredByName(scored, "sasha")
	require.NotNil(t, sasha)
	require.NotNil(t, sasha.Rate)
	assert.InDelta(t, 0.72, *sasha.Rate, 1e-9)

	penny := scoredByName(scored, "penny")
	require.NotNil(t, penny)
	assert.Nil(t, penny.Rate, "persona with no scorecard data has nil rate (n/a)")
}

func TestListWithScores_CaseInsensitiveJoin(t *testing.T) {
	// Scores map is keyed by lowercase reviewer name; a community persona whose
	// file name differs in case still joins.
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "security"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "security", "Owasp.yaml"), []byte(validPersonaYAML), 0o644))

	scores := map[string]float64{"security/owasp": 0.6} // lowercase key
	scored, err := ListWithScores(dir, scores, nil)
	require.NoError(t, err)

	owasp := scoredByName(scored, "security/Owasp")
	require.NotNil(t, owasp)
	require.NotNil(t, owasp.Rate, "case-insensitive lookup must join")
	assert.InDelta(t, 0.6, *owasp.Rate, 1e-9)
}

func TestListWithScores_ZeroRateIsNotNa(t *testing.T) {
	scores := map[string]float64{"sasha": 0.0}
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores, nil)
	require.NoError(t, err)
	sasha := scoredByName(scored, "sasha")
	require.NotNil(t, sasha)
	require.NotNil(t, sasha.Rate, "rate 0.0 is data, not n/a")
	assert.Equal(t, "0.0%", FormatRate(sasha.Rate))
}

func TestListWithScores_NaNRateTreatedAsNa(t *testing.T) {
	scores := map[string]float64{"sasha": math.NaN()}
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores, nil)
	require.NoError(t, err)
	sasha := scoredByName(scored, "sasha")
	require.NotNil(t, sasha)
	assert.Nil(t, sasha.Rate, "NaN rate must be treated as n/a")
	assert.Equal(t, "n/a", FormatRate(sasha.Rate))
}

// --- sortScoredPersonas -----------------------------------------------------

func names(ps []ScoredPersona) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

func TestSortScoredPersonas_NumericDescThenNaAlpha(t *testing.T) {
	ps := []ScoredPersona{
		{PersonaMeta: PersonaMeta{Name: "tracer"}, Rate: nil},
		{PersonaMeta: PersonaMeta{Name: "sentinel"}, Rate: ratePtr(0.72)},
		{PersonaMeta: PersonaMeta{Name: "guardian"}, Rate: nil},
		{PersonaMeta: PersonaMeta{Name: "idiomatic"}, Rate: ratePtr(0.50)},
	}
	sortScoredPersonas(ps)
	assert.Equal(t, []string{"sentinel", "idiomatic", "guardian", "tracer"}, names(ps))
}

func TestSortScoredPersonas_TieBreakAlphabetical(t *testing.T) {
	ps := []ScoredPersona{
		{PersonaMeta: PersonaMeta{Name: "sentinel"}, Rate: ratePtr(0.60)},
		{PersonaMeta: PersonaMeta{Name: "idiomatic"}, Rate: ratePtr(0.60)},
	}
	sortScoredPersonas(ps)
	assert.Equal(t, []string{"idiomatic", "sentinel"}, names(ps))
}

func TestSortScoredPersonas_AllNaAlphabetical(t *testing.T) {
	ps := []ScoredPersona{
		{PersonaMeta: PersonaMeta{Name: "tracer"}, Rate: nil},
		{PersonaMeta: PersonaMeta{Name: "guardian"}, Rate: nil},
		{PersonaMeta: PersonaMeta{Name: "sentinel"}, Rate: nil},
	}
	sortScoredPersonas(ps)
	assert.Equal(t, []string{"guardian", "sentinel", "tracer"}, names(ps))
}

func TestListWithScores_SortedOutput(t *testing.T) {
	// End-to-end: ListWithScores applies the sort so the first numeric row leads.
	scores := map[string]float64{"sasha": 0.9, "ingrid": 0.4}
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores, nil)
	require.NoError(t, err)
	require.NotEmpty(t, scored)
	assert.Equal(t, "sasha", scored[0].Name, "highest numeric rate sorts first")
}

// --- AC 06-05: the explainability detail joins additively -------------------

func TestJoinScores_AttachesDetailAlongsideTheExistingRate(t *testing.T) {
	// AC 06-05 Scenario 1. The detail map is a SECOND input (D4), keyed by the
	// same strings.ToLower(m.Name) the rate lookup has always used, and it never
	// replaces or perturbs Rate.
	metas := []PersonaMeta{{Name: "dax"}, {Name: "bruce"}}
	scores := map[string]float64{"dax": 0.8, "bruce": 0.4}
	details := map[string]ScoreDetail{
		"dax": {Counted: 20, Excluded: 5, Reasons: map[string]int{
			reasonOutcomeIneligibleForTest: 5,
		}},
		"bruce": {Counted: 40, Excluded: 0},
	}

	scored := joinScores(metas, scores, details)

	dax := scoredByName(scored, "dax")
	require.NotNil(t, dax)
	require.NotNil(t, dax.Rate)
	assert.InDelta(t, 0.8, *dax.Rate, 1e-9, "the rate path is untouched by the detail path")
	require.NotNil(t, dax.Detail)
	assert.Equal(t, 20, dax.Detail.Counted)
	assert.Equal(t, 5, dax.Detail.Excluded)
	assert.Equal(t, 5, dax.Detail.Reasons[reasonOutcomeIneligibleForTest])

	bruce := scoredByName(scored, "bruce")
	require.NotNil(t, bruce)
	require.NotNil(t, bruce.Detail)
	assert.Equal(t, 40, bruce.Detail.Counted)
}

func TestJoinScores_MixedCasePersonaFindsItsDetail(t *testing.T) {
	// The casing convention is shared by both lookups, so a persona cannot be
	// present in one map and missed in the other for a casing reason.
	metas := []PersonaMeta{{Name: "SASHA"}}
	scored := joinScores(metas,
		map[string]float64{"sasha": 0.5},
		map[string]ScoreDetail{"sasha": {Counted: 7}})

	require.Len(t, scored, 1)
	require.NotNil(t, scored[0].Detail)
	assert.Equal(t, 7, scored[0].Detail.Counted)
}

func TestJoinScores_AbsentFromDetailMapIsNilNotAFabricatedZero(t *testing.T) {
	// AC 06-05 Edge Cases 1 and 2. A persona with no history, and a below-floor
	// persona scorecard omitted, must both read as "no data" — never as an
	// evaluated-but-empty history, which would tell a maintainer the lens was
	// measured and found wanting when it was never measured at all.
	metas := []PersonaMeta{{Name: "ghost"}}
	scored := joinScores(metas, map[string]float64{}, map[string]ScoreDetail{})

	require.Len(t, scored, 1)
	assert.Nil(t, scored[0].Rate)
	assert.Nil(t, scored[0].Detail,
		"absent from the detail map must be nil Detail, never a zero-valued struct")
}

func TestJoinScores_NilDetailMapIsLegalAndYieldsNilDetail(t *testing.T) {
	// Every pre-Phase-5 caller passed no detail at all; a nil map must behave as
	// "no explainability available" rather than panicking.
	scored := joinScores([]PersonaMeta{{Name: "dax"}}, map[string]float64{"dax": 0.3}, nil)
	require.Len(t, scored, 1)
	require.NotNil(t, scored[0].Rate)
	assert.Nil(t, scored[0].Detail)
}

func TestSortScoredPersonas_NeverConsultsTheDetailFields(t *testing.T) {
	// AC 06-05 Scenario 2. Identical rates, wildly different detail — the
	// comparator must still fall through to the alphabetical tie-break, because
	// sortScoredPersonas' contract keys on Rate alone (D4/D6).
	metas := []PersonaMeta{{Name: "Zeta"}, {Name: "Alpha"}}
	scores := map[string]float64{"zeta": 0.5, "alpha": 0.5}
	details := map[string]ScoreDetail{
		"zeta":  {Counted: 999, Excluded: 0},
		"alpha": {Counted: 1, Excluded: 500},
	}

	scored := joinScores(metas, scores, details)
	require.Len(t, scored, 2)
	assert.Equal(t, "Alpha", scored[0].Name,
		"equal rates tie-break alphabetically; the new fields must not reorder them")
	assert.Equal(t, "Zeta", scored[1].Name)
}

// reasonOutcomeIneligibleForTest is scorecard.ReasonOutcomeIneligible's literal
// value. It is spelled out rather than imported because internal/personas must
// not import internal/scorecard (see ScoreDetail), and a test import would be
// just as much a boundary violation as a production one — internalImports in
// internal/boundaries_test.go reads every .go file in the package.
//
// The duplication is safe because this package never INTERPRETS the label: it
// carries Reasons through as opaque keys. The value is asserted against
// scorecard's own constant by cli/personas_test.go, which legally imports both.
const reasonOutcomeIneligibleForTest = "outcome-ineligible"
