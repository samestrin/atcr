// Package benchmark defines the standard-suite contract for the Model-Eval
// Leaderboard (Epic 10.0): a versioned suite manifest of fixed-diff cases with
// planted-defect expected categories, a deterministic reproducibility hash over
// that content, and the suite-tagged public submission envelope.
//
// This package is the in-repo contract + scorer half of `atcr benchmark`. It
// ships the CONTRACT (Load/Validate/ReproHash), the suite-tagged Submission
// envelope and the RunResult contract `atcr benchmark export` consumes, and the
// scorer (Score, which folds per-case findings into the public reviewer schema).
// It stays stdlib + scorecard-type only, with no live-LLM dependency: the suite
// EXECUTION loop that drives each case's diff through the review pipeline lives in
// cmd/atcr (the composition root that may import internal/fanout). The curated
// standard-v1 suite CONTENT is bundled at benchmarks/standard-v1/ in this repo.
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
// should surface; Score matches findings against them (case-insensitively).
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
		fi, err := os.Lstat(diffPath)
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
		// Dedupe on the NORMALIZED category, which is how the scorer counts distinct
		// expected categories (normalizeSet). Checking the raw string here would let
		// ["maintainability", "Maintainability"] validate as two categories and then
		// score as one, changing the recall denominator from 2 to 1 with no
		// diagnostic anywhere.
		seenCats := make(map[string]bool, len(c.ExpectedCategories))
		normCats := make([]string, 0, len(c.ExpectedCategories))
		for _, cat := range c.ExpectedCategories {
			n := normalize(cat)
			if n == "" {
				return fmt.Errorf("case %q: expected_category must not be empty or blank", c.ID)
			}
			if seenCats[n] {
				return fmt.Errorf("case %q: duplicate expected_category %q", c.ID, cat)
			}
			seenCats[n] = true
			normCats = append(normCats, n)
		}
		// Make familyOf's identity fallback TOTAL. The fallback overlaps the family
		// table: a case expecting both a coarse category and a member of that
		// category's family lets ONE raised finding satisfy BOTH — Expected
		// ["maintainability","style"] with Raised ["style"] measures recall 1.0 where
		// exact matching gave 0.5. Rejecting the shape here is what lets
		// equivalence.go keep saying an unfamilied expected category is "still scored
		// by exact match, exactly as before": it can no longer ALSO be reached
		// through a sibling's family. Costs valid suites nothing — the families are
		// disjoint (TestEquivalence_FamiliesAreDisjoint), so only a hand-authored
		// suite planting a fine word beside its coarse parent can trip this.
		for i, a := range normCats {
			for j, b := range normCats {
				if i == j {
					continue
				}
				for _, member := range familyOf(b) {
					if member == a {
						return fmt.Errorf("case %q: expected_category %q is already satisfied by %q's equivalence family; one raised finding would satisfy both and inflate recall", c.ID, a, b)
					}
				}
			}
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
// ReproHash loads the suite at suitePath and returns its reproducibility hash.
// Callers that already hold a loaded *Manifest should use ReproHashManifest to
// avoid a redundant Load.
func ReproHash(suitePath string) (string, error) {
	m, err := Load(suitePath)
	if err != nil {
		return "", err
	}
	return ReproHashManifest(m, suitePath)
}

// ReproHashManifest returns the reproducibility hash for an already-loaded
// manifest. It is the implementation body of ReproHash; callers that have
// already called Load (such as `atcr benchmark verify`) can use this directly
// to skip the redundant parse.
func ReproHashManifest(m *Manifest, suitePath string) (string, error) {
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
		diffPath := filepath.Join(suitePath, c.Diff)
		f, err := os.Open(diffPath)
		if err != nil {
			return "", fmt.Errorf("hashing case %q diff: %w", c.ID, err)
		}
		fi, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return "", fmt.Errorf("hashing case %q diff stat: %w", c.ID, err)
		}
		if fi.Size() > MaxDiffBytes {
			_ = f.Close()
			return "", fmt.Errorf("hashing case %q diff: size %d exceeds max %d bytes", c.ID, fi.Size(), MaxDiffBytes)
		}
		// Length-prefix for unambiguous hashing (matches writeField format). h is a
		// sha256 hash whose Write never returns an error, so the discarded write
		// result here (and in writeField) is a safe-to-ignore unreachable failure.
		_, _ = fmt.Fprintf(h, "%d:", fi.Size())
		if _, err := io.Copy(h, f); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("hashing case %q diff: %w", c.ID, err)
		}
		_ = f.Close()
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeField writes a length-prefixed field to the hash so concatenation is
// unambiguous across field boundaries. h is always a sha256 hash (hash.Hash),
// whose Write is contractually documented never to return an error, so the
// discarded write results below are unreachable failures — checking them would
// be dead code. Kept as io.Writer for testability; do not widen the signature to
// return an error for callers that can never observe one.
func writeField(h io.Writer, s string) {
	_, _ = fmt.Fprintf(h, "%d:", len(s))
	_, _ = io.WriteString(h, s)
}

