package scorecard

import (
	"fmt"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/reconcile"
	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The ambiguous-charge era bump (Epic 35.16.11.3.1 T3). Charge rule B (T2)
// changed what FindingsRaised counts, so the records written under it are era 4
// and the era-3 records written before it stay era 3. See
// raisedDenominatorAmbiguousCharged and foldAmbiguousChargeEra.

// eraRec is one strict reviewer record under the given era, with ambiguous
// singletons charged on top of a base raised/corroborated split. For era 3 the
// ambiguous count is what the run's filtered solos WOULD have charged, but era
// 3 did not charge them, so they are left out of every count.
func eraRec(runID, name string, era, raised, corroborated, ambiguous int) Record {
	r := Record{
		SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer, Outcome: outcomeFindings,
		RunID: runID, Reviewer: name, Model: "m",
		ConsensusLevel:           reclib.ConsensusStrict,
		RaisedIncludesUnresolved: true,
		RaisedDenominator:        era,
		FindingsRaised:           raised,
		FindingsCorroborated:     corroborated,
	}
	if era == raisedDenominatorAmbiguousCharged {
		r.FindingsRaised += ambiguous
		r.FindingsAmbiguous = ambiguous
	}
	r.FindingsSolo = r.FindingsRaised - r.FindingsCorroborated
	r.CorroborationRate = ratio(r.FindingsCorroborated, r.FindingsRaised)
	return r
}

func appendEra(t *testing.T, dir string, n int, prefix, name string, era, raised, corroborated, ambiguous int) {
	t.Helper()
	for i := 0; i < n; i++ {
		require.NoError(t, Append(dir, eraRec(fmt.Sprintf("2026-08-%02dT00:00:00Z-%s-%02d", 1+i%28, prefix, i), name, era, raised, corroborated, ambiguous)))
	}
}

func TestRaisedDenominatorCurrent_IsTheAmbiguousChargedEra(t *testing.T) {
	assert.Equal(t, 4, raisedDenominatorAmbiguousCharged, "era 4 is the first era under charge rule B")
	assert.Equal(t, raisedDenominatorAmbiguousCharged, RaisedDenominatorCurrent,
		"rule B changed what FindingsRaised counts, so the current era must move with it")
	assert.Equal(t, 3, raisedDenominatorRoutedExShield, "era 3 keeps its number: its records are still in every store")
	assert.Less(t, RaisedDenominatorCurrent, RaisedDenominatorBenchmarkSuite,
		"production eras must stay below the benchmark-suite marker")
}

func TestEmitForReconcile_StampsTheAmbiguousChargedEra(t *testing.T) {
	recs := emitAmbiguousRun(t, t.TempDir(), reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Category: "correctness", Reviewers: []string{"sasha"}}},
		},
		Ambiguous: []reconcile.AmbiguousCluster{
			{Findings: []reconcile.Finding{
				{File: "b.go", Line: 9, Problem: "solo security nit", Category: "security", Reviewer: "sasha"},
			}},
		},
	})
	sasha := findReviewer(recs, "sasha")
	require.NotNil(t, sasha)
	assert.Equal(t, raisedDenominatorAmbiguousCharged, sasha.RaisedDenominator,
		"a record written under rule B must say so")
	assert.True(t, sasha.RaisedIncludesUnresolved, "the bool still says routed findings are in the denominator, which stays true")
}

func TestRaisedDenominatorOf_ReadsARuleBEraThreeRecordAsEraFour(t *testing.T) {
	// A binary built between T2 and T3 charged the stream but still stamped
	// era 3. findings_ambiguous did not exist before rule B, so a non-zero one
	// on an era-3 record can only come from such a build.
	mislabelled := eraRec("r", "sasha", raisedDenominatorRoutedExShield, 3, 1, 0)
	mislabelled.FindingsAmbiguous = 2
	assert.Equal(t, raisedDenominatorAmbiguousCharged, raisedDenominatorOf(mislabelled))

	honest := eraRec("r", "sasha", raisedDenominatorRoutedExShield, 3, 1, 0)
	assert.Equal(t, raisedDenominatorRoutedExShield, raisedDenominatorOf(honest),
		"an era-3 record that charged nothing means the same under both rules and stays era 3")
}

