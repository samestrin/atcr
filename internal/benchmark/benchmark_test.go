package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/samestrin/atcr/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ValidSuite(t *testing.T) {
	m, err := Load("testdata/suite-valid")
	require.NoError(t, err)
	assert.Equal(t, "fixture-mini", m.Suite)
	assert.Equal(t, "1.0.0", m.SuiteVersion)
	require.Len(t, m.Cases, 2)
	assert.Equal(t, "case-01-nil-deref", m.Cases[0].ID)
	assert.Equal(t, "case-02.diff", m.Cases[1].Diff)
	assert.Equal(t, []string{"security", "correctness"}, m.Cases[1].ExpectedCategories)
}

// The export gate hard-rejects any case id the publication scrub rewrites, but
// every gate fixture uses synthetic "case-01" ids — a future scrub rule touching
// hyphenated tokens (say "-pr-<digits>") would make every submission built from
// the bundled suite fail export while the whole test suite stayed green. Pin the
// ids of the ACTUALLY SHIPPED suite against the scrubber.
func TestStandardV1CaseIDs_SurviveThePublicationScrub(t *testing.T) {
	m, err := Load("../../benchmarks/standard-v1")
	require.NoError(t, err, "the bundled standard-v1 suite must load")
	require.NotEmpty(t, m.Cases, "precondition: the suite has cases")
	for _, c := range m.Cases {
		assert.Equal(t, c.ID, scorecard.ScrubPublicString(c.ID),
			"the publication scrub must not rewrite shipped suite case id %q — "+
				"the export gate rejects rewritten ids, so this suite would become unexportable", c.ID)
	}
}

// BuildSubmission validates nothing by design, so the envelope invariants the
// docs promise need a home a FUTURE caller (an MCP surface, a library consumer)
// will actually find: Validate on the built Submission. Each case below builds a
// document the docs say cannot exist and asserts it is rejected.
func TestSubmission_Validate(t *testing.T) {
	valid := func() Submission {
		return Submission{
			SuiteCaseIDs: []string{"case-01", "case-02"},
			Reviewers:    []scorecard.PublicRecord{{Model: "m", Persona: "p"}},
			Coverage: []SubmissionCoverage{
				{Model: "m", Persona: "p", CaseIDs: []string{"case-01"}},
			},
		}
	}

	require.NoError(t, valid().Validate(), "the healthy envelope validates")
	require.NoError(t, Submission{}.Validate(),
		"both keys absent is the legal unmeasured shape")

	t.Run("coverage without a denominator", func(t *testing.T) {
		s := valid()
		s.SuiteCaseIDs = nil
		require.Error(t, s.Validate())
	})
	t.Run("denominator without coverage", func(t *testing.T) {
		s := valid()
		s.Coverage = nil
		require.Error(t, s.Validate())
	})
	t.Run("repeated denominator id", func(t *testing.T) {
		s := valid()
		s.SuiteCaseIDs = []string{"case-01", "case-01"}
		require.Error(t, s.Validate())
	})
	t.Run("empty denominator id", func(t *testing.T) {
		s := valid()
		s.SuiteCaseIDs = []string{"case-01", ""}
		require.Error(t, s.Validate())
	})
	t.Run("covered id outside the denominator", func(t *testing.T) {
		s := valid()
		s.Coverage[0].CaseIDs = []string{"case-99"}
		require.Error(t, s.Validate())
	})
	t.Run("null row case_ids", func(t *testing.T) {
		s := valid()
		s.Coverage[0].CaseIDs = nil
		require.Error(t, s.Validate(), "the never-null contract is structural, not per-writer")
	})
	t.Run("coverage row with no reviewer", func(t *testing.T) {
		s := valid()
		s.Coverage[0].Model = "someone-else"
		require.Error(t, s.Validate())
	})
}

func TestLoad_MissingSuiteJSON(t *testing.T) {
	_, err := Load(t.TempDir())
	require.Error(t, err, "a directory without suite.json must fail to load")
}

