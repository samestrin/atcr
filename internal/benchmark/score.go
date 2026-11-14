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
// A case with no expected categories contributes zero recall and is excluded
// from the CorroborationRate denominator so it cannot silently drag the rate
// down. Runs and FindingsRaisedAvg still reflect total case volume.
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
	// output the reproducibility AC requires even on an identity tie. The comparator
	// is modelPersonaLess, the ONE (model, persona) ordering in this package:
	// PerReviewerVocabulary sorts by it too, so reviewers[i] and
	// reviewer_vocabulary[i] are the same row by construction, not by two parallel
	// comparator copies staying in sync.
	sort.SliceStable(out, func(i, j int) bool {
		return modelPersonaLess(out[i].Model, out[i].Persona, out[j].Model, out[j].Persona)
	})
	return out
}

// modelPersonaLess is the single (model, persona) ordering behind both Score's
// reviewers[] and PerReviewerVocabulary's reviewer_vocabulary[]: the positional
// alignment between the two rests on this ONE definition, not on duplicated
// comparators drifting apart.
func modelPersonaLess(aModel, aPersona, bModel, bPersona string) bool {
	if aModel != bModel {
		return aModel < bModel
	}
	return aPersona < bPersona
}

// scoreOne computes the public metrics for a single reviewer before scrubbing.
func scoreOne(r ReviewerScore) scorecard.PublicRecord {
	pr := scorecard.PublicRecord{
		Model:   r.Model,
		Persona: r.Persona,
		Runs:    len(r.Cases),
		// Stamped explicitly, and NOT with the production era. PublicRecord is
		// shared with `leaderboard --export`, but sharing the TYPE does not
		// populate the field — a struct literal here reaches none of the
		// production export's accumulation, so without this every benchmark row
		// would publish `raised_denominator: 0`, an undefined era, onto the same
		// public board.
		//
		// The value is the benchmark one because the numbers below are not the
		// production quantities: corroboration_rate carries CATEGORY RECALL here
		// and findings_raised_avg is mean findings per case. Claiming the current
		// production definition would be false, and it is the claim a board would
		// act on. See scorecard.RaisedDenominatorBenchmarkSuite.
		RaisedDenominator: scorecard.RaisedDenominatorBenchmarkSuite,
		LatencyP50MS:      r.LatencyP50MS,
	}
	if len(r.Cases) == 0 {
		return pr
	}

	var totalFindings, matchedFindings, ratedCases int
	var recallSum float64
	for _, c := range r.Cases {
		m := matchCase(c)
		totalFindings += len(c.Raised)

		if m.expected > 0 {
			ratedCases++
			recallSum += float64(m.hit) / float64(m.expected)
		}
		// Cost-per-corroborated denominator: every finding whose category satisfied
		// an expected (planted) category.
		//
		// The unit is FINDINGS, not distinct categories, and that is deliberate. It
		// mirrors the production producer scorecard.costPer, whose denominator
		// accumulates FindingsCorroborated — a count of findings — because
		// cost_per_corroborated_finding_usd is not a benchmark field: it sits on the
		// frozen PublicRecord allowlist shared verbatim between `benchmark export`
		// and production `leaderboard --export`. It also matches CaseScore.Raised's
		// one-entry-per-finding semantics and the out-of-vocabulary rate's denominator.
		// Pinned by TestScore_CostDenominatorCountsFindingsNotDistinctCategories —
		// every other cost fixture raises all-distinct categories and so pins the same
		// number under either unit.
		//
		// KNOWN GAMING SURFACE, accepted rather than closed. The widened satisfying
		// set admits six words for `maintainability`, so a reviewer emitting one nit
		// six times under style/naming/bloat/complexity/duplication/maintainability
		// buys six denominator and publishes a 6x cheaper number with no extra
		// detection. Exact matching made the same inflation visible as duplicate-label
		// spam; varied family labels read as legitimate. The magnitude is unchanged —
		// six findings bought six before too — only the detectability. The
		// counter-signal is findings_raised_avg, which sits on the same published row
		// and rises in lockstep, so compare the two.
		//
		// Do NOT "fix" this by switching the unit here. Forking a frozen shared key's
		// meaning between its two producers — cost-per-finding from production,
		// cost-per-detected-category from the benchmark, distinguishable only by the
		// envelope's source tag — is a worse defect than the hole. A unit change must
		// version or rename the metric, in an epic scoped to touch PublicRecord.
		matchedFindings += m.matched
	}

	pr.FindingsRaisedAvg = float64(totalFindings) / float64(len(r.Cases))
	if ratedCases > 0 {
		pr.CorroborationRate = clamp01(recallSum / float64(ratedCases))
	}
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

// caseMatch is one case's category outcome: the distinct expected categories,
// how many of them the reviewer hit, and how many of its findings matched one.
type caseMatch struct {
	expected int
	hit      int
	matched  int
}

// matchCase scores one case. It is the ONE definition of "matched" behind recall,
// the cost denominator and FalseAlarms, so the three never disagree about which
// finding matched.
func matchCase(c CaseScore) caseMatch {
	// The expected side needs only the distinct set, so it uses normalizeDistinct
	// rather than normalizeSet — whose parallel per-finding slice exists for the
	// RAISED side alone and would otherwise be built and discarded every case.
	expected := normalizeDistinct(c.Expected)
	raised, normalizedRaised := normalizeSet(c.Raised)

	// Both quantities below resolve through the equivalence relation in
	// equivalence.go: a coarse expected category is satisfied by any member of
	// the ATCR family it spans, so a reviewer that writes `style` for a planted
	// `maintainability` defect is credited for detecting it. The relation is
	// scorer-side only — no product category is merged — and reduces to exact
	// matching for any expected category with no family.
	//
	// One walk of the families yields both: hit (recall's numerator, decided
	// per expected category) and satisfying (their union, the matched-finding
	// membership test).
	hit, satisfying := satisfactions(expected, raised)

	// Drive the count off normalizedRaised so this pass normalizes each finding
	// once rather than twice. It is NOT once per finding per RUN: OutOfVocabularyRate
	// is a separate top-level walk over the same ReviewerScores and normalizes them
	// again, and FalseAlarms calls this function a second time. That duplication is
	// deliberate — it keeps the run diagnostics off Score's signature and out of the
	// frozen PublicRecord — and is negligible against an LLM-bound run.
	m := caseMatch{expected: len(expected), hit: hit}
	for _, cat := range normalizedRaised {
		if satisfying[cat] {
			m.matched++
		}
	}
	return m
}

// ReviewerFalseAlarms is one reviewer's false-alarm figure across the suite: the
// findings whose category satisfied no expected category of their case (epic 113,
// owner answer Q3).
//
// It is an UPPER BOUND on false alarms, not a count of them. expected_categories is
// the planted-defect SUBSET, not exhaustive ground truth, so an unmatched finding
// may be a real defect the suite never planted. That is the same reason Score
// reports no precision; this figure is the raw volume that precision would have
// hidden, named for what it can and cannot say. Telling a real extra find from a
// false alarm needs a person reading the finding.
//
// Run-result-only, like ReviewerVocabulary (owner answer Q7): scorecard.PublicRecord
// is the frozen schema shared with production `leaderboard --export`, and
// BuildSubmission does not carry this array forward.
type ReviewerFalseAlarms struct {
	Model   string `json:"model"`
	Persona string `json:"persona"`

	// Cases, Findings and Unmatched are the counts behind UnmatchedPerCase,
	// published beside it so a reader can tell 1/1 from 40/40.
	Cases     int `json:"cases"`
	Findings  int `json:"findings"`
	Unmatched int `json:"unmatched"`

	// UnmatchedPerCase is Unmatched / Cases, the same per-case denominator
	// findings_raised_avg uses. A POINTER so a reviewer with no cases is
	// unmeasured rather than a 0.0 that reads as "no false alarms".
	UnmatchedPerCase *float64 `json:"unmatched_per_case,omitempty"`
}

// FalseAlarms computes each reviewer's ReviewerFalseAlarms row. A finding is
// unmatched when matchCase does not count it as matched, so the figure and
// cost_per_corroborated_finding_usd's denominator are complements over the same
// findings. An empty category matches nothing and counts, and every finding of a
// case that plants no category counts.
//
// Rows are scrubbed and sorted exactly as Score's are, so entry i describes
// reviewers[i] (see PerReviewerVocabulary for the same contract). nil when there
// are no reviewers, so the run-result omits the key.
func FalseAlarms(reviewers []ReviewerScore) []ReviewerFalseAlarms {
	if len(reviewers) == 0 {
		return nil
	}
	out := make([]ReviewerFalseAlarms, 0, len(reviewers))
	for _, r := range reviewers {
		id := scorecard.ScrubPublicRecord(scorecard.PublicRecord{Model: r.Model, Persona: r.Persona})
		e := ReviewerFalseAlarms{Model: id.Model, Persona: id.Persona, Cases: len(r.Cases)}
		for _, c := range r.Cases {
			e.Findings += len(c.Raised)
			e.Unmatched += len(c.Raised) - matchCase(c).matched
		}
		if e.Cases > 0 {
			v := float64(e.Unmatched) / float64(e.Cases)
			e.UnmatchedPerCase = &v
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return modelPersonaLess(out[i].Model, out[i].Persona, out[j].Model, out[j].Persona)
	})
	return out
}

// normalize lowercases and trims a category so matching is case-insensitive and
// whitespace-insensitive, mirroring reconcile.ModalCategory.
func normalize(cat string) string { return strings.ToLower(strings.TrimSpace(cat)) }

// normalizeSet returns the distinct non-empty normalized categories in cats
// and a parallel slice of every normalized value (preserving order, including
// empty entries) so a caller can iterate findings without re-normalizing them
// within the same pass.
func normalizeSet(cats []string) (map[string]bool, []string) {
	set := make(map[string]bool, len(cats))
	normalized := make([]string, len(cats))
	for i, c := range cats {
		n := normalize(c)
		normalized[i] = n
		if n != "" {
			set[n] = true
		}
	}
	return set, normalized
}

// normalizeDistinct returns just the distinct non-empty normalized categories —
// normalizeSet without the parallel per-finding slice, for callers that only need
// the set. Same membership semantics; the two must stay in step.
func normalizeDistinct(cats []string) map[string]bool {
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
