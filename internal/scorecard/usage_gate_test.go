package scorecard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// usageGateRecords returns two runs of one (persona, model): a measured run and a
// diff-cache replay. The pool summary records the model on every completed slot,
// usage or not, so the replay lands in the same row with no tokens, $0 and the
// replay's near-zero wall clock.
func usageGateRecords() []Record {
	measured := Record{
		SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer, RunID: "2026-09-01T00:00:00Z-a",
		Reviewer: "bruce", Model: "m1", Role: "reviewer",
		FindingsRaised: 4, FindingsCorroborated: 2, CorroborationRate: 0.5,
		CostUSD: 0.10, TokensIn: 1000, TokensOut: 200, LatencyMS: 1000,
	}
	replay := measured
	replay.RunID = "2026-09-02T00:00:00Z-b"
	replay.CostUSD, replay.TokensIn, replay.TokensOut, replay.LatencyMS = 0, 0, 0, 6
	return []Record{measured, replay}
}

// TestAggregate_NoUsageRunsExcludedFromCostAndLatency pins that a run with no
// reported usage contributes neither cost nor latency: its $0 must not halve the
// cost per corroborated finding, and its replay wall clock must not pull the
// average latency down.
func TestAggregate_NoUsageRunsExcludedFromCostAndLatency(t *testing.T) {
	rows := Aggregate(usageGateRecords())
	require.Len(t, rows, 1)
	assert.Equal(t, 2, rows[0].Runs)
	assert.Equal(t, 4, rows[0].FindingsCorroborated, "corroboration still counts every run")
	require.True(t, rows[0].HasCostPerCorroborated)
	assert.InDelta(t, 0.05, rows[0].CostPerCorroborated, 1e-9)
	assert.Equal(t, int64(1000), rows[0].AvgLatencyMS)
}

// TestAnonymizeRecords_NoUsageRunsExcludedFromCostAndLatency pins the same rule
// on the public export row.
func TestAnonymizeRecords_NoUsageRunsExcludedFromCostAndLatency(t *testing.T) {
	var acc reviewerAcc
	for _, r := range usageGateRecords() {
		acc.add(r)
	}
	pr := acc.finalize()
	assert.Equal(t, 2, pr.Runs)
	require.NotNil(t, pr.CostPerCorroboratedFindingUSD)
	assert.InDelta(t, 0.05, *pr.CostPerCorroboratedFindingUSD, 1e-9)
	assert.Equal(t, int64(1000), pr.LatencyP50MS)
}

// TestAggregate_OnlyNoUsageRunsHaveNoCostPerCorroborated pins that a row with no
// measured run reports cost per corroborated finding as absent, not $0.
func TestAggregate_OnlyNoUsageRunsHaveNoCostPerCorroborated(t *testing.T) {
	rows := Aggregate(usageGateRecords()[1:])
	require.Len(t, rows, 1)
	assert.False(t, rows[0].HasCostPerCorroborated)
	assert.Equal(t, int64(0), rows[0].AvgLatencyMS)

	var acc reviewerAcc
	acc.add(usageGateRecords()[1])
	assert.Nil(t, acc.finalize().CostPerCorroboratedFindingUSD)
}
