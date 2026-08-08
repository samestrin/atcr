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
	"regexp"
	"sort"
	"strings"
)

// commitSHAPattern bounds the commit fields to a plain hex object name.
var commitSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// Comment is one review comment attached to a PR record. Upstream flags
// LLM-proposed comments via is_ai_comment and source_model — a majority of the
// dataset's comments are model-proposed and retained after human review, not
// human-authored (see benchmarks/standard-v1/NOTICE.md). IsAIComment is decoded
// for provenance but deliberately does not filter ExpectedCategories.
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
	seen := make(map[string]int, len(recs))
	for i, r := range recs {
		if strings.TrimSpace(r.GithubPrURL) == "" {
			return nil, fmt.Errorf("record %d: githubPrUrl is required", i)
		}
		// The URL is validated at the parse boundary alongside the commit SHAs:
		// owner/repo reach the compare API as path segments and the clone URL
		// unescaped, so only the canonical bare-PR shape is accepted.
		if !prURLPattern.MatchString(strings.TrimSpace(r.GithubPrURL)) {
			return nil, fmt.Errorf("record %d: githubPrUrl is not a canonical GitHub PR URL: %s", i, r.GithubPrURL)
		}
		// The PR URL is the sample's sort key and the case id's basis. A duplicate
		// would make sampling order-dependent (sort.Slice is not stable) and would
		// emit two cases with the same id, which the manifest contract rejects.
		if prev, dup := seen[r.GithubPrURL]; dup {
			return nil, fmt.Errorf("record %d duplicates record %d: %s", i, prev, r.GithubPrURL)
		}
		seen[r.GithubPrURL] = i
		// Commit values arrive from downloaded third-party JSON and are passed to
		// git as bare arguments and interpolated into an API path. A value
		// starting with "-" would be read by git as an option (--upload-pack=...),
		// so the shape is constrained here rather than trusted.
		if !commitSHAPattern.MatchString(r.SourceCommit) || !commitSHAPattern.MatchString(r.TargetCommit) {
			return nil, fmt.Errorf("record %d (%s): source_commit and target_commit must each be a 7-40 character hex SHA", i, r.GithubPrURL)
		}
		if len(r.Comments) == 0 {
			return nil, fmt.Errorf("record %d (%s): no comments, so it carries no ground truth", i, r.GithubPrURL)
		}
	}
	return recs, nil
}

// Sample selects a deterministic subset of size n for the given seed.
//
// Records are canonicalized before shuffling so the selection depends on the
// seed alone — a reordered upstream release yields the same sample. The result
// is sorted for the same reason: the emitted suite, and therefore its
// reproducibility hash, must not vary with input ordering.
//
// The uniqueness invariant that makes URLs a safe primary key is owned by
// ParseDataset, which rejects duplicate PR URLs. Sample is exported and
// callable with arbitrary records, so its comparator is total (URL, then the
// commit pair): the documented order independence holds even without it.
//
// A request the pool cannot satisfy — a non-positive n, an empty pool, or
// n larger than the pool — is an error, never a silent clamp: an undersized
// suite must fail loudly rather than ship under its requested label.
func Sample(recs []Record, n int, seed int64) ([]Record, error) {
	if n <= 0 {
		return nil, fmt.Errorf("sample size must be positive, got %d", n)
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("dataset holds no records to sample")
	}
	if n > len(recs) {
		return nil, fmt.Errorf("sample size %d exceeds the %d-record pool", n, len(recs))
	}
	pool := make([]Record, len(recs))
	copy(pool, recs)
	sort.Slice(pool, func(i, j int) bool { return recordLess(pool[i], pool[j]) })

	//nolint:gosec // deterministic reproducibility is the point; this is not a security context.
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })

	out := pool[:n]
	sort.Slice(out, func(i, j int) bool { return recordLess(out[i], out[j]) })
	return out, nil
}

// recordLess orders records canonically: PR URL first, then the commit pair,
// so the order is total even when two records share a URL.
func recordLess(a, b Record) bool {
	if a.GithubPrURL != b.GithubPrURL {
		return a.GithubPrURL < b.GithubPrURL
	}
	if a.SourceCommit != b.SourceCommit {
		return a.SourceCommit < b.SourceCommit
	}
	return a.TargetCommit < b.TargetCommit
}
