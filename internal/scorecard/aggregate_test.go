package scorecard

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runIDAt builds a run_id whose timestamp prefix is t (UTC RFC3339), matching the
// emitter's `reconciled_at + "-" + base` format, so --since filtering can be
// exercised deterministically against a fixed "now".
func runIDAt(t time.Time, base string) string {
	return t.UTC().Format(time.RFC3339) + "-" + base
}

func reviewer(runID, name, model string, raised, corroborated int, cost float64, latency int64) Record {
	return Record{
		SchemaVersion:        SchemaVersion,
		RecordType:           RecordTypeReviewer,
		RunID:                runID,
		Reviewer:             name,
		Model:                model,
		Role:                 "reviewer",
		FindingsRaised:       raised,
		FindingsCorroborated: corroborated,
		FindingsSolo:         raised - corroborated,
		CorroborationRate:    ratio(corroborated, raised),
		CostUSD:              cost,
		LatencyMS:            latency,
	}
}

func TestParseSince(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"7d", 7 * 24 * time.Hour, false},
		{"2w", 14 * 24 * time.Hour, false},
		{"3m", 90 * 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"abc", 0, true},
		{"0d", 0, true},
		{"-1d", 0, true},
		{"", 0, true},
		{"5y", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSince(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAggregate_RankedTable(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	records := []Record{
		reviewer(runIDAt(now, "r1"), "low", "m", 10, 2, 0.01, 1000),  // rate 0.20
		reviewer(runIDAt(now, "r2"), "high", "m", 10, 8, 0.02, 2000), // rate 0.80
		reviewer(runIDAt(now, "r3"), "mid", "m", 10, 5, 0.03, 1500),  // rate 0.50
	}
	rows := Aggregate(records)
	require.Len(t, rows, 3)
	assert.Equal(t, "high", rows[0].Reviewer, "highest corroboration rate ranked first")
	assert.Equal(t, "mid", rows[1].Reviewer)
	assert.Equal(t, "low", rows[2].Reviewer)
}

func TestAggregate_GroupsByReviewerModel(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	records := []Record{
		reviewer(runIDAt(now, "a"), "bruce", "claude-sonnet-4-6", 10, 6, 0.05, 1000),
		reviewer(runIDAt(now, "b"), "bruce", "claude-sonnet-4-6", 5, 4, 0.03, 2000),
		reviewer(runIDAt(now, "c"), "bruce", "gpt-4o", 8, 2, 0.04, 1500),
	}
	rows := Aggregate(records)
	require.Len(t, rows, 2, "same reviewer, different model → distinct rows")
	var sonnet *LeaderboardRow
	for i := range rows {
		if rows[i].Model == "claude-sonnet-4-6" {
			sonnet = &rows[i]
		}
	}
	require.NotNil(t, sonnet)
	assert.Equal(t, 2, sonnet.Runs)
	assert.Equal(t, 15, sonnet.FindingsRaised)
	assert.Equal(t, 10, sonnet.FindingsCorroborated)
	assert.InDelta(t, 10.0/15.0, sonnet.CorroborationRate, 1e-9)
}

// TestAggregate_RankStableOnEqualRate locks the adversarial-review fix: two
// groups whose corroboration rate is equal-by-value but summed from different run
// counts must tie-break deterministically by reviewer name, not by float jitter.
func TestAggregate_RankStableOnEqualRate(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	records := []Record{
		reviewer(runIDAt(now, "z"), "zara", "m", 2, 1, 0, 0),  // 1/2 = 0.5
		reviewer(runIDAt(now, "a1"), "alan", "m", 2, 1, 0, 0), // summed → 2/4 = 0.5
		reviewer(runIDAt(now, "a2"), "alan", "m", 2, 1, 0, 0),
	}
	rows := Aggregate(records)
	require.Len(t, rows, 2)
	assert.Equal(t, "alan", rows[0].Reviewer, "equal rate → tie-break by reviewer name asc")
	assert.Equal(t, "zara", rows[1].Reviewer)
}

func TestApplyFilters_ParsesOffsetTimestamp(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	// A run_id whose timestamp carries a numeric offset (not 'Z') must still be
	// time-filtered, not silently dropped.
	runID := "2026-06-15T11:00:00+00:00-offset"
	got, err := ApplyFilters([]Record{reviewer(runID, "a", "m", 1, 1, 0, 0)}, FilterOpts{Since: "7d"}, now)
	require.NoError(t, err)
	require.Len(t, got, 1, "offset-form RFC3339 timestamps are parsed, not dropped")
}

func TestAggregate_CostPerCorroborated_ZeroCorroborated(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	rows := Aggregate([]Record{reviewer(runIDAt(now, "x"), "eve", "haiku", 5, 0, 0.02, 1000)})
	require.Len(t, rows, 1)
	assert.False(t, rows[0].HasCostPerCorroborated, "zero corroborated → cost/corr is undefined (renders as dash)")
}

func TestAggregate_ExcludesAggregateRecords(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	agg := Record{SchemaVersion: SchemaVersion, RecordType: RecordTypeAggregate, RunID: runIDAt(now, "x"), FindingsRaised: 99}
	records := []Record{
		reviewer(runIDAt(now, "x"), "bruce", "m", 10, 6, 0.05, 1000),
		agg,
	}
	rows := Aggregate(records)
	require.Len(t, rows, 1, "aggregate record excluded from the leaderboard")
	assert.Equal(t, "bruce", rows[0].Reviewer)
}

func TestAggregate_EmptyStore(t *testing.T) {
	assert.Empty(t, Aggregate(nil))
}

func TestApplyFilters_SinceDays(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	records := []Record{
		reviewer(runIDAt(now.AddDate(0, 0, -3), "recent"), "a", "m", 1, 1, 0, 0),
		reviewer(runIDAt(now.AddDate(0, 0, -10), "old"), "b", "m", 1, 1, 0, 0),
	}
	got, err := ApplyFilters(records, FilterOpts{Since: "7d"}, now)
	require.NoError(t, err)
	require.Len(t, got, 1, "only records within 7 days")
	assert.Equal(t, "a", got[0].Reviewer)
}

func TestApplyFilters_SinceWeeks(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	records := []Record{
		reviewer(runIDAt(now.AddDate(0, 0, -10), "in"), "a", "m", 1, 1, 0, 0),
		reviewer(runIDAt(now.AddDate(0, 0, -20), "out"), "b", "m", 1, 1, 0, 0),
	}
	got, err := ApplyFilters(records, FilterOpts{Since: "2w"}, now)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "a", got[0].Reviewer)
}

func TestApplyFilters_SinceSpansMonthBoundary(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	records := []Record{
		reviewer(runIDAt(now.AddDate(0, 0, -2), "jul"), "a", "m", 1, 1, 0, 0),
		reviewer(runIDAt(now.AddDate(0, 0, -20), "jun"), "b", "m", 1, 1, 0, 0),
	}
	got, err := ApplyFilters(records, FilterOpts{Since: "30d"}, now)
	require.NoError(t, err)
	require.Len(t, got, 2, "30d window spans the June/July boundary")
}

func TestApplyFilters_SinceBoundaryInclusive(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	exactly7d := runIDAt(now.AddDate(0, 0, -7), "edge")
	got, err := ApplyFilters([]Record{reviewer(exactly7d, "a", "m", 1, 1, 0, 0)}, FilterOpts{Since: "7d"}, now)
	require.NoError(t, err)
	require.Len(t, got, 1, "a record exactly at the cutoff is included (>= cutoff)")
}

func TestApplyFilters_Model(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	records := []Record{
		reviewer(runIDAt(now, "a"), "bruce", "claude-sonnet-4-6", 1, 1, 0, 0),
		reviewer(runIDAt(now, "b"), "diana", "gpt-4o", 1, 1, 0, 0),
	}
	got, err := ApplyFilters(records, FilterOpts{Model: "claude-sonnet-4-6"}, now)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "bruce", got[0].Reviewer)
}

