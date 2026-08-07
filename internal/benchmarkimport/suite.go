package benchmarkimport

import (
	"context"
)

// DiffFetcher retrieves the unified diff between two commits of a GitHub repo.
type DiffFetcher interface {
	FetchDiff(ctx context.Context, owner, repo, base, head string) ([]byte, error)
}

// Options configures a single suite build.
type Options struct {
	Records      []Record
	OutDir       string
	Suite        string
	SuiteVersion string
	Fetcher      DiffFetcher
}

// Result reports what a suite build produced.
type Result struct {
	CasesWritten int
	Skipped      int
}

// MapCategory translates an aacr-bench category to ATCR's vocabulary.
func MapCategory(in string) (string, bool) {
	return "", false
}

// ExpectedCategories returns the deduped, sorted ATCR categories for a record.
func ExpectedCategories(rec Record) []string {
	return nil
}

// CaseID derives a stable, slug-safe case id from a record's PR URL.
func CaseID(rec Record) (string, error) {
	return "", nil
}

// BuildSuite writes suite.json plus one diff file per case into OutDir.
func BuildSuite(ctx context.Context, opts Options) (Result, error) {
	return Result{}, nil
}
