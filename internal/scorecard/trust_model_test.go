package scorecard

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Trust priors on the reconcile path are keyed on persona AND the model the
// persona runs on now (owner ruling 2026-09-23, option A): a persona that
// switches models starts from the neutral baseline until DefaultTrustMinRuns
// runs accumulate on the new model. These tests drive ratesFromRecords with a
// current-model map; a nil map is the pre-existing persona-only fold.

// modelRuns builds n kept-eligible reviewer records for one persona on one
// model, each on its own run.
func modelRuns(prefix, persona, model string, n, raised, corroborated int) []Record {
	out := make([]Record, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, reviewer_(runIDAt(time.Now(), fmt.Sprintf("%s-%s-%s-%03d", prefix, persona, model, i)),
			persona, model, raised, corroborated))
	}
	return out
}

func ratesForModels(records []Record, models map[string]string) map[string]float64 {
	return ratesFromRecords(records, DefaultTrustMinRuns, nil, defaultTrustWindow, time.Now(), models)
}

// AC1: a model switch resets trust.
func TestRatesFromRecords_ModelSwitchStartsNeutral(t *testing.T) {
	recs := modelRuns("a", "sasha", "m1", DefaultTrustMinRuns, 2, 1)

	assert.NotContains(t, ratesForModels(recs, map[string]string{"sasha": "m2"}), "sasha",
		"no runs on the current model: the old model's trust must not carry over")

	onA := ratesForModels(recs, map[string]string{"sasha": "m1"})
	require.Contains(t, onA, "sasha")
	assert.InDelta(t, 0.5, onA["sasha"], 1e-9)
}

// AC2: only runs on the current model count, for the floor and for the rate.
func TestRatesFromRecords_OnlyCurrentModelRunsCount(t *testing.T) {
	under := append(modelRuns("a", "sasha", "m1", 15, 2, 1), modelRuns("b", "sasha", "m2", 10, 2, 1)...)
	assert.NotContains(t, ratesForModels(under, map[string]string{"sasha": "m2"}), "sasha",
		"10 runs on the current model is under the floor, whatever the old model holds")

	// m1 corroborates half, m2 corroborates everything: the rate must be m2's.
	both := append(modelRuns("a", "sasha", "m1", 25, 2, 1), modelRuns("b", "sasha", "m2", DefaultTrustMinRuns, 2, 2)...)
	rates := ratesForModels(both, map[string]string{"sasha": "m2"})
	require.Contains(t, rates, "sasha")
	assert.InDelta(t, 1.0, rates["sasha"], 1e-9)
}

// AC2: the weighted numerator reads the same current-model runs as the binary
// rate, so old-model credit cannot re-enter through it.
func TestRatesFromRecords_WeightedCreditIsCurrentModelOnly(t *testing.T) {
	credited := func(recs []Record, credit float64) []Record {
		for i := range recs {
			recs[i].CreditEra = CreditEraCurrent
			recs[i].WeightedCredit = credit
		}
		return recs
	}
	recs := append(credited(modelRuns("a", "sasha", "m1", DefaultTrustMinRuns, 2, 1), 0),
		credited(modelRuns("b", "sasha", "m2", DefaultTrustMinRuns, 2, 1), 2)...)
	// A confirmation that clears minConfirmationOutcomes at factor 1.0, so the
	// weighted branch decides the rate.
	gt := func(time.Duration, time.Time) (map[string]Confirmation, error) {
		return map[string]Confirmation{"sasha": {Confirmed: minConfirmationOutcomes}}, nil
	}

	rates := ratesFromRecords(recs, DefaultTrustMinRuns, gt, defaultTrustWindow, time.Now(), map[string]string{"sasha": "m2"})
	require.Contains(t, rates, "sasha")
	assert.InDelta(t, 1.0, rates["sasha"], 1e-9, "credit 40 over raised 40 on m2; m1's zero credit must not dilute it")
}

// AC3: a persona with no known current model is neutral.
func TestRatesFromRecords_PersonaWithoutACurrentModelIsNeutral(t *testing.T) {
	recs := modelRuns("a", "sasha", "m1", DefaultTrustMinRuns, 2, 1)
	assert.NotContains(t, ratesForModels(recs, map[string]string{"dax": "m1"}), "sasha")
	assert.Empty(t, ratesForModels(recs, map[string]string{}), "an empty (non-nil) map means nobody's model is known")
}

// The model comparison ignores case and surrounding space: registries and
// provider responses do not agree on a spelling.
func TestRatesFromRecords_ModelMatchIgnoresCaseAndSpace(t *testing.T) {
	recs := modelRuns("a", "sasha", "Qwen3.8-Max", DefaultTrustMinRuns, 2, 1)
	assert.Contains(t, ratesForModels(recs, map[string]string{"sasha": " qwen3.8-max "}), "sasha")
}

// AC4: a nil map is the unchanged persona-only fold.
func TestRatesFromRecords_NilModelsSumsAcrossModels(t *testing.T) {
	recs := append(modelRuns("a", "sasha", "m1", 10, 2, 1), modelRuns("b", "sasha", "m2", 10, 2, 2)...)
	rates := ratesForModels(recs, nil)
	require.Contains(t, rates, "sasha", "20 runs across two models clear the floor when no model is pinned")
	assert.InDelta(t, 0.75, rates["sasha"], 1e-9)
}

// AC4: another persona's model switch cannot move this persona's rate. The two
// share runs, so the per-run opportunity union is exercised.
func TestRatesFromRecords_OtherPersonaSwitchDoesNotMoveThisRate(t *testing.T) {
	var recs []Record
	for i := 0; i < DefaultTrustMinRuns; i++ {
		run := runIDAt(time.Now(), fmt.Sprintf("shared-%03d", i))
		g := reviewer_(run, "greta", "m1", 2, 1)
		b := reviewer_(run, "bruce", "old", 3, 1)
		b.CategoriesRaised = []string{"security"}
		recs = append(recs, g, b)
	}

	base := ratesForModels(recs, map[string]string{"greta": "m1", "bruce": "old"})
	switched := ratesForModels(recs, map[string]string{"greta": "m1", "bruce": "new"})
	require.Contains(t, base, "greta")
	assert.Equal(t, base["greta"], switched["greta"])
	assert.NotContains(t, switched, "bruce")
}
