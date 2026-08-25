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
	// it would close (see the cost-denominator comment in score.go's
	// scoreOne for the same argument applied to the unit).
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
	// It exists because the micro-averaged run-level scalar cannot name the drifting
	// model and actively hides it; PerReviewerVocabulary states that argument once and
	// is the place to change it.
	//
	// Rows are positionally aligned with Reviewers (both sorted by the same
	// (model, persona) key with the same stable sort), so a consumer reads the
	// all-`other` signature by correlating entry i's RoutingValues with
	// Reviewers[i].CorroborationRate — which is why recall is not duplicated here.
	//
	// Run-result-only: it gates nothing and belongs to no public schema, so
	// BuildSubmission does not carry it into a Submission. (Coverage was
	// run-result-only for the same reason until submission_schema 2 gave the board a
	// concrete need for it — a partial run that could not be told from a full one.
	// This field has no such consumer, so it stays put.) omitempty
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

	// COVERAGE IS CARRIED HERE AS OF submission_schema 2 (epic 35.16.6.2); the
	// constant is shared with the production `leaderboard --export` envelope, so
	// the bump versioned both — see "Schema versioning" in docs/scorecard.md.
	// Coverage still does NOT live on scorecard.PublicRecord, so the production
	// export's key set is unchanged.
	//
	// NIL POLICY — one rule for the whole envelope, stated here and not restated
	// per function: where the JSON layer can distinguish absent from empty, that
	// distinction is the contract. Keys WITH omitempty (SuiteCaseIDs, Coverage)
	// preserve nil, so an unmeasured run reads as an ABSENT key rather than
	// "measured as empty". Keys WITHOUT it (Reviewers, and each row's CaseIDs)
	// are always arrays, never null — a decoder never needs a null branch.

	// SuiteCaseIDs is the suite's full case-id list, in manifest order — the
	// denominator every Coverage row is short of or equal to. Copied from
	// RunResult.SuiteCaseIDs as-is, omitempty included: a run-result written before
	// coverage existed carries no list, and the key must then be ABSENT rather than
	// null, so a consumer reads "nobody measured" instead of "measured as empty".
	// That is the same unmeasured-vs-short distinction RunResult and the export gate
	// already depend on.
	SuiteCaseIDs []string `json:"suite_case_ids,omitempty"`

	// Coverage names the cases behind each reviewer row, joined to Reviewers by the
	// (Model, Persona) pair. It is a TRIMMED projection of RunResult.Coverage — see
	// SubmissionCoverage for why the run-result's outcomes/fallback_cases are not
	// carried. omitempty for the same unmeasured-vs-short reason as SuiteCaseIDs.
	Coverage []SubmissionCoverage `json:"reviewer_coverage,omitempty"`
}

// SubmissionCoverage is the PUBLIC, trimmed coverage row: which suite cases one
// reviewer row actually scored, and nothing else. ReviewerCoverage's Outcomes and
// FallbackCases are run-level diagnostics and stay out of the public allowlist
// (docs/scorecard.md); the board needs only the covered-case SET to tell a full
// run from a short one. The shared field names and JSON keys match
// ReviewerCoverage's, so a consumer reading either document reads the same shape.
type SubmissionCoverage struct {
	Model   string `json:"model"`
	Persona string `json:"persona"`
	// CaseIDs are the suite case ids this row scored, in manifest order. Compared
	// as a SET against Submission.SuiteCaseIDs; a row short of the suite was
	// measured over less than the full benchmark.
	CaseIDs []string `json:"case_ids"`
}

