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
	scored, err := ListTiersWithScores(projectDir, communityDir, scores)
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
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores)
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
	scored, err := ListWithScores(dir, scores)
	require.NoError(t, err)

	owasp := scoredByName(scored, "security/Owasp")
	require.NotNil(t, owasp)
	require.NotNil(t, owasp.Rate, "case-insensitive lookup must join")
	assert.InDelta(t, 0.6, *owasp.Rate, 1e-9)
}

func TestListWithScores_ZeroRateIsNotNa(t *testing.T) {
	scores := map[string]float64{"sasha": 0.0}
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores)
	require.NoError(t, err)
	sasha := scoredByName(scored, "sasha")
	require.NotNil(t, sasha)
	require.NotNil(t, sasha.Rate, "rate 0.0 is data, not n/a")
	assert.Equal(t, "0.0%", FormatRate(sasha.Rate))
}

func TestListWithScores_NaNRateTreatedAsNa(t *testing.T) {
	scores := map[string]float64{"sasha": math.NaN()}
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores)
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
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores)
	require.NoError(t, err)
	require.NotEmpty(t, scored)
	assert.Equal(t, "sasha", scored[0].Name, "highest numeric rate sorts first")
}