func TestLoad_RejectsMissingDiffFile(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{"suite":"s","suite_version":"1.0.0","cases":[{"id":"c1","diff":"nope.diff","expected_categories":["x"]}]}`)
	_, err := Load(dir)
	require.Error(t, err, "a case whose diff file does not exist must fail to load")
}

func TestLoad_RejectsSymlinkAsDiff(t *testing.T) {
	dir := t.TempDir()
	// Create a target file outside the suite and a symlink inside pointing to it.
	// os.Stat follows symlinks (so the old code accepted them); os.Lstat inspects
	// the link itself, which is not a regular file.
	external := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(external, []byte("sensitive"), 0o600))
	link := filepath.Join(dir, "case.diff")
	require.NoError(t, os.Symlink(external, link))
	writeManifest(t, dir, `{"suite":"s","suite_version":"1.0.0","cases":[{"id":"c1","diff":"case.diff","expected_categories":["x"]}]}`)
	_, err := Load(dir)
	require.Error(t, err, "a symlink used as a diff must be rejected (Load must not follow symlinks outside the suite)")
}

func TestLoad_RejectsDirectoryAsDiff(t *testing.T) {
	dir := t.TempDir()
	// Create a subdirectory that the manifest will point to as the diff "file".
	require.NoError(t, os.Mkdir(filepath.Join(dir, "not-a-file"), 0o700))
	writeManifest(t, dir, `{"suite":"s","suite_version":"1.0.0","cases":[{"id":"c1","diff":"not-a-file","expected_categories":["x"]}]}`)
	_, err := Load(dir)
	require.Error(t, err, "a directory used as a diff must be rejected (only regular files are valid diffs)")
}

func TestValidate_RejectsEmptyAndDuplicateCategories(t *testing.T) {
	cases := map[string]Manifest{
		"empty category string": {
			Suite: "s", SuiteVersion: "1.0.0",
			Cases: []Case{{ID: "c", Diff: "c.diff", ExpectedCategories: []string{""}}},
		},
		"whitespace-only category": {
			Suite: "s", SuiteVersion: "1.0.0",
			Cases: []Case{{ID: "c", Diff: "c.diff", ExpectedCategories: []string{"  "}}},
		},
		"mixed valid and empty": {
			Suite: "s", SuiteVersion: "1.0.0",
			Cases: []Case{{ID: "c", Diff: "c.diff", ExpectedCategories: []string{"security", ""}}},
		},
		"duplicate categories": {
			Suite: "s", SuiteVersion: "1.0.0",
			Cases: []Case{{ID: "c", Diff: "c.diff", ExpectedCategories: []string{"security", "security"}}},
		},
		// The scorer dedupes on the NORMALIZED category (normalizeSet), so a
		// manifest that validates these as two distinct categories then scores them
		// as one — silently halving the recall denominator with no diagnostic.
		// Validate must apply the same notion of distinct that the scorer does.
		"case-differing duplicate": {
			Suite: "s", SuiteVersion: "1.0.0",
			Cases: []Case{{ID: "c", Diff: "c.diff", ExpectedCategories: []string{"maintainability", "Maintainability"}}},
		},
		"whitespace-differing duplicate": {
			Suite: "s", SuiteVersion: "1.0.0",
			Cases: []Case{{ID: "c", Diff: "c.diff", ExpectedCategories: []string{"security", " security "}}},
		},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			require.Error(t, m.Validate(), "%s must be rejected", name)
		})
	}
}

// familyOf's identity fallback OVERLAPS the family table, so a case that plants
// both a coarse category and a member of that category's family lets ONE raised
// finding satisfy TWO distinct expected categories: with Expected
// ["maintainability", "style"] and Raised ["style"], `style` is satisfied by the
// identity fallback AND `maintainability` by the family — measured recall 1.0 where
// exact matching gave 0.5. Validate rejects that shape, which is what makes the
// fallback TOTAL: an expected category with no family of its own is scored by exact
// match and can never also be reachable through another expected category's family.
func TestValidate_RejectsOverlappingExpectedCategories(t *testing.T) {
	overlapping := map[string][]string{
		"fine member alongside its coarse parent": {"maintainability", "style"},
		"declared in the other order":             {"style", "maintainability"},
		"security family":                         {"security", "secret"},
		"normalized before comparing":             {"Maintainability", "  STYLE "},
	}
	for name, cats := range overlapping {
		t.Run(name, func(t *testing.T) {
			m := Manifest{
				Suite: "s", SuiteVersion: "1.0.0",
				Cases: []Case{{ID: "c", Diff: "c.diff", ExpectedCategories: cats}},
			}
			require.Error(t, m.Validate(), "%v: one raised finding would satisfy both", cats)
		})
	}

	// The families are disjoint, so every shape the bundled suite can express stays
	// valid — this guard costs existing suites nothing.
	t.Run("disjoint coarse categories stay valid", func(t *testing.T) {
		m := Manifest{
			Suite: "s", SuiteVersion: "1.0.0",
			Cases: []Case{{ID: "c", Diff: "c.diff",
				ExpectedCategories: []string{"correctness", "security", "maintainability", "performance"}}},
		}
		require.NoError(t, m.Validate())
	})

	// Two fine words, neither of which is a family key, are each scored by exact
	// match and cannot double-count.
	t.Run("two fine words with no family stay valid", func(t *testing.T) {
		m := Manifest{
			Suite: "s", SuiteVersion: "1.0.0",
			Cases: []Case{{ID: "c", Diff: "c.diff", ExpectedCategories: []string{"style", "naming"}}},
		}
		require.NoError(t, m.Validate())
	})
}

func TestValidate_Errors(t *testing.T) {
	cases := map[string]Manifest{
		"empty suite name":     {SuiteVersion: "1.0.0", Cases: []Case{{ID: "c", Diff: "c.diff", ExpectedCategories: []string{"x"}}}},
		"empty suite version":  {Suite: "s", Cases: []Case{{ID: "c", Diff: "c.diff", ExpectedCategories: []string{"x"}}}},
		"no cases":             {Suite: "s", SuiteVersion: "1.0.0"},
		"empty case id":        {Suite: "s", SuiteVersion: "1.0.0", Cases: []Case{{Diff: "c.diff", ExpectedCategories: []string{"x"}}}},
		"empty diff":           {Suite: "s", SuiteVersion: "1.0.0", Cases: []Case{{ID: "c", ExpectedCategories: []string{"x"}}}},
		"no expected category": {Suite: "s", SuiteVersion: "1.0.0", Cases: []Case{{ID: "c", Diff: "c.diff"}}},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			require.Error(t, m.Validate(), "%s must be rejected", name)
		})
	}
}

func TestValidate_RejectsDuplicateCaseID(t *testing.T) {
	m := Manifest{
		Suite: "s", SuiteVersion: "1.0.0",
		Cases: []Case{
			{ID: "dup", Diff: "a.diff", ExpectedCategories: []string{"x"}},
			{ID: "dup", Diff: "b.diff", ExpectedCategories: []string{"x"}},
		},
	}
	require.Error(t, m.Validate(), "duplicate case ids must be rejected")
}

func TestValidate_RejectsPathTraversalDiff(t *testing.T) {
	m := Manifest{
		Suite: "s", SuiteVersion: "1.0.0",
		Cases: []Case{{ID: "c", Diff: "../../../etc/passwd", ExpectedCategories: []string{"x"}}},
	}
	require.Error(t, m.Validate(), "a diff path escaping the suite dir must be rejected")
}

func TestValidate_RejectsDotDiffPath(t *testing.T) {
	m := Manifest{
		Suite: "s", SuiteVersion: "1.0.0",
		Cases: []Case{{ID: "c", Diff: ".", ExpectedCategories: []string{"x"}}},
	}
	require.Error(t, m.Validate(), "a diff path of '.' must be rejected (not a valid diff file)")
}

func TestValidate_Valid(t *testing.T) {
	m := Manifest{
		Suite: "s", SuiteVersion: "1.0.0",
		Cases: []Case{{ID: "c", Diff: "c.diff", ExpectedCategories: []string{"x"}}},
	}
	require.NoError(t, m.Validate())
}

func TestReproHashManifest_MatchesReproHash(t *testing.T) {
	// ReproHashManifest must produce the same hash as ReproHash when given the
	// same manifest — it's an optimization that skips the redundant Load.
	m, err := Load("testdata/suite-valid")
	require.NoError(t, err)
	h1, err := ReproHash("testdata/suite-valid")
	require.NoError(t, err)
	h2, err := ReproHashManifest(m, "testdata/suite-valid")
	require.NoError(t, err)
	assert.Equal(t, h1, h2, "ReproHashManifest must produce the same hash as ReproHash")
}

func TestReproHash_DeterministicAndContentSensitive(t *testing.T) {
	h1, err := ReproHash("testdata/suite-valid")
	require.NoError(t, err)
	h2, err := ReproHash("testdata/suite-valid")
	require.NoError(t, err)
	assert.Equal(t, h1, h2, "reproducibility hash must be deterministic for identical content")
	assert.NotEmpty(t, h1)

	// A copy with one diff byte changed must hash differently.
	dir := t.TempDir()
	copySuite(t, "testdata/suite-valid", dir)
	appendByte(t, filepath.Join(dir, "case-01.diff"))
	h3, err := ReproHash(dir)
	require.NoError(t, err)
	assert.NotEqual(t, h1, h3, "a changed diff must change the reproducibility hash")
}

func TestReproHash_RejectsOversizedDiff(t *testing.T) {
	// ReproHash must enforce a per-file size cap so a hostile suite cannot
	// exhaust memory via a multi-GB diff. The cap is an implementation detail;
	// what we assert is that a diff exceeding the cap is rejected, not silently
	// read into memory.
	dir := t.TempDir()
	// Use a cap-friendly manifest: one case whose diff is "big.diff".
	writeManifest(t, dir, `{"suite":"s","suite_version":"1.0.0","cases":[{"id":"c1","diff":"big.diff","expected_categories":["x"]}]}`)
	// Create a file that exceeds the per-file cap (we set cap via an internal
	// helper in the impl; here we just write a file > 0 bytes and trust that
	// the impl's cap is exercised). For this RED test we write a 1-byte file
	// and rely on a unit-level cap check via an exported constant.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.diff"), []byte("x"), 0o600))

	// The implementation should expose MaxDiffBytes. With a 0-byte cap, even a
	// 1-byte diff must be rejected.
	origCap := MaxDiffBytes
	MaxDiffBytes = 0
	defer func() { MaxDiffBytes = origCap }()

	_, err := ReproHash(dir)
	require.Error(t, err, "a diff exceeding MaxDiffBytes must be rejected")
}

func TestReproHash_IndependentOfCaseOrder(t *testing.T) {
	// Reordering cases in the manifest must NOT change the hash (content, not order,
	// defines reproducibility).
	base, err := ReproHash("testdata/suite-valid")
	require.NoError(t, err)

	dir := t.TempDir()
	copySuite(t, "testdata/suite-valid", dir)
	m, err := Load(dir)
	require.NoError(t, err)
	m.Cases[0], m.Cases[1] = m.Cases[1], m.Cases[0]
	writeManifestStruct(t, dir, m)

	reordered, err := ReproHash(dir)
	require.NoError(t, err)
	assert.Equal(t, base, reordered, "case order must not affect the reproducibility hash")
}

func TestBuildSubmission_TagsSuiteAndDistinctFromProduction(t *testing.T) {
	data, err := os.ReadFile("testdata/run-result.json")
	require.NoError(t, err)
	var rr RunResult
	require.NoError(t, json.Unmarshal(data, &rr))

	at := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	sub := BuildSubmission(rr, at)

	assert.Equal(t, "benchmark-suite", sub.Source, "source marks this as a suite submission, not production")
	assert.Equal(t, "fixture-mini", sub.Suite)
	assert.Equal(t, "1.0.0", sub.SuiteVersion)
	assert.Equal(t, version.Version, sub.AtcrVersion)
	assert.Equal(t, "2026-06-24T12:00:00Z", sub.SubmittedAt)
	require.Len(t, sub.Reviewers, 1)
	assert.Equal(t, "bruce", sub.Reviewers[0].Persona)

	// Distinct from production --export: the suite/source fields are present and
	// the envelope marshals them.
	out, err := json.Marshal(sub)
	require.NoError(t, err)
	s := string(out)
	for _, k := range []string{`"source"`, `"suite"`, `"suite_version"`} {
		assert.Contains(t, s, k, "benchmark submission must carry %s (distinct from production export)", k)
	}
}

func TestBuildSubmission_ReScrubsReviewerPII(t *testing.T) {
	// Defense-in-depth: rr.Reviewers come from an externally-supplied run-result
	// (atcr benchmark export consumes a hand-suppliable --in file), so a
	// non-conforming record carrying PII in its identity fields must be scrubbed
	// before it lands in a public submission — not passed through verbatim.
	rr := RunResult{
		Suite:        "fixture-mini",
		SuiteVersion: "1.0.0",
		GeneratedAt:  "2026-06-24T00:00:00Z",
		Reviewers: []scorecard.PublicRecord{{
			Persona: "bruce /Users/sam/secret.txt",
			Model:   "anthropic/claude-3 sam@example.com",
			Runs:    1,
		}},
	}
	at := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	sub := BuildSubmission(rr, at)

	require.Len(t, sub.Reviewers, 1)
	assert.Equal(t, "bruce", sub.Reviewers[0].Persona, "absolute-path PII must be scrubbed from persona")
	assert.Equal(t, "anthropic/claude-3", sub.Reviewers[0].Model, "email PII must be scrubbed from model")
}

// The suite envelope stamps the SHARED constant, and this pins both halves of that
// statement: the literal version, and the fact that it is sourced from
// scorecard.SubmissionSchema rather than a benchmark-local copy.
//
// The literal matters on its own. Asserting only `== scorecard.SubmissionSchema`
// would pass for any value the constant ever takes, so it cannot notice a bump — and
// a bump is exactly the event that needs a deliberate decision, because the constant
// is shared with the production leaderboard export. Pinning the number here forces
// the next bump to visit this test and, through it, this comment.
//
// This is a characterization test: it locks behavior the preceding tasks already
// produced. It earns its place by failing when the constant moves, not by having
// failed first.
func TestBuildSubmission_StampsSharedSubmissionSchema(t *testing.T) {
	at := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	sub := BuildSubmission(coverageRunResult(), at)

	assert.Equal(t, 2, sub.SubmissionSchema,
		"epic 35.16.6.2 bumped submission_schema to 2 — the version that added suite_case_ids/reviewer_coverage")
	assert.Equal(t, scorecard.SubmissionSchema, sub.SubmissionSchema,
		"the suite envelope must stamp the SHARED constant, never a benchmark-local copy")
}

// coverageRunResult is a measured run: a two-case suite where one reviewer row
// covered both cases and the other covered only one. The short row is the whole
// point — before submission_schema 2 it published indistinguishably from the full
// one. Outcomes and FallbackCases are populated precisely so the trimming assertions
// below have something real to prove is dropped.
func coverageRunResult() RunResult {
	return RunResult{
		Suite:        "fixture-mini",
		SuiteVersion: "1.0.0",
		GeneratedAt:  "2026-06-24T00:00:00Z",
		Reviewers: []scorecard.PublicRecord{
			{Persona: "brad", Model: "llm-large", Runs: 1},
			{Persona: "bruce", Model: "llm-small", Runs: 1},
		},
		SuiteCaseIDs: []string{"case-01", "case-02"},
		Coverage: []ReviewerCoverage{
			{
				Model:         "llm-large",
				Persona:       "brad",
				CaseIDs:       []string{"case-01", "case-02"},
				Outcomes:      map[string]int{"findings": 2},
				FallbackCases: 1,
			},
			{
				Model:         "llm-small",
				Persona:       "bruce",
				CaseIDs:       []string{"case-01"},
				Outcomes:      map[string]int{"findings": 1},
				FallbackCases: 0,
			},
		},
	}
}

// AC1: a measured run carries its denominator and every row's covered-case set into
// the submission envelope, so a board consumer can tell a short row from a full one
// without the run-result file. This is the hole submission_schema 2 exists to close.
func TestBuildSubmission_CarriesCoverage(t *testing.T) {
	at := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	sub := BuildSubmission(coverageRunResult(), at)

	assert.Equal(t, []string{"case-01", "case-02"}, sub.SuiteCaseIDs,
		"the suite denominator is carried, in manifest order")

	require.Len(t, sub.Coverage, 2, "one coverage row per reviewer row")
	assert.Equal(t, SubmissionCoverage{
		Model:   "llm-large",
		Persona: "brad",
		CaseIDs: []string{"case-01", "case-02"},
	}, sub.Coverage[0], "the fully-covered row carries both case ids")
	assert.Equal(t, SubmissionCoverage{
		Model:   "llm-small",
		Persona: "bruce",
		CaseIDs: []string{"case-01"},
	}, sub.Coverage[1], "the SHORT row is what the board could not previously see")

	data, err := json.Marshal(sub)
	require.NoError(t, err)
	s := string(data)
	assert.Contains(t, s, `"suite_case_ids"`)
	assert.Contains(t, s, `"reviewer_coverage"`)
	assert.Contains(t, s, `"case_ids"`)
}

// The submission row is a TRIMMED projection: the run-result's per-case outcome
// tally and fallback count are run-level diagnostics and must not ride along into a
// public, allowlist-based envelope. The fixture populates both, so their absence
// here is a real exclusion rather than an artifact of empty input.
func TestBuildSubmission_TrimsCoverageToCaseSet(t *testing.T) {
	at := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	data, err := json.Marshal(BuildSubmission(coverageRunResult(), at))
	require.NoError(t, err)
	s := string(data)

	assert.NotContains(t, s, `"outcomes"`,
		"the per-case outcome tally is a run-result diagnostic, not a public field")
	assert.NotContains(t, s, `"fallback_cases"`,
		"the fallback count is a run-result diagnostic, not a public field")
}

// A run-result written before coverage existed is UNMEASURED, not empty. Both keys
// must be ABSENT — not null, not [] — so a consumer reads "nobody measured" rather
// than "measured as zero cases", the same distinction RunResult and the export gate
// already turn on.
func TestBuildSubmission_OmitsUnmeasuredCoverage(t *testing.T) {
	rr := RunResult{
		Suite:        "fixture-mini",
		SuiteVersion: "1.0.0",
		GeneratedAt:  "2026-06-24T00:00:00Z",
		Reviewers:    []scorecard.PublicRecord{{Persona: "bruce", Model: "llm-small", Runs: 1}},
	}
	at := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	sub := BuildSubmission(rr, at)

	assert.Nil(t, sub.SuiteCaseIDs, "an unmeasured run carries no denominator")
	assert.Nil(t, sub.Coverage, "an unmeasured run carries no coverage rows")

	data, err := json.Marshal(sub)
	require.NoError(t, err)
	s := string(data)
	assert.NotContains(t, s, "suite_case_ids", "omitempty drops the key entirely — not null, not []")
	assert.NotContains(t, s, "reviewer_coverage")
}

// Coverage rows come from the same externally-supplied --in file as the reviewer
// rows, so they get the same defense-in-depth re-scrub. Scrubbing only one array
// would both leak PII and BREAK the documented (model, persona) join, because the
// two arrays would then spell the same identity differently.
func TestBuildSubmission_ReScrubsCoverageIdentities(t *testing.T) {
	rr := RunResult{
		Suite:        "fixture-mini",
		SuiteVersion: "1.0.0",
		GeneratedAt:  "2026-06-24T00:00:00Z",
		Reviewers: []scorecard.PublicRecord{{
			Persona: "bruce /Users/sam/secret.txt",
			Model:   "anthropic/claude-3 sam@example.com",
			Runs:    1,
		}},
		SuiteCaseIDs: []string{"case-01"},
		Coverage: []ReviewerCoverage{{
			Model:   "anthropic/claude-3 sam@example.com",
			Persona: "bruce /Users/sam/secret.txt",
			CaseIDs: []string{"case-01"},
		}},
	}
	at := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	sub := BuildSubmission(rr, at)

	require.Len(t, sub.Coverage, 1)
	assert.Equal(t, "bruce", sub.Coverage[0].Persona, "absolute-path PII must be scrubbed from a coverage persona")
	assert.Equal(t, "anthropic/claude-3", sub.Coverage[0].Model, "email PII must be scrubbed from a coverage model")

	require.Len(t, sub.Reviewers, 1)
	assert.Equal(t, sub.Reviewers[0].Persona, sub.Coverage[0].Persona,
		"both arrays must scrub identically or the documented join breaks")
	assert.Equal(t, sub.Reviewers[0].Model, sub.Coverage[0].Model)

	data, err := json.Marshal(sub)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "/Users/sam", "no path may reach a public submission")
	assert.NotContains(t, string(data), "sam@example.com", "no email may reach a public submission")
}

// Case ids are untrusted for the SAME reason the identity fields are: both come from
// the hand-suppliable --in file, and `benchmark export` is where that file first
// enters the tool. cli/benchmark_coverage.go already treats them as untrusted for
// terminal output; carrying them into a PUBLIC envelope owes them the same scrub the
// identity beside them already gets. Scrubbing one and not the other is the
// inconsistency, not the scrub.
//
// Both arrays are scrubbed with the same function, so the documented set comparison
// between suite_case_ids and a row's case_ids still lines up afterwards.
func TestBuildSubmission_ScrubsUntrustedCaseIDs(t *testing.T) {
	rr := RunResult{
		Suite:        "fixture-mini",
		SuiteVersion: "1.0.0",
		GeneratedAt:  "2026-06-24T00:00:00Z",
		Reviewers:    []scorecard.PublicRecord{{Persona: "bruce", Model: "llm-small", Runs: 1}},
		SuiteCaseIDs: []string{"case-01 /Users/sam/secret.txt", "case-02"},
		Coverage: []ReviewerCoverage{{
			Model:   "llm-small",
			Persona: "bruce",
			CaseIDs: []string{"case-01 /Users/sam/secret.txt"},
		}},
	}
	at := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	sub := BuildSubmission(rr, at)

	require.Len(t, sub.SuiteCaseIDs, 2)
	assert.Equal(t, "case-01", sub.SuiteCaseIDs[0], "path PII must be scrubbed from the denominator")
	assert.Equal(t, "case-02", sub.SuiteCaseIDs[1], "a clean id is passed through untouched")

	require.Len(t, sub.Coverage, 1)
	require.Len(t, sub.Coverage[0].CaseIDs, 1)
	assert.Equal(t, sub.SuiteCaseIDs[0], sub.Coverage[0].CaseIDs[0],
		"both arrays scrub identically, so the documented set comparison still lines up")

	data, err := json.Marshal(sub)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "/Users/sam", "no path may reach a public submission")
}

// Scrubbing must not mutate the caller's RunResult. `benchmark export` validates
// coverage against the run-result BEFORE building the submission, and a caller that
// builds twice, or inspects rr afterwards, must see the file it supplied — not a
// version this function quietly rewrote in place.
func TestBuildSubmission_DoesNotMutateSourceRunResult(t *testing.T) {
	rr := coverageRunResult()
	rr.SuiteCaseIDs[0] = "case-01 /Users/sam/secret.txt"
	rr.Coverage[0].CaseIDs[0] = "case-01 /Users/sam/secret.txt"
	rate, cost := 0.5, 0.01
	rr.Reviewers[0].SurvivedSkepticRate = &rate
	rr.Reviewers[0].CostPerCorroboratedFindingUSD = &cost

	at := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	sub := BuildSubmission(rr, at)

	assert.Equal(t, "case-01 /Users/sam/secret.txt", rr.SuiteCaseIDs[0],
		"the caller's denominator slice must be left alone")
	assert.Equal(t, "case-01 /Users/sam/secret.txt", rr.Coverage[0].CaseIDs[0],
		"the caller's per-row case list must be left alone")

	// The pointer metrics must be deep-copied too: a struct copy aliases them, so
	// mutating the submission would silently rewrite the caller's RunResult.
	*sub.Reviewers[0].SurvivedSkepticRate = 0.99
	*sub.Reviewers[0].CostPerCorroboratedFindingUSD = 9.99
	assert.Equal(t, 0.5, *rr.Reviewers[0].SurvivedSkepticRate,
		"the caller's survived-skeptic rate must not alias the submission's")
	assert.Equal(t, 0.01, *rr.Reviewers[0].CostPerCorroboratedFindingUSD,
		"the caller's cost metric must not alias the submission's")
}

// --- helpers ---

func writeManifest(t *testing.T, dir, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"), []byte(body), 0o600))
}

func writeManifestStruct(t *testing.T, dir string, m *Manifest) {
	t.Helper()
	b, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"), b, 0o600))
}

func copySuite(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	require.NoError(t, err)
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dst, e.Name()), b, 0o600))
	}
}

func appendByte(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("x")
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

// A coverage row that omits case_ids must still publish an ARRAY, never null.
//
// Reachable in practice: `--allow-partial-coverage` publishes a row that covered
// nothing, and a hand-supplied run-result can omit the key outright. At the ROW level
// nil and empty carry no distinct meaning — if reviewer_coverage is present at all
// then coverage WAS measured, so a row with no ids simply covered no cases. The
// unmeasured signal lives one level up, in the absence of the whole key.
//
// Emitting null there would hand a strict board decoder a type it did not ask for,
// on the exact field this schema bump exists to introduce — and board-side tolerance
// is an open coordination item, not something to spend on a value that means nothing.
func TestBuildSubmission_EmptyCoverageRowPublishesArrayNotNull(t *testing.T) {
	rr := RunResult{
		Suite:        "fixture-mini",
		SuiteVersion: "1.0.0",
		GeneratedAt:  "2026-06-24T00:00:00Z",
		Reviewers:    []scorecard.PublicRecord{{Persona: "bruce", Model: "llm-small", Runs: 1}},
		SuiteCaseIDs: []string{"case-01"},
		Coverage:     []ReviewerCoverage{{Model: "llm-small", Persona: "bruce"}}, // CaseIDs omitted -> nil
	}
	at := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	sub := BuildSubmission(rr, at)

	require.Len(t, sub.Coverage, 1)
	assert.NotNil(t, sub.Coverage[0].CaseIDs, "a row's case list is an array even when it is empty")
	assert.Empty(t, sub.Coverage[0].CaseIDs, "and it is empty, not fabricated")

	data, err := json.Marshal(sub)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"case_ids":[]`, "the wire form is [], never null")
	assert.NotContains(t, string(data), `"case_ids":null`)
}
