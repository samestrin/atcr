// Package benchmark defines the standard-suite contract for the Model-Eval
// Leaderboard (Epic 10.0): a versioned suite manifest of fixed-diff cases with
// planted-defect expected categories, a deterministic reproducibility hash over
// that content, and the suite-tagged public submission envelope.
//
// This package is the bounded in-repo half of `atcr benchmark`. It ships the
// CONTRACT (Load/Validate/ReproHash), the suite-tagged Submission envelope, and
// the RunResult input contract that `atcr benchmark export` consumes. It does NOT
// execute reviews against the suite or score findings: live execution + scoring
// is Epic 10.1, and the curated standard-v1 suite CONTENT lives in the external
// atcr/benchmark-suite repo (Task 3). Keeping execution out keeps this package
// stdlib + scorecard-type only, with no live-LLM dependency.
package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/samestrin/atcr/internal/version"
)

// Manifest is a versioned benchmark suite: a stable set of diff cases that any
// user can run to produce comparable scores. suite_version is what pins
// reproducibility — only suite runs (not cherry-picked production runs) are
// eligible for the public board, so the suite identity travels with every
// submission.
type Manifest struct {
	Suite        string `json:"suite"`
	SuiteVersion string `json:"suite_version"`
	Cases        []Case `json:"cases"`
}

// Case is one fixed-diff benchmark case. Diff is a path RELATIVE to the suite
// directory (never absolute, never escaping it — enforced by Validate).
// ExpectedCategories are the planted-defect categories a competent reviewer
// should surface; the scoring engine that consumes them is Epic 10.1.
type Case struct {
	ID                 string   `json:"id"`
	Diff               string   `json:"diff"`
	ExpectedCategories []string `json:"expected_categories"`
}

// Load reads <suitePath>/suite.json, validates the manifest structurally, and
// confirms every case's diff file exists on disk. It returns a clear error
// (rather than a half-built manifest) on any failure, so a caller never runs a
// partially-valid suite.
func Load(suitePath string) (*Manifest, error) {
	manifestPath := filepath.Join(suitePath, "suite.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("reading suite manifest %s: %w", manifestPath, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing suite manifest %s: %w", manifestPath, err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("invalid suite manifest %s: %w", manifestPath, err)
	}
	for _, c := range m.Cases {
		diffPath := filepath.Join(suitePath, c.Diff)
		fi, err := os.Stat(diffPath)
		if err != nil {
			return nil, fmt.Errorf("case %q diff file: %w", c.ID, err)
		}
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("case %q diff file %q is not a regular file", c.ID, c.Diff)
		}
	}
	return &m, nil
}

// Validate enforces the structural contract: non-empty suite/version, at least
// one case, and for every case a non-empty unique id, a non-empty relative diff
// path that does not escape the suite directory, and at least one expected
// category. It does NOT touch the filesystem (Load does the existence check), so
// it is usable on an in-memory manifest.
func (m *Manifest) Validate() error {
	if strings.TrimSpace(m.Suite) == "" {
		return fmt.Errorf("suite name is required")
	}
	if strings.TrimSpace(m.SuiteVersion) == "" {
		return fmt.Errorf("suite_version is required")
	}
	if len(m.Cases) == 0 {
		return fmt.Errorf("suite must define at least one case")
	}
	seen := make(map[string]bool, len(m.Cases))
	for i, c := range m.Cases {
		if strings.TrimSpace(c.ID) == "" {
			return fmt.Errorf("case %d: id is required", i)
		}
		if seen[c.ID] {
			return fmt.Errorf("case %q: duplicate id", c.ID)
		}
		seen[c.ID] = true
		if strings.TrimSpace(c.Diff) == "" {
			return fmt.Errorf("case %q: diff path is required", c.ID)
		}
		if !isSafeRelPath(c.Diff) {
			return fmt.Errorf("case %q: diff path %q must be relative and within the suite directory", c.ID, c.Diff)
		}
		if len(c.ExpectedCategories) == 0 {
			return fmt.Errorf("case %q: at least one expected_category is required", c.ID)
		}
		seenCats := make(map[string]bool, len(c.ExpectedCategories))
		for _, cat := range c.ExpectedCategories {
			if strings.TrimSpace(cat) == "" {
				return fmt.Errorf("case %q: expected_category must not be empty or blank", c.ID)
			}
			if seenCats[cat] {
				return fmt.Errorf("case %q: duplicate expected_category %q", c.ID, cat)
			}
			seenCats[cat] = true
		}
	}
	return nil
}

