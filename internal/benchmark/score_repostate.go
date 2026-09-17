package benchmark

// RepoStateCaseScore is one reviewer's outcome on a single repo-state case.
type RepoStateCaseScore struct {
	CaseID  string
	Matches []FindingMatch
}

// RepoStateReviewerScore is the full per-reviewer input to ScorePositional.
type RepoStateReviewerScore struct {
	Model   string
	Persona string
	Cases   []RepoStateCaseScore
}

// ReviewerPositionalRecall is one reviewer's located-finding recall.
type ReviewerPositionalRecall struct {
	Model               string   `json:"model"`
	Persona             string   `json:"persona"`
	ExpectedTotal       int      `json:"expected_total"`
	MatchedTotal        int      `json:"matched_total"`
	Recall              *float64 `json:"recall,omitempty"`
	ExpectedOutsideDiff int      `json:"expected_outside_diff"`
	MatchedOutsideDiff  int      `json:"matched_outside_diff"`
	OutsideDiffRecall   *float64 `json:"outside_diff_recall,omitempty"`
	ExpectedWithinDiff  int      `json:"expected_within_diff"`
	MatchedWithinDiff   int      `json:"matched_within_diff"`
	WithinDiffRecall    *float64 `json:"within_diff_recall,omitempty"`
}

// ScorePositional folds per-case match outcomes into per-reviewer recall.
func ScorePositional(reviewers []RepoStateReviewerScore) []ReviewerPositionalRecall {
	return nil
}
