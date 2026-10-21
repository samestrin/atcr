package scorecard

import (
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/reconcile"
	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The ambiguous-stream charge rule (Epic 35.16.11.3.1 T2, rule B, decided by
// Sam on the EST-2052 probe): a ONE-FINDING ambiguous cluster — a
// consensus-filtered solo or a DBSCAN-isolated noise finding, both absent from
// res.Findings — is charged to its reviewer as an uncorroborated raise. A
// gray-zone pair is not: its members also reach res.Findings and are charged
// there. See EmitInput.AmbiguousFindings.

// emitAmbiguousRun reconciles one run into store and returns its records.
func emitAmbiguousRun(t *testing.T, store string, res reconcile.Result) []Record {
	t.Helper()
	if res.Summary.ReconciledAt == "" {
		res.Summary.ReconciledAt = "2026-06-14T10:00:00Z"
	}
	EmitForReconcile(t.TempDir(), res, EmitOpts{Dir: store, Diag: io.Discard})
	recs, err := ReadAll(store, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	return recs
}

func TestEmitForReconcile_AmbiguousSingletonIsChargedAsAnUncorroboratedRaise(t *testing.T) {
	recs := emitAmbiguousRun(t, t.TempDir(), reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Category: "correctness", Reviewers: []string{"sasha"}}},
		},
		Ambiguous: []reconcile.AmbiguousCluster{
			// The production shape singletonAmbiguousCluster writes: Reviewer
			// singular, Reviewers nil.
			{Findings: []reconcile.Finding{
				{File: "b.go", Line: 9, Problem: "solo security nit", Category: "security", Reviewer: "sasha"},
			}},
		},
	})

	sasha := findReviewer(recs, "sasha")
	require.NotNil(t, sasha)
	assert.Equal(t, 2, sasha.FindingsRaised, "the filtered solo WAS raised, so it charges the denominator")
	assert.Equal(t, 0, sasha.FindingsCorroborated, "and it is never corroboration")
	assert.Equal(t, 2, sasha.FindingsSolo)
	assert.Equal(t, 1, sasha.FindingsAmbiguous, "the charged count is recorded so the ceiling and explain can see it")
	assert.InDelta(t, isolatedFindingWeight, sasha.WeightedCredit, 1e-9,
		"credit comes from the surviving finding only; a filtered solo earns none")
	assert.Equal(t, []string{"correctness", "security"}, sasha.CategoriesRaised,
		"its category is a charged finding's word, so the charge mirror holds")
	assert.InDelta(t, 0.0, sasha.CorroborationRate, 1e-9)
}

func TestEmitForReconcile_GrayPairMemberIsNotChargedTwice(t *testing.T) {
	// A gray pair's members are guarded out of the noise exclusion, so they
	// reach res.Findings too and are charged there. Charging the ambiguous
	// copy as well would count each one twice.
	recs := emitAmbiguousRun(t, t.TempDir(), reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "x leaks", Category: "security", Reviewers: []string{"sasha"}}},
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "x leaks a handle", Category: "performance", Reviewers: []string{"penny"}}},
		},
		Ambiguous: []reconcile.AmbiguousCluster{
			{Findings: []reconcile.Finding{
				{File: "a.go", Line: 1, Problem: "x leaks", Category: "security", Reviewer: "sasha"},
				{File: "a.go", Line: 1, Problem: "x leaks a handle", Category: "performance", Reviewer: "penny"},
			}},
		},
	})

	for _, name := range []string{"sasha", "penny"} {
		r := findReviewer(recs, name)
		require.NotNil(t, r, name)
		assert.Equal(t, 1, r.FindingsRaised, "%s: charged once, through res.Findings", name)
		assert.Equal(t, 0, r.FindingsAmbiguous, "%s: a gray member is category-only on this stream", name)
	}
}

func TestEmitForReconcile_SameReviewerGrayPairIsNotCharged(t *testing.T) {
	// The discriminator is the CLUSTER'S SIZE, not its distinct-reviewer count.
	// A gray pair can be two near-duplicate findings by one reviewer, and both
	// of those reach res.Findings like any other gray member.
	recs := emitAmbiguousRun(t, t.TempDir(), reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "x leaks", Category: "security", Reviewers: []string{"sasha"}}},
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "x leaks badly", Category: "security", Reviewers: []string{"sasha"}}},
		},
		Ambiguous: []reconcile.AmbiguousCluster{
			{Findings: []reconcile.Finding{
				{File: "a.go", Line: 1, Problem: "x leaks", Category: "security", Reviewer: "sasha"},
				{File: "a.go", Line: 1, Problem: "x leaks badly", Category: "security", Reviewer: "sasha"},
			}},
		},
	})

	sasha := findReviewer(recs, "sasha")
	require.NotNil(t, sasha)
	assert.Equal(t, 2, sasha.FindingsRaised)
	assert.Equal(t, 0, sasha.FindingsAmbiguous)
}

