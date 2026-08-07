// Package benchmarkimport converts Alibaba's aacr-bench dataset
// (github.com/alibaba/aacr-bench, Apache-2.0) into an ATCR benchmark suite
// directory that satisfies the manifest contract in docs/benchmark.md.
//
// All logic here is I/O-free apart from explicitly injected fetchers, so it
// stays unit-testable; process wiring lives in cmd/ingest-alibaba-benchmark.
package benchmarkimport

// Comment is one expert-verified review comment attached to a PR record.
type Comment struct {
	IsAIComment bool   `json:"is_ai_comment"`
	Note        string `json:"note"`
	Path        string `json:"path"`
	Side        string `json:"side"`
	SourceModel string `json:"source_model"`
	FromLine    int    `json:"from_line"`
	ToLine      int    `json:"to_line"`
	Category    string `json:"category"`
	Context     string `json:"context"`
}

// Record is one pull-request entry from dataset/positive_samples.json.
type Record struct {
	ChangeLineCount     int       `json:"change_line_count"`
	ProjectMainLanguage string    `json:"project_main_language"`
	SourceCommit        string    `json:"source_commit"`
	TargetCommit        string    `json:"target_commit"`
	GithubPrURL         string    `json:"githubPrUrl"`
	Comments            []Comment `json:"comments"`
	Category            string    `json:"category"`
}

// ParseDataset decodes a positive_samples.json payload.
func ParseDataset(raw []byte) ([]Record, error) {
	return nil, nil
}

// Sample selects a deterministic subset of size n for the given seed.
func Sample(recs []Record, n int, seed int64) ([]Record, error) {
	return nil, nil
}
