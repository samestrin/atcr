package benchmark

import (
	"math"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/scorecard"
)

// CaseScore is one reviewer's outcome on a single benchmark case: the case's
// expected (planted-defect) categories and the category of every finding the
// reviewer raised for that case. Raised carries one entry per finding (duplicates
// allowed) so FindingsRaisedAvg counts findings, not distinct categories.
type CaseScore struct {
	Expected []string
	Raised   []string
}

// ReviewerScore is the full per-reviewer input to Score: identity, recorded
// usage, and per-case outcomes across the suite. CostUSD and LatencyP50MS are
// sourced by the run orchestrator from the pool usage the providers reported; a
// stub completer reports none, so both stay 0 and a no-usage run's score is
// deterministic.
type ReviewerScore struct {
	Model        string
	Persona      string
	Cases        []CaseScore
	CostUSD      float64
	LatencyP50MS int64
}

// Score folds each reviewer's per-case category outcomes into the single public
// reviewer schema. It does NOT modify the scorecard package: it emits
// scorecard.PublicRecord values and re-scrubs each via scorecard.ScrubPublicRecord
// (defense in depth — the same pass BuildSubmission applies — so identity PII can
// never reach a public submission even from a non-conforming producer).
//
// Every CaseScore.Expected must be non-empty: a case with no expected categories
// contributes zero recall while still counting in the macro-average denominator,
// silently lowering the reported CorroborationRate.
//
// CorroborationRate carries CATEGORY RECALL: the macro-average across the
// reviewer's cases of (distinct expected categories the reviewer surfaced at least
// one matching finding for) / (distinct expected categories). This repurposes the
// only rate field the frozen PublicRecord carries as the benchmark proxy the risk
// table sanctions ("consistent with 10.0's corroboration caveat"); the
// source=="benchmark-suite" tag on the Submission disambiguates it from production
// cross-reviewer corroboration. FindingsRaisedAvg is the mean findings per case and
// Runs is the number of cases scored.
//
// Precision against expected_categories is intentionally NOT computed: the expected
// set is the planted-defect SUBSET, not exhaustive ground truth, so a
// precision-vs-planted metric would penalize a thorough reviewer that also surfaces
// legitimate non-planted issues. Recall measures "did you catch the planted
// defects"; FindingsRaisedAvg already exposes volume without that penalty.
//
// Records are returned sorted ascending by (model, persona), so the same input
// always produces byte-identical output.
func Score(reviewers []ReviewerScore) []scorecard.PublicRecord {
	out := make([]scorecard.PublicRecord, 0, len(reviewers))
	for _, r := range reviewers {
		out = append(out, scorecard.ScrubPublicRecord(scoreOne(r)))
	}
	// Stable so that two reviewers sharing the same (model, persona) keep the
	// orchestrator's deterministic input order — preserving the byte-identical
	// output the reproducibility AC requires even on an identity tie.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Model != out[j].Model {
			return out[i].Model < out[j].Model
		}
		return out[i].Persona < out[j].Persona
	})
	return out
}

// scoreOne computes the public metrics for a single reviewer before scrubbing.
func scoreOne(r ReviewerScore) scorecard.PublicRecord {
	pr := scorecard.PublicRecord{
		Model:        r.Model,
		Persona:      r.Persona,
		Runs:         len(r.Cases),
		LatencyP50MS: r.LatencyP50MS,
	}
	if len(r.Cases) == 0 {
		return pr
	}

	var totalFindings, matchedFindings int
	var recallSum float64
	for _, c := range r.Cases {
		expected := normalizeSet(c.Expected)
		raised := normalizeSet(c.Raised)
		totalFindings += len(c.Raised)

		if len(expected) > 0 {
			hit := 0
			for cat := range expected {
				if raised[cat] {
					hit++
				}
			}
			recallSum += float64(hit) / float64(len(expected))
		}
		// Cost-per-corroborated denominator: every finding whose category matched
		// an expected (planted) category. Counts findings, not distinct categories.
		for _, cat := range c.Raised {
			if expected[normalize(cat)] {
				matchedFindings++
			}
		}
	}

	pr.FindingsRaisedAvg = float64(totalFindings) / float64(len(r.Cases))
	pr.CorroborationRate = clamp01(recallSum / float64(len(r.Cases)))
	// matchedFindings == 0 leaves the field nil: cost-per-corroborated is
	// undefined (a priced reviewer that matched nothing must not read the same
	// as a genuinely free reviewer), so omitempty drops the key entirely. A
	// non-nil value (including a real 0.0, e.g. a free reviewer with matches)
	// mirrors the production export path in scorecard.costPer.
	if matchedFindings > 0 && !math.IsNaN(r.CostUSD) && !math.IsInf(r.CostUSD, 0) && r.CostUSD >= 0 {
		v := r.CostUSD / float64(matchedFindings)
		pr.CostPerCorroboratedFindingUSD = &v
	}
	return pr
}

// normalize lowercases and trims a category so matching is case-insensitive and
// whitespace-insensitive, mirroring reconcile.ModalCategory.
func normalize(cat string) string { return strings.ToLower(strings.TrimSpace(cat)) }

// normalizeSet returns the distinct non-empty normalized categories in cats.
func normalizeSet(cats []string) map[string]bool {
	set := make(map[string]bool, len(cats))
	for _, c := range cats {
		if n := normalize(c); n != "" {
			set[n] = true
		}
	}
	return set
}

// clamp01 bounds a rate to [0,1]; a well-formed recall is already in range, this
// guards a corrupt input from emitting an out-of-range public rate.
func clamp01(f float64) float64 {
	switch {
	case f < 0:
		return 0
	case f > 1:
		return 1
	default:
		return f
	}
}
