package benchmarkimport

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/benchmark"
)

// DiffFetcher retrieves the unified diff between two commits of a GitHub repo.
// It is an interface so ingestion can be unit-tested without network access:
// the live implementations live in fetch.go and run only at authoring time.
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
	// Skipped counts records whose categories were all outside ATCR's vocabulary.
	Skipped int
	// Unavailable counts records whose diff no longer exists upstream.
	Unavailable int
	// UnmappedCategories counts individual comments dropped because their
	// category had no mapping, even when a sibling comment mapped and the
	// record was kept. Without it a partial miss is invisible, and the shrunken
	// expected set inflates the recall scored against the suite.
	UnmappedCategories int
}

// ErrDiffUnavailable marks a record whose diff cannot be retrieved because the
// upstream commits are gone (force-pushed or garbage-collected). It is a
// property of that one pull request, so ingestion drops the record and
// continues; every other fetch error aborts the build.
var ErrDiffUnavailable = errors.New("diff unavailable upstream")

// categoryMap translates aacr-bench's comment categories into ATCR's reviewer
// vocabulary (personas/_base.md:44). Keys are lowercased on lookup. Both the
// dataset's actual literals and the shorter spellings used in the epic body are
// accepted. A comment whose category has no mapping is dropped from the
// expected set — and tallied: ExpectedCategories returns the count so the
// build can report it, because a silently shrunken expected set would inflate
// the scored recall of every reviewer run against the suite.
var categoryMap = map[string]string{
	"code defect":                     "correctness",
	"security vulnerability":          "security",
	"security":                        "security",
	"maintainability and readability": "maintainability",
	"maintainability":                 "maintainability",
	"performance":                     "performance",
}

// prURLPattern captures owner, repo, and PR number from a GitHub PR URL. It is
// anchored and constrained to GitHub's real namespace charset: the dataset is
// third-party JSON, and owner/repo flow into a compare-API path and an
// unescaped clone URL, so a loose match would admit traversal- and query-shaped
// values the consumers cannot safely carry.
var prURLPattern = regexp.MustCompile(`^https?://github\.com/([A-Za-z0-9][A-Za-z0-9-]*)/([A-Za-z0-9._-]+)/pull/(\d+)/?$`)

// slugUnsafe matches everything that must not appear in a case id or filename.
var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// MapCategory translates an aacr-bench category to ATCR's vocabulary. The bool
// reports whether the category is known; callers must not guess at a default.
func MapCategory(in string) (string, bool) {
	out, ok := categoryMap[strings.ToLower(strings.TrimSpace(in))]
	return out, ok
}

// ExpectedCategories returns the deduped, sorted ATCR categories for a record
// plus the number of comments whose category had no mapping and was dropped.
// Sorting and deduping are required, not cosmetic: the manifest contract
// rejects a duplicate category within a case, and the reproducibility hash is
// computed over the category list. The unmapped tally must travel with the
// result — a partial miss is otherwise invisible, and internal/benchmark
// computes recall against the shrunken set, flattering every score.
func ExpectedCategories(rec Record) (cats []string, unmapped int) {
	seen := make(map[string]struct{}, len(rec.Comments))
	for _, c := range rec.Comments {
		if mapped, ok := MapCategory(c.Category); ok {
			seen[mapped] = struct{}{}
		} else {
			unmapped++
		}
	}
	out := make([]string, 0, len(seen))
	for cat := range seen {
		out = append(out, cat)
	}
	sort.Strings(out)
	return out, unmapped
}

// CaseID derives a stable, slug-safe case id from a record's PR URL. Case ids
// may legally contain "/" per the manifest contract, but a flat slug keeps the
// id usable as its own diff filename.
//
// The slug is NOT injective: it erases the owner/repo boundary, so
// foo-bar/baz, foo/bar-baz, and foo/bar.baz all derive the same id. Callers
// writing files keyed by id must go through uniqueCaseID, which resolves
// collisions without disturbing the committed suite's ids.
func CaseID(rec Record) (string, error) {
	m := prURLPattern.FindStringSubmatch(strings.TrimSpace(rec.GithubPrURL))
	if m == nil {
		return "", fmt.Errorf("cannot derive a case id from %q: not a GitHub pull-request URL", rec.GithubPrURL)
	}
	slug := func(s string) string {
		return strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	}
	id := fmt.Sprintf("%s-%s-pr-%s", slug(m[1]), slug(m[2]), m[3])
	if id == "" || strings.HasPrefix(id, "-") {
		return "", fmt.Errorf("derived an empty case id from %q", rec.GithubPrURL)
	}
	return id, nil
}

