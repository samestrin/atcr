package benchmark

import "sort"

// RepoStateCaseScore is one reviewer's outcome on a single repo-state case: the
// per-expected-finding match results MatchFindings produced. It carries MATCHES
// rather than raw findings because the positional decision — which report settled
// which expectation — is made once, by the matcher, and re-deriving it here would
// be a second implementation of the tie-break rules to keep in step.
type RepoStateCaseScore struct {
	CaseID  string
	Matches []FindingMatch
}

// RepoStateReviewerScore is the full per-reviewer input to ScorePositional: one
// identity and its outcomes across the suite. It mirrors ReviewerScore's shape so
// the two scorers read the same way, but stays a separate type — AC2 turns on
// standard-v1's CaseScore semantics being untouched, and a shared type would put
// both tiers one field away from each other.
type RepoStateReviewerScore struct {
	Model   string
	Persona string
	Cases   []RepoStateCaseScore
}

// ReviewerPositionalRecall is one reviewer's LOCATED-finding recall across a
// repo-state suite, with the out-of-diff half reported separately.
//
// The separation is the point (AC4). A single blended score can improve for
// unrelated reasons; a reviewer that finds every in-diff defect and no out-of-diff
// one has demonstrated exactly the gap this tier exists to measure, and averaging
// the two halves would report that reviewer as mediocre-at-everything rather than
// as blind to unchanged code.
//
// Every rate is a POINTER and every rate has its counts beside it. The counts are
// what make a rate auditable — 1/2 and 50/100 are the same rate and not the same
// evidence — and the pointer keeps an unmeasured rate distinguishable from a
// measured zero, the rule CostPerCorroboratedFindingUSD and OutOfVocabularyRate
// already follow on this type's siblings.
type ReviewerPositionalRecall struct {
	Model   string `json:"model"`
	Persona string `json:"persona"`

	// ExpectedTotal / MatchedTotal / Recall cover every expected finding in the
	// suite, both halves together.
	ExpectedTotal int      `json:"expected_total"`
	MatchedTotal  int      `json:"matched_total"`
	Recall        *float64 `json:"recall,omitempty"`

	// The out-of-diff half: findings whose settling line the diff does not touch.
	// This is the number epic 35.16.8's AC5 needs in order to be falsifiable.
	ExpectedOutsideDiff int      `json:"expected_outside_diff"`
	MatchedOutsideDiff  int      `json:"matched_outside_diff"`
	OutsideDiffRecall   *float64 `json:"outside_diff_recall,omitempty"`

	// The in-diff half, reported too rather than left to be derived. A reader
	// comparing the two halves is the intended use, and a derived number invites
	// each consumer to re-derive it slightly differently.
	ExpectedWithinDiff int      `json:"expected_within_diff"`
	MatchedWithinDiff  int      `json:"matched_within_diff"`
	WithinDiffRecall   *float64 `json:"within_diff_recall,omitempty"`
}

// ScorePositional folds per-case match outcomes into per-reviewer recall.
//
// The rates are MICRO-averaged over findings, not macro-averaged over cases, which
// is the opposite of Score's choice for CorroborationRate and deliberately so. A
// macro-average needs a per-case rate, and a case that plants no out-of-diff
// finding has none — every answer for it (0, 1, or excluded) is an invention. The
// suite is also small and its out-of-diff findings are sparse, so macro weighting
// would let a case planting one such finding outweigh a case planting five.
//
// Rows come back sorted by modelPersonaLess — the SAME comparator Score and
// publicCoverage use, not a second copy — so reviewers[i] and
// reviewer_positional_recall[i] are one row, the positional join Coverage and
// Vocabulary already depend on.
func ScorePositional(reviewers []RepoStateReviewerScore) []ReviewerPositionalRecall {
	if len(reviewers) == 0 {
		return nil
	}
	out := make([]ReviewerPositionalRecall, 0, len(reviewers))
	for _, r := range reviewers {
		out = append(out, positionalOne(r))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return modelPersonaLess(out[i].Model, out[i].Persona, out[j].Model, out[j].Persona)
	})
	return out
}

// positionalOne computes one reviewer's recall row.
func positionalOne(r RepoStateReviewerScore) ReviewerPositionalRecall {
	pr := ReviewerPositionalRecall{Model: r.Model, Persona: r.Persona}
	for _, c := range r.Cases {
		for _, m := range c.Matches {
			outside := m.Expected.IsOutsideDiff()
			pr.ExpectedTotal++
			if outside {
				pr.ExpectedOutsideDiff++
			} else {
				pr.ExpectedWithinDiff++
			}
			if !m.Matched {
				continue
			}
			pr.MatchedTotal++
			if outside {
				pr.MatchedOutsideDiff++
			} else {
				pr.MatchedWithinDiff++
			}
		}
	}
	pr.Recall = rate(pr.MatchedTotal, pr.ExpectedTotal)
	pr.OutsideDiffRecall = rate(pr.MatchedOutsideDiff, pr.ExpectedOutsideDiff)
	pr.WithinDiffRecall = rate(pr.MatchedWithinDiff, pr.ExpectedWithinDiff)
	return pr
}

// rate returns matched/expected, or nil when nothing was expected.
//
// nil rather than 0 because the two say opposite things. A suite that plants no
// out-of-diff finding gives a reviewer nothing to miss, and reporting 0.0 would
// claim they missed findings that were never there — on the one metric whose whole
// job is being believed. The counts beside the rate let a consumer tell which case
// it is without guessing.
func rate(matched, expected int) *float64 {
	if expected <= 0 {
		return nil
	}
	v := clamp01(float64(matched) / float64(expected))
	return &v
}
