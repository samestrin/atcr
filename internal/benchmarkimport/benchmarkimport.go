// Package benchmarkimport converts Alibaba's aacr-bench dataset
// (github.com/alibaba/aacr-bench, Apache-2.0) into an ATCR benchmark suite
// directory that satisfies the manifest contract in docs/benchmark.md.
//
// All logic here is I/O-free apart from explicitly injected fetchers, so it
// stays unit-testable; process wiring lives in cmd/ingest-alibaba-benchmark.
package benchmarkimport

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
)

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

// ParseDataset decodes a positive_samples.json payload and rejects any record
// that cannot yield a diff. Failing here rather than mid-ingestion keeps a
// malformed upstream release from producing a half-built suite.
func ParseDataset(raw []byte) ([]Record, error) {
	var recs []Record
	if err := json.Unmarshal(raw, &recs); err != nil {
		return nil, fmt.Errorf("parsing dataset: %w", err)
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("dataset holds no records")
	}
	for i, r := range recs {
		if strings.TrimSpace(r.GithubPrURL) == "" {
			return nil, fmt.Errorf("record %d: githubPrUrl is required", i)
		}
		if strings.TrimSpace(r.SourceCommit) == "" || strings.TrimSpace(r.TargetCommit) == "" {
			return nil, fmt.Errorf("record %d (%s): both source_commit and target_commit are required", i, r.GithubPrURL)
		}
		if len(r.Comments) == 0 {
			return nil, fmt.Errorf("record %d (%s): no comments, so it carries no ground truth", i, r.GithubPrURL)
		}
	}
	return recs, nil
}

// Sample selects a deterministic subset of size n for the given seed.
//
// Records are canonicalized by URL before shuffling so the selection depends on
// the seed alone — a reordered upstream release yields the same sample. The
// result is sorted for the same reason: the emitted suite, and therefore its
// reproducibility hash, must not vary with input ordering.
func Sample(recs []Record, n int, seed int64) ([]Record, error) {
	if n <= 0 {
		return nil, fmt.Errorf("sample size must be positive, got %d", n)
	}
	pool := make([]Record, len(recs))
	copy(pool, recs)
	sort.Slice(pool, func(i, j int) bool { return pool[i].GithubPrURL < pool[j].GithubPrURL })

	if n > len(pool) {
		n = len(pool)
	}
	//nolint:gosec // deterministic reproducibility is the point; this is not a security context.
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })

	out := pool[:n]
	sort.Slice(out, func(i, j int) bool { return out[i].GithubPrURL < out[j].GithubPrURL })
	return out, nil
}