func TestFoldAmbiguousChargeEra_MapsEraFourOntoEraThreeBelowTheFloor(t *testing.T) {
	in := []Record{eraRec("r", "sasha", raisedDenominatorAmbiguousCharged, 4, 2, 3)}
	got := foldAmbiguousChargeEra(in)

	require.Len(t, got, 1)
	assert.Equal(t, raisedDenominatorRoutedExShield, got[0].RaisedDenominator)
	assert.Equal(t, 4, got[0].FindingsRaised, "the charged singletons come back out of the denominator")
	assert.Equal(t, 2, got[0].FindingsSolo, "and out of solo, where rule B put them")
	assert.Equal(t, 0, got[0].FindingsAmbiguous)
	assert.InDelta(t, 0.5, got[0].CorroborationRate, 1e-9)
	assert.Equal(t, 7, in[0].FindingsRaised, "the input slice is never mutated")
}

func TestFoldAmbiguousChargeEra_LeavesEraFourAloneAtTheFloor(t *testing.T) {
	var in []Record
	for i := 0; i < DefaultTrustMinRuns; i++ {
		in = append(in, eraRec(fmt.Sprintf("r%02d", i), "sasha", raisedDenominatorAmbiguousCharged, 4, 2, 3))
	}
	// A rule-B record a pre-T3 build stamped era 3 counts toward the floor and
	// is relabelled, so the prefer-newest pass groups it with era 4.
	mislabelled := eraRec("rx", "sasha", raisedDenominatorRoutedExShield, 4, 2, 0)
	mislabelled.FindingsRaised += 3
	mislabelled.FindingsAmbiguous = 3
	in = append(in, mislabelled)

	got := foldAmbiguousChargeEra(in)
	for _, r := range got {
		assert.Equal(t, raisedDenominatorAmbiguousCharged, r.RaisedDenominator)
		assert.Equal(t, 7, r.FindingsRaised, "at the floor the charge stands")
	}
}

func TestFoldAmbiguousChargeEra_CountsTheFloorPerReviewer(t *testing.T) {
	var in []Record
	for i := 0; i < DefaultTrustMinRuns; i++ {
		in = append(in, eraRec(fmt.Sprintf("r%02d", i), "Sasha", raisedDenominatorAmbiguousCharged, 1, 1, 1))
	}
	in = append(in, eraRec("rp", "penny", raisedDenominatorAmbiguousCharged, 1, 1, 1))
	got := foldAmbiguousChargeEra(in)

	assert.Equal(t, raisedDenominatorAmbiguousCharged, got[0].RaisedDenominator, "sasha has reached the floor")
	assert.Equal(t, raisedDenominatorRoutedExShield, got[len(got)-1].RaisedDenominator,
		"penny has one era-4 run; sasha's twenty must not switch her over")
}

func TestFoldAmbiguousChargeEra_LeavesOtherRecordsAlone(t *testing.T) {
	era3 := eraRec("a", "sasha", raisedDenominatorRoutedExShield, 3, 1, 0)
	era2 := eraRec("b", "sasha", raisedDenominatorAllRouted, 3, 1, 0)
	above := eraRec("c", "sasha", RaisedDenominatorCurrent+1, 3, 1, 0)
	above.FindingsAmbiguous = 2
	agg := Record{RecordType: RecordTypeAggregate, RaisedDenominator: raisedDenominatorAmbiguousCharged, FindingsRaised: 5, FindingsAmbiguous: 2}

	got := foldAmbiguousChargeEra([]Record{era3, era2, above, agg})
	assert.Equal(t, []Record{era3, era2, above, agg}, got)
}

func TestFoldAmbiguousChargeEra_NeverDrivesACountNegative(t *testing.T) {
	// A hand-edited store can claim more ambiguous charges than raises.
	r := eraRec("r", "sasha", raisedDenominatorAmbiguousCharged, 0, 0, 0)
	r.FindingsRaised = 1
	r.FindingsAmbiguous = 5
	got := foldAmbiguousChargeEra([]Record{r})
	assert.Equal(t, 0, got[0].FindingsRaised)
	assert.Equal(t, 0, got[0].FindingsSolo)
}

func TestMergeRoutedEras_DoesNotFoldARuleBEraThreeRecord(t *testing.T) {
	// The 3-to-2 equivalence is a proof about records that charged no
	// ambiguous singleton. One that did is era 4 in all but its stamp.
	r := eraRec("r", "sasha", raisedDenominatorRoutedExShield, 3, 1, 0)
	r.FindingsAmbiguous = 2
	got := mergeRoutedEras([]Record{r})
	assert.Equal(t, raisedDenominatorRoutedExShield, got[0].RaisedDenominator)
	assert.Equal(t, 3, got[0].FindingsRaised)
}