func TestApplyFilters_Persona(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	records := []Record{
		reviewer(runIDAt(now, "a"), "bruce", "m", 1, 1, 0, 0),
		reviewer(runIDAt(now, "b"), "diana", "m", 1, 1, 0, 0),
	}
	got, err := ApplyFilters(records, FilterOpts{Persona: "bruce"}, now)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "bruce", got[0].Reviewer)
}

func TestApplyFilters_Composed(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	records := []Record{
		reviewer(runIDAt(now.AddDate(0, 0, -1), "A"), "bruce", "claude-sonnet-4-6", 1, 1, 0, 0),  // match
		reviewer(runIDAt(now.AddDate(0, 0, -1), "B"), "bruce", "gpt-4o", 1, 1, 0, 0),             // wrong model
		reviewer(runIDAt(now.AddDate(0, 0, -1), "C"), "diana", "claude-sonnet-4-6", 1, 1, 0, 0),  // wrong persona
		reviewer(runIDAt(now.AddDate(0, 0, -20), "D"), "bruce", "claude-sonnet-4-6", 1, 1, 0, 0), // too old
	}
	got, err := ApplyFilters(records, FilterOpts{Since: "7d", Model: "claude-sonnet-4-6", Persona: "bruce"}, now)
	require.NoError(t, err)
	require.Len(t, got, 1, "AND semantics across since + model + persona")
}

func TestApplyFilters_NoMatch(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	records := []Record{reviewer(runIDAt(now, "a"), "bruce", "gpt-4o", 1, 1, 0, 0)}
	got, err := ApplyFilters(records, FilterOpts{Model: "nonexistent"}, now)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestApplyFilters_InvalidSince(t *testing.T) {
	_, err := ApplyFilters(nil, FilterOpts{Since: "abc"}, time.Now())
	require.Error(t, err, "an invalid --since value surfaces as an error")
}