// RunResult is the input contract `atcr benchmark export` consumes: the
// model-eval aggregates produced by a suite run, tagged with the suite identity.
// `atcr benchmark run` (Run) produces conforming values; the Reviewers reuse the
// single public reviewer schema so benchmark and production submissions share
// columns.
//
// PRIVACY CONTRACT: Reviewers SHOULD already be anonymized by the producer (Score
// scrubs identity strings at source via scorecard.ScrubPublicRecord, exactly like
// `leaderboard --export`). As defense-in-depth — because
// `atcr benchmark export` consumes a hand-suppliable run-result file that never
// passed through the producer — BuildSubmission additionally re-scrubs each
// reviewer's identity fields via scorecard.ScrubPublicRecord, so a non-conforming
// run-result cannot carry PII into a public submission. The PublicRecord allowlist
// remains the primary guarantee; this re-scrub is the backstop.
type RunResult struct {
	Suite        string                   `json:"suite"`
	SuiteVersion string                   `json:"suite_version"`
	GeneratedAt  string                   `json:"generated_at"`
	Reviewers    []scorecard.PublicRecord `json:"reviewers"`

	// OutOfVocabularyRate is the share of the run's findings whose category is not
	// a member of the closed reviewer vocabulary. The value is produced by the
	// package-level OutOfVocabularyRate FUNCTION in vocabulary.go, which documents
	// the metric's three definitional choices; this field only carries it. (The
	// function and this field deliberately share a name — say which one you mean at
	// a call site, since `benchmark.OutOfVocabularyRate` alone is ambiguous.)
	//
	// It is a DIAGNOSTIC on the run, not a reviewer metric, which is why it sits
	// here rather than on scorecard.PublicRecord: that type is the frozen public
	// schema shared with the production leaderboard export, and a benchmark-only
	// column does not belong in a public submission. BuildSubmission accordingly
	// does not carry this field forward.
	//
	// A POINTER for the same reason CostPerCorroboratedFindingUSD is one: 0.0 is a
	// real and desirable measurement (perfect vocabulary agreement) and must not
	// read identically to "nobody measured". omitempty drops the key only when the
	// pointer is nil, so a producer that computed a clean run still emits an
	// explicit 0 — while a run-result file predating epic 35.16.5 unmarshals to nil
	// and is honestly reported as unmeasured rather than as flawless.
	OutOfVocabularyRate *float64 `json:"out_of_vocabulary_rate,omitempty"`

	// SuiteCaseIDs is the suite's full case-id list, in manifest order, recorded
	// ONCE for the whole run. It is the denominator every ReviewerCoverage row is
	// measured against: without it a covered case set has nothing to be short of,
	// and `benchmark export` cannot tell a complete run from a partial one.
	//
	// omitempty so a run-result written before this field existed unmarshals to nil
	// and is honestly reported as unmeasured coverage, exactly as a nil
	// OutOfVocabularyRate reports an unmeasured rate.
	SuiteCaseIDs []string `json:"suite_case_ids,omitempty"`

	// Coverage records which cases each reviewer row actually scored, plus the
	// per-row outcome tally. It sits HERE rather than on scorecard.PublicRecord
	// deliberately: that type is the frozen public schema shared byte-for-byte
	// between `benchmark export` and production `leaderboard --export`, and forking
	// a shared schema for a benchmark-only concern is a worse defect than the hole
	// it would close (see score.go:139-143 for the same argument applied to the cost
	// denominator's unit).
	//
	// It exists because Runs is a COUNT, not a SET. Case difficulty on this suite
	// varies enormously, so a recall computed over one 8-case subset is not
	// comparable to a recall computed over a different 8-case subset — and splitting
	// rows by realized model makes uneven coverage the normal case rather than an
	// exceptional one.
	Coverage []ReviewerCoverage `json:"reviewer_coverage,omitempty"`

	// Vocabulary breaks OutOfVocabularyRate down per reviewer row: the same drift,
	// measured the same way, attributed. The value is produced by the package-level
	// PerReviewerVocabulary function in vocabulary.go, which documents the
	// alignment and the routing-value discriminator; this field only carries it.
	//
	// It exists because the run-level scalar is micro-averaged — correctly, since the
	// rate is a property of the run's findings — and micro-averaging cannot name the
	// drifting model. Worse, it hides it: a 12-finding reviewer at 100% drift pooled
	// against a 300-finding clean one reports 0.038, passes the ceiling, and the run
	// reads clean. Tightening the ceiling raises the dilution such a row needs without
	// bounding it, since the ratio is set by the rest of the roster.
	//
	// Rows are positionally aligned with Reviewers (both sorted by the same
	// (model, persona) key with the same stable sort), so a consumer reads the
	// all-`other` signature by correlating entry i's RoutingValues with
	// Reviewers[i].CorroborationRate — which is why recall is not duplicated here.
	//
	// Run-result-only, exactly as Coverage is: it gates nothing and belongs to no
	// public schema, so BuildSubmission does not carry it into a Submission. omitempty
	// drops the key when a new run has no reviewers, so such a run serializes
	// identically to a run-result written before this field existed — and both
	// unmarshal to nil (the key is absent, tag or no tag), reading as "no breakdown
	// recorded" rather than as a run with no reviewers.
	Vocabulary []ReviewerVocabulary `json:"reviewer_vocabulary,omitempty"`
}