// TestTrustPriors_TheEraBumpBlacksOutNoLens is the T3 success criterion: a
// lens above DefaultTrustMinRuns on era-3 history keeps its window the day its
// first era-4 record lands, and its prior is read under ONE definition.
func TestTrustPriors_TheEraBumpBlacksOutNoLens(t *testing.T) {
	dir := t.TempDir()
	appendEra(t, dir, 30, "old", "sasha", raisedDenominatorRoutedExShield, 4, 2, 0)
	appendEra(t, dir, 1, "new", "sasha", raisedDenominatorAmbiguousCharged, 4, 2, 4)

	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.Contains(t, priors, "sasha",
		"one era-4 run must not drop thirty era-3 runs and push the lens under the floor")
	assert.InDelta(t, 0.5, priors["sasha"], 1e-9,
		"read under era 3 until era 4 has its own floor: 0.484 would be the blend")
}

func TestTrustPriors_SwitchesToEraFourAtTheFloor(t *testing.T) {
	dir := t.TempDir()
	appendEra(t, dir, 30, "old", "sasha", raisedDenominatorRoutedExShield, 4, 2, 0)
	appendEra(t, dir, DefaultTrustMinRuns, "new", "sasha", raisedDenominatorAmbiguousCharged, 4, 2, 4)

	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.Contains(t, priors, "sasha")
	assert.InDelta(t, 0.25, priors["sasha"], 1e-9,
		"with a full era-4 window the charge stands and era 3 is superseded: 2 of 8")
}

func TestExplainTrustPriors_CountsTheFoldedEraFourRecord(t *testing.T) {
	// The explain walk must run the same fold, or it reports the era-3 window
	// as dropped at the era boundary while TrustPriors keeps it.
	var recs []Record
	for i := 0; i < 30; i++ {
		recs = append(recs, eraRec(fmt.Sprintf("2026-08-01T00:00:00Z-old-%02d", i), "sasha", raisedDenominatorRoutedExShield, 4, 2, 0))
	}
	recs = append(recs, eraRec("2026-08-02T00:00:00Z-new", "sasha", raisedDenominatorAmbiguousCharged, 4, 2, 4))

	assert.Len(t, keptForTrust(recs), 31)
	details := detailsFromRecords(recs, 0)
	assert.Equal(t, 31, details["sasha"].Counted)
	assert.Equal(t, 124, details["sasha"].Raised, "the era-4 run is counted under era 3")
}

func TestExport_RendersBothEras(t *testing.T) {
	recs := []Record{
		// bruce has not run since the bump: his row is era 3.
		eraRec("2026-08-01T00:00:00Z-bruce", "bruce", raisedDenominatorRoutedExShield, 4, 2, 0),
		// greta has: her era-3 run is superseded and her row is era 4.
		eraRec("2026-08-01T00:00:00Z-greta-old", "greta", raisedDenominatorRoutedExShield, 2, 2, 0),
		eraRec("2026-08-02T00:00:00Z-greta-new", "greta", raisedDenominatorAmbiguousCharged, 2, 1, 2),
	}
	out, err := Export(recs, FilterOpts{}, time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	env := parseEnvelope(t, out)
	require.Len(t, env.Reviewers, 2)
	by := map[string]PublicRecord{}
	for _, r := range env.Reviewers {
		by[r.Persona] = r
	}
	assert.Equal(t, raisedDenominatorRoutedExShield, by["bruce"].RaisedDenominator)
	assert.InDelta(t, 0.5, by["bruce"].CorroborationRate, 1e-9)
	assert.Equal(t, raisedDenominatorAmbiguousCharged, by["greta"].RaisedDenominator)
	assert.Equal(t, 1, by["greta"].Runs)
	assert.InDelta(t, 0.25, by["greta"].CorroborationRate, 1e-9, "1 of 4: her two filtered solos are charged")
}

func TestAggregate_SumsFindingsAmbiguous(t *testing.T) {
	rows := Aggregate([]Record{
		eraRec("2026-08-01T00:00:00Z-a", "sasha", raisedDenominatorAmbiguousCharged, 2, 1, 2),
		eraRec("2026-08-02T00:00:00Z-b", "sasha", raisedDenominatorRoutedExShield, 2, 1, 0),
	})
	require.Len(t, rows, 1)
	assert.Equal(t, 2, rows[0].FindingsAmbiguous)
	assert.Equal(t, 6, rows[0].FindingsRaised)
}
