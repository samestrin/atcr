package personas

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ratePtr(f float64) *float64 { return &f }

// --- FormatRate -------------------------------------------------------------

func TestFormatRate(t *testing.T) {
	assert.Equal(t, "n/a", FormatRate(nil))
	assert.Equal(t, "0.0%", FormatRate(ratePtr(0.0)))
	assert.Equal(t, "50.0%", FormatRate(ratePtr(0.5)))
	assert.Equal(t, "72.5%", FormatRate(ratePtr(0.725)))
	assert.Equal(t, "100.0%", FormatRate(ratePtr(1.0)))
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
	scores := map[string]float64{"sentinel": 0.72} // tracer absent
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores)
	require.NoError(t, err)

	sentinel := scoredByName(scored, "sentinel")
	require.NotNil(t, sentinel)
	require.NotNil(t, sentinel.Rate)
	assert.InDelta(t, 0.72, *sentinel.Rate, 1e-9)

	tracer := scoredByName(scored, "tracer")
	require.NotNil(t, tracer)
	assert.Nil(t, tracer.Rate, "persona with no scorecard data has nil rate (n/a)")
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
	scores := map[string]float64{"sentinel": 0.0}
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores)
	require.NoError(t, err)
	sentinel := scoredByName(scored, "sentinel")
	require.NotNil(t, sentinel)
	require.NotNil(t, sentinel.Rate, "rate 0.0 is data, not n/a")
	assert.Equal(t, "0.0%", FormatRate(sentinel.Rate))
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
	scores := map[string]float64{"sentinel": 0.9, "idiomatic": 0.4}
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores)
	require.NoError(t, err)
	require.NotEmpty(t, scored)
	assert.Equal(t, "sentinel", scored[0].Name, "highest numeric rate sorts first")
}