func TestEmitForReconcile_AmbiguousChargeStillMintsNoRecord(t *testing.T) {
	// The charge lands on an EXISTING record only; the stream still never
	// creates one. On the EST-2052 corpus every singleton's reviewer already
	// had a record, so this boundary costs no evidence.
	recs := emitAmbiguousRun(t, t.TempDir(), reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Category: "correctness", Reviewers: []string{"bruce"}}},
		},
		Ambiguous: []reconcile.AmbiguousCluster{
			{Findings: []reconcile.Finding{
				{File: "b.go", Line: 9, Problem: "solo", Category: "security", Reviewer: "sasha"},
			}},
		},
	})

	assert.Nil(t, findReviewer(recs, "sasha"))
	agg := recs[len(recs)-1]
	require.Equal(t, RecordTypeAggregate, agg.RecordType)
	assert.Equal(t, 1, agg.FindingsRaised, "an unregistered reviewer's singleton charges nobody")
}

func TestScrubForgedCredit_CeilingExcludesAmbiguousCharges(t *testing.T) {
	// A charged ambiguous finding earns no credit, so it must not widen the
	// ceiling: one surviving solo plus two charged singletons can carry at most
	// one finding's worth of credit, not three.
	honest := reviewer_("r", "Sasha", "m1", 3, 0)
	honest.FindingsAmbiguous = 2
	honest.WeightedCredit = maxPerFindingCredit(isolatedFindingWeight)
	honest.CreditEra = CreditEraCurrent

	forged := honest
	forged.WeightedCredit = 3 * maxPerFindingCredit(isolatedFindingWeight)

	out := scrubForgedCredit([]Record{honest, forged})
	assert.Equal(t, CreditEraCurrent, out[0].CreditEra, "an honest record is left measured")
	assert.Equal(t, 0, out[1].CreditEra, "credit above (raised - routed - ambiguous) is un-measured")
	assert.Equal(t, 3, out[1].FindingsRaised, "un-measuring never touches the counts")
}

func TestTrustPriors_AmbiguousOutOfLanePhantomsNoLongerPublishAtOne(t *testing.T) {
	// The TD-048 escape on the production strict path, end to end through
	// EmitForReconcile. trust.go's phase-gate probe put a hundred phantoms on
	// dax's records directly; in production the consensus filter routes each
	// uncorroborated phantom into res.Ambiguous, the record reached the trust
	// fold with FindingsRaised == 0, the opportunity gate dropped it, and dax
	// stayed at 1.00 — above reconcile's trustHighThreshold.
	store := t.TempDir()
	now := time.Now().UTC()
	emit := func(i int, res reconcile.Result) {
		t.Helper()
		reviewDir := t.TempDir()
		_, err := fanout.WritePool(filepath.Join(reviewDir, "sources", "pool"), []fanout.Result{
			{Agent: "dax", Status: fanout.StatusOK, Content: "x", Model: "m1"},
			{Agent: "sasha", Status: fanout.StatusOK, Content: "x", Model: "m1"},
		}, nil)
		require.NoError(t, err)
		res.Summary = reconcile.Summary{
			ReconciledAt:   now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			ConsensusLevel: "strict",
		}
		EmitForReconcile(reviewDir, res, EmitOpts{Dir: store, Diag: io.Discard})
	}
	for i := 0; i < 20; i++ {
		emit(i, reconcile.Result{Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "t.go", Line: i + 1, Problem: "missing test", Category: reclib.CategoryTesting, Reviewers: []string{"dax", "sasha"}}},
		}})
	}
	for i := 0; i < 20; i++ {
		res := reconcile.Result{Findings: []reconcile.Merged{
			// sasha keeps the run's union discriminating.
			{Finding: reconcile.Finding{File: "s.go", Line: 1, Problem: "real injection", Category: reclib.CategorySecurity, Reviewers: []string{"sasha"}}},
		}}
		for k := 0; k < 5; k++ {
			res.Ambiguous = append(res.Ambiguous, reconcile.AmbiguousCluster{Findings: []reconcile.Finding{
				{File: "p.go", Line: k + 1, Problem: fmt.Sprintf("phantom %d", k), Category: reclib.CategorySecurity, Reviewer: "dax"},
			}})
		}
		emit(100+i, res)
	}

	rates, err := TrustPriors(store, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.Contains(t, rates, "dax")
	// 20 corroborated of (20 + 100) raised.
	assert.InDelta(t, 20.0/120.0, rates["dax"], 1e-9,
		"a hundred filtered out-of-lane phantoms must reach the denominator")
	assert.Less(t, rates["dax"], 0.3, "and leave the lens demoted, not exempt at 1.00")
}