// uniqueCaseID resolves rec's case id against the ids already issued in this
// build. CaseID's dash-slug erases the owner/repo boundary — foo-bar/baz,
// foo/bar-baz, and foo/bar.baz all derive the same id — so a collision must
// never reach the diff write: the second record's bytes would overwrite the
// first's on disk before manifest.Validate could complain. The committed
// suite's ids are immutable (they are pinned golden and content-hashed), so
// the plain format is kept whenever possible and only a genuine collision is
// disambiguated, deterministically, with a short hash of the full URL.
func uniqueCaseID(rec Record, taken map[string]string) (string, error) {
	id, err := CaseID(rec)
	if err != nil {
		return "", err
	}
	prev, dup := taken[id]
	if !dup {
		return id, nil
	}
	if prev == rec.GithubPrURL {
		return "", fmt.Errorf("case id %q: record %q appears twice in the input", id, rec.GithubPrURL)
	}
	sum := sha256.Sum256([]byte(rec.GithubPrURL))
	id = fmt.Sprintf("%s-%x", id, sum[:4])
	if other, stillDup := taken[id]; stillDup {
		return "", fmt.Errorf("case id collision between %q and %q persists after disambiguation (%q already holds it)",
			prev, rec.GithubPrURL, other)
	}
	return id, nil
}

// BuildSuite writes suite.json plus one diff file per case into OutDir.
//
// Every record is fetched and written before the manifest is emitted, and a
// fetch failure aborts the whole build: a half-written suite would still load,
// so a partial ingestion must never look like a complete one. The one exception
// is ErrDiffUnavailable — a pull request whose commits are gone upstream is a
// property of that record, so it is dropped and counted.
func BuildSuite(ctx context.Context, opts Options) (Result, error) {
	var res Result

	if opts.Fetcher == nil {
		return res, fmt.Errorf("a diff fetcher is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return res, fmt.Errorf("an output directory is required")
	}
	// Suite identity is validated up front: a typo'd flag must fail before any
	// network fetch or file write, not after the API budget is spent and the
	// output directory is littered (manifest.Validate would only catch it at
	// the end).
	if strings.TrimSpace(opts.Suite) == "" {
		return res, fmt.Errorf("a suite name is required")
	}
	if strings.TrimSpace(opts.SuiteVersion) == "" {
		return res, fmt.Errorf("a suite version is required")
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return res, fmt.Errorf("creating suite directory: %w", err)
	}

	manifest := benchmark.Manifest{Suite: opts.Suite, SuiteVersion: opts.SuiteVersion}

	seenIDs := make(map[string]string, len(opts.Records))
	for _, rec := range opts.Records {
		// Records may be constructed programmatically, bypassing ParseDataset's
		// commit validation; re-assert it at the trust boundary so an
		// option-shaped value can never reach git or the compare API path.
		if !commitSHAPattern.MatchString(rec.SourceCommit) || !commitSHAPattern.MatchString(rec.TargetCommit) {
			return res, fmt.Errorf("record %q: source_commit and target_commit must each be a 7-40 character hex SHA", rec.GithubPrURL)
		}

		cats, unmapped := ExpectedCategories(rec)
		res.UnmappedCategories += unmapped
		if len(cats) == 0 {
			res.Skipped++
			continue
		}

		id, err := uniqueCaseID(rec, seenIDs)
		if err != nil {
			return res, err
		}
		seenIDs[id] = rec.GithubPrURL

		owner, repo, err := repoFromPrURL(rec.GithubPrURL)
		if err != nil {
			return res, err
		}

		diff, err := opts.Fetcher.FetchDiff(ctx, owner, repo, rec.SourceCommit, rec.TargetCommit)
		if errors.Is(err, ErrDiffUnavailable) {
			res.Unavailable++
			continue
		}
		if err != nil {
			return res, fmt.Errorf("case %q: fetching diff: %w", id, err)
		}
		// A blank diff produces no reviewable entries, and the benchmark runner
		// fails the entire run — not just that case — when ingestion yields none.
		if len(strings.TrimSpace(string(diff))) == 0 {
			return res, fmt.Errorf("case %q: fetched diff is empty", id)
		}

		name := id + ".diff"
		if err := os.WriteFile(filepath.Join(opts.OutDir, name), diff, 0o644); err != nil {
			return res, fmt.Errorf("case %q: writing diff: %w", id, err)
		}

		manifest.Cases = append(manifest.Cases, benchmark.Case{
			ID:                 id,
			Diff:               name,
			ExpectedCategories: cats,
		})
		res.CasesWritten++
	}

	if len(manifest.Cases) == 0 {
		return res, fmt.Errorf("no records yielded a mappable category: refusing to write an empty suite")
	}

	// Sort by id so the manifest ordering is reproducible regardless of the
	// order records arrived in.
	sort.Slice(manifest.Cases, func(i, j int) bool { return manifest.Cases[i].ID < manifest.Cases[j].ID })

	if err := manifest.Validate(); err != nil {
		return res, fmt.Errorf("emitted manifest is invalid: %w", err)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return res, fmt.Errorf("encoding manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(opts.OutDir, "suite.json"), data, 0o644); err != nil {
		return res, fmt.Errorf("writing suite.json: %w", err)
	}

	return res, nil
}

func repoFromPrURL(raw string) (owner, repo string, err error) {
	m := prURLPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return "", "", fmt.Errorf("cannot parse owner/repo from %q", raw)
	}
	return m[1], m[2], nil
}