// MarshalJSON makes the "case_ids is always an array, never null" contract
// structural: it holds no matter how the row was constructed, so a writer that
// builds SubmissionCoverage directly cannot bypass it the way routing through
// publicCoverage used to be required.
func (c SubmissionCoverage) MarshalJSON() ([]byte, error) {
	type alias SubmissionCoverage // no recursion through this method
	if c.CaseIDs == nil {
		c.CaseIDs = []string{}
	}
	return json.Marshal(alias(c))
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
//
// PROJECTION ONLY — it validates nothing. It re-scrubs identities and case ids as
// defense-in-depth, but every coverage INVARIANT is the caller's:
// suite_case_ids and reviewer_coverage being written together, each covered id being
// a suite member, no duplicates, and no id that scrubs to empty or collides with
// another. `atcr benchmark export` enforces all of them (cli/benchmark_coverage.go
// checkCoverage plus validateScrubbedCaseIDs) before calling this.
//
// So a DIFFERENT caller can produce documents the docs say cannot exist — e.g.
// Coverage set with SuiteCaseIDs nil yields coverage rows with no denominator.
// Any new caller owes the same checks; this function will not supply them.
// Submission.Validate exists for exactly that caller.
func BuildSubmission(rr RunResult, submittedAt time.Time) Submission {
	// Defense-in-depth re-scrub: rr.Reviewers — and the suite identity — may come
	// from an externally-supplied run-result, so re-apply the field scrub here
	// rather than trusting the producer (see PRIVACY CONTRACT above). anchorSuiteDenominator
	// compares the PRE-scrub rr.Suite against the manifest, so scrubbing only this
	// projection does not disturb anchoring.
	scrubbed := make([]scorecard.PublicRecord, len(rr.Reviewers))
	for i, rev := range rr.Reviewers {
		scrubbed[i] = scorecard.ScrubPublicRecord(rev)
		// Deep-copy the pointer metrics: a PublicRecord struct copy aliases them,
		// so mutating the submission would rewrite the caller's RunResult — the
		// same non-mutation rule the string slices below follow.
		if rev.SurvivedSkepticRate != nil {
			v := *rev.SurvivedSkepticRate
			scrubbed[i].SurvivedSkepticRate = &v
		}
		if rev.CostPerCorroboratedFindingUSD != nil {
			v := *rev.CostPerCorroboratedFindingUSD
			scrubbed[i].CostPerCorroboratedFindingUSD = &v
		}
	}
	// Sort the SCRUBBED reviewers with the same comparator publicCoverage sorts its
	// rows by, so reviewers[i] and reviewer_coverage[i] are one row.
	//
	// rr.Reviewers arrives in the producer's order, and cli/benchmark_run.go emits
	// coverage in that same order — but publicCoverage then re-sorts, on the SCRUBBED
	// pair, for the determinism guarantee. Copying Reviewers through unsorted broke
	// the positional join that buildRunResult documents ("a consumer can join coverage
	// to its row positionally as well as by identity"), and nothing upstream requires
	// a hand-supplied run-result's rows to arrive sorted at all.
	//
	// Sorting HERE rather than dropping publicCoverage's sort keeps both properties:
	// one order for the two arrays, and byte-identical output for two run-results
	// with identical logical content in different row orders.
	sort.SliceStable(scrubbed, func(i, j int) bool {
		return modelPersonaLess(scrubbed[i].Model, scrubbed[i].Persona, scrubbed[j].Model, scrubbed[j].Persona)
	})
	// One memo for the whole projection: covered ids are (after the export gate)
	// a subset of the suite ids, so without it every row re-scrubs the same list
	// the denominator just paid for — R+1 full passes over the case set.
	scrubMemo := make(map[string]string, len(rr.SuiteCaseIDs))
	return Submission{
		SubmissionSchema: scorecard.SubmissionSchema,
		AtcrVersion:      version.Version,
		SubmittedAt:      submittedAt.UTC().Format(time.RFC3339),
		Source:           SourceBenchmarkSuite,
		Suite:            scrubID(rr.Suite),
		SuiteVersion:     scrubID(rr.SuiteVersion),
		Reviewers:        scrubbed,
		SuiteCaseIDs:     scrubIDs(rr.SuiteCaseIDs, scrubMemo),
		Coverage:         publicCoverage(rr.Coverage, scrubMemo),
	}
}

// Validate checks the envelope invariants the submission docs promise a consumer:
// suite_case_ids and reviewer_coverage written together or both absent, a
// denominator with no empty or repeated id, every covered id a member of that
// denominator, every row's case_ids an array (never null), and every row joined
// to a reviewers[] identity.
//
// BuildSubmission validates NOTHING — it is a projection — so any caller that did
// not come through `atcr benchmark export`'s RunResult gate owes this call before
// publishing. The CLI makes it too, as a backstop beneath its sharper
// diagnostic-level checks.
func (s Submission) Validate() error {
	if (len(s.SuiteCaseIDs) == 0) != (len(s.Coverage) == 0) {
		return fmt.Errorf("submission carries %d suite_case_ids but %d reviewer_coverage rows; "+
			"the two are written together or both absent", len(s.SuiteCaseIDs), len(s.Coverage))
	}
	denominator := make(map[string]bool, len(s.SuiteCaseIDs))
	for _, id := range s.SuiteCaseIDs {
		if id == "" {
			return fmt.Errorf("submission carries an empty suite_case_ids entry")
		}
		if denominator[id] {
			return fmt.Errorf("submission lists suite case %q more than once", id)
		}
		denominator[id] = true
	}
	joined := make(map[[2]string]bool, len(s.Reviewers))
	for _, r := range s.Reviewers {
		joined[[2]string{r.Model, r.Persona}] = true
	}
	for _, c := range s.Coverage {
		if !joined[[2]string{c.Model, c.Persona}] {
			return fmt.Errorf("submission records coverage for %q/%q with no matching reviewers[] row",
				c.Model, c.Persona)
		}
		if c.CaseIDs == nil {
			return fmt.Errorf("submission records a null case_ids for %q/%q; the key is always an array",
				c.Model, c.Persona)
		}
		for _, id := range c.CaseIDs {
			if id == "" {
				return fmt.Errorf("submission records an empty covered case id for %q/%q", c.Model, c.Persona)
			}
			if !denominator[id] {
				return fmt.Errorf("submission records covered case %q for %q/%q, which is not in suite_case_ids",
					id, c.Model, c.Persona)
			}
		}
	}
	return nil
}

// scrubID applies the reviewer-identity scrub to one untrusted case id, borrowing
// scorecard.ScrubPublicString so the identity and case-id rules cannot diverge —
// both sit in the same envelope under the same "no paths, emails, or credentials"
// contract (docs/scorecard.md). BuildSubmission's defense-in-depth re-scrub depends
// on the scrub being idempotent; scrubField guarantees it.
func scrubID(s string) string {
	return scorecard.ScrubPublicString(s)
}

// scrubIDs returns a scrubbed COPY of ids, preserving nil per the Submission nil
// policy: `benchmark export` validates coverage against the caller's RunResult
// before building the submission, so scrubbing in place would rewrite the file's
// own data underneath a caller that may still read or re-validate it.
func scrubIDs(ids []string, memo map[string]string) []string {
	if ids == nil {
		return nil
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		s, ok := memo[id]
		if !ok {
			s = scrubID(id)
			memo[id] = s
		}
		out[i] = s
	}
	return out
}

// publicCoverage projects run-result coverage rows onto the trimmed public row.
// Two invariants live here: a row's nil CaseIDs is normalized to an empty slice —
// case_ids has no omitempty and is always an array, never null — and identities go
// through scorecard.ScrubPublicRecord, the same function as Reviewers, so the
// (Model, Persona) join between the two arrays cannot diverge. CaseIDs get the same
// scrub via scrubIDs.
func publicCoverage(rows []ReviewerCoverage, memo map[string]string) []SubmissionCoverage {
	if rows == nil {
		return nil
	}
	out := make([]SubmissionCoverage, len(rows))
	for i, c := range rows {
		id := scorecard.ScrubPublicRecord(scorecard.PublicRecord{Model: c.Model, Persona: c.Persona})
		ids := scrubIDs(c.CaseIDs, memo)
		if ids == nil {
			ids = []string{}
		}
		out[i] = SubmissionCoverage{
			Model:   id.Model,
			Persona: id.Persona,
			CaseIDs: ids,
		}
	}
	// Deterministic row order: two run-results with identical logical content but
	// differently-ordered rows must marshal to the same bytes, the guarantee
	// scorecard.Export makes for the production envelope.
	//
	// modelPersonaLess, not a second copy of the comparator: score.go's doc comment
	// says the positional alignment "rests on this ONE definition, not on duplicated
	// comparators drifting apart", and the duplicate here was exactly that drift
	// risk. BuildSubmission sorts the scrubbed Reviewers with the same call, which is
	// what makes reviewers[i] and reviewer_coverage[i] one row — the earlier claim
	// that this order "matches how the producer sorted Reviewers" was true of the
	// coverage half only.
	sort.SliceStable(out, func(i, j int) bool {
		return modelPersonaLess(out[i].Model, out[i].Persona, out[j].Model, out[j].Persona)
	})
	return out
}