// ReviewerCoverage names the cases behind one reviewer row of the same run-result,
// joined to that row by its (Model, Persona) identity — the same pair PublicRecord
// carries and Score sorts on.
type ReviewerCoverage struct {
	Model   string `json:"model"`
	Persona string `json:"persona"`
	// CaseIDs are the suite case ids this row actually scored, in manifest order.
	// Compared as a SET against RunResult.SuiteCaseIDs; a row whose set is short of
	// the suite was measured over less than the full benchmark.
	CaseIDs []string `json:"case_ids"`

	// Outcomes tallies this row's per-case outcomes, keyed by outcome tally key
	// (OutcomeTallyKey: the benchmark.Outcome* wire values, with OutcomeUnknown
	// spelled "unknown"). A map rather than a struct so the tally cannot drift out
	// of step with the enum, and because encoding/json emits map keys sorted —
	// keeping the run-result deterministic. A row with no recorded outcomes omits
	// the key.
	Outcomes map[string]int `json:"outcomes,omitempty"`

	// FallbackCases counts this row's cases that fanout served from a fallback model
	// rather than the slot's configured primary. It is deliberately NOT an outcome
	// enum member: a fallback-served case is independently clean, unparseable, or
	// failed, so folding it into the outcome enum would admit exactly the impossible
	// combined states the enum exists to prevent.
	FallbackCases int `json:"fallback_cases,omitempty"`
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

	// COVERAGE IS DELIBERATELY NOT CARRIED HERE. It would be genuinely useful on the
	// board — an opted-out partial submission is otherwise indistinguishable from a
	// full one — but adding a key to this envelope is a schema-versioning decision,
	// not a documentation fix (docs/benchmark.md, "the run-result contract"), and
	// submission_schema is scorecard.SubmissionSchema: the SHARED constant the
	// production `leaderboard --export` envelope also stamps. Bumping it here would
	// version the production envelope for a benchmark-only reason — the same
	// fork-a-shared-schema hazard that keeps coverage off scorecard.PublicRecord.
	//
	// Coverage therefore lives on RunResult only, where `benchmark export` reads it
	// to gate publication and an operator can inspect it directly. Putting it in the
	// envelope belongs to an epic scoped to bump submission_schema.
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
	// Defense-in-depth re-scrub: rr.Reviewers may come from an externally-supplied
	// run-result, so re-apply the field scrub here rather than trusting the
	// producer (see PRIVACY CONTRACT above).
	scrubbed := make([]scorecard.PublicRecord, len(rr.Reviewers))
	for i, rev := range rr.Reviewers {
		scrubbed[i] = scorecard.ScrubPublicRecord(rev)
	}
	return Submission{
		SubmissionSchema: scorecard.SubmissionSchema,
		AtcrVersion:      version.Version,
		SubmittedAt:      submittedAt.UTC().Format(time.RFC3339),
		Source:           SourceBenchmarkSuite,
		Suite:            rr.Suite,
		SuiteVersion:     rr.SuiteVersion,
		Reviewers:        scrubbed,
	}
}