// isSafeRelPath rejects absolute paths and any path that, once cleaned, escapes
// the suite directory (a leading ".." segment). This is the suite-manifest's
// path-traversal guard: a malicious or buggy suite must not make Load stat an
// arbitrary file outside the suite tree.
func isSafeRelPath(p string) bool {
	if filepath.IsAbs(p) {
		return false
	}
	clean := filepath.Clean(p)
	if clean == "." {
		return false
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// ReproHash returns a deterministic SHA-256 over the suite's reproducible
// content: the suite identity, each case's id + expected categories, and the
// BYTES of each case's diff file. Cases are sorted by id first, so manifest
// ordering does not affect the hash — content does. Two suites with identical
// cases and diff bytes hash equally; a single changed diff byte changes the hash.
// This is the `atcr benchmark verify` reproducibility anchor.
func ReproHash(suitePath string) (string, error) {
	m, err := Load(suitePath)
	if err != nil {
		return "", err
	}
	cases := make([]Case, len(m.Cases))
	copy(cases, m.Cases)
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })

	h := sha256.New()
	// Suite identity. Length-prefixing each field prevents ambiguity between
	// adjacent fields (e.g. suite "ab"+version "c" vs "a"+"bc").
	writeField(h, m.Suite)
	writeField(h, m.SuiteVersion)
	for _, c := range cases {
		writeField(h, c.ID)
		cats := make([]string, len(c.ExpectedCategories))
		copy(cats, c.ExpectedCategories)
		sort.Strings(cats)
		for _, cat := range cats {
			writeField(h, cat)
		}
		diffBytes, err := os.ReadFile(filepath.Join(suitePath, c.Diff))
		if err != nil {
			return "", fmt.Errorf("hashing case %q diff: %w", c.ID, err)
		}
		writeField(h, string(diffBytes))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeField writes a length-prefixed field to the hash so concatenation is
// unambiguous across field boundaries.
func writeField(h io.Writer, s string) {
	_, _ = fmt.Fprintf(h, "%d:", len(s))
	_, _ = io.WriteString(h, s)
}

// RunResult is the input contract `atcr benchmark export` consumes: the
// model-eval aggregates produced by a suite run, tagged with the suite identity.
// Epic 10.0 ships this minimal shape (the fields export needs); Epic 10.1's
// `atcr benchmark run` produces conforming files (and may add scoring detail)
// under ~/.config/atcr/benchmark/<run-id>.json. Reviewers reuse the single
// public reviewer schema so benchmark and production submissions share columns.
//
// PRIVACY CONTRACT: Reviewers MUST already be anonymized by the producer (the
// scorecard aggregation that `atcr benchmark run` uses scrubs identity strings
// at source, exactly like `leaderboard --export`). BuildSubmission wraps these
// records verbatim and does NOT re-scrub, so a hand-crafted or non-conforming
// run-result could carry PII into a public submission. A defense-in-depth
// re-scrub at export time is tracked as tech debt (see TD: benchmark export
// re-anonymization).
type RunResult struct {
	Suite        string                   `json:"suite"`
	SuiteVersion string                   `json:"suite_version"`
	GeneratedAt  string                   `json:"generated_at"`
	Reviewers    []scorecard.PublicRecord `json:"reviewers"`
}

// Submission is the suite-tagged public submission envelope — DISTINCT from the
// production scorecard export by the source/suite/suite_version fields. Only
// suite-sourced submissions (source == "benchmark-suite") are eligible for the
// public board, which is what prevents cherry-picked production runs from gaming
// it.
type Submission struct {
	SubmissionSchema int                      `json:"submission_schema"`
	AtcrVersion      string                   `json:"atcr_version"`
	SubmittedAt      string                   `json:"submitted_at"`
	Source           string                   `json:"source"`
	Suite            string                   `json:"suite"`
	SuiteVersion     string                   `json:"suite_version"`
	Reviewers        []scorecard.PublicRecord `json:"reviewers"`
}

// SourceBenchmarkSuite marks a submission as produced by the standard suite (not
// a production review). The public board accepts only this source.
const SourceBenchmarkSuite = "benchmark-suite"

// MaxDiffBytes is the per-file size cap for diff files read during ReproHash.
// A hostile or accidental multi-GB diff in an externally-sourced suite must not
// cause unbounded memory allocation. Set to 0 to reject all diffs (used by tests).
var MaxDiffBytes = int64(10 * 1024 * 1024) // 10 MiB

// BuildSubmission wraps a suite RunResult in the public submission envelope,
// stamping the schema version, build version, source marker, and submittedAt.
// submittedAt is passed in (not time.Now) so the result is reproducible.
func BuildSubmission(rr RunResult, submittedAt time.Time) Submission {
	return Submission{
		SubmissionSchema: scorecard.SubmissionSchema,
		AtcrVersion:      version.Version,
		SubmittedAt:      submittedAt.UTC().Format(time.RFC3339),
		Source:           SourceBenchmarkSuite,
		Suite:            rr.Suite,
		SuiteVersion:     rr.SuiteVersion,
		Reviewers:        rr.Reviewers,
	}
}
