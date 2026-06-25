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
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			require.Error(t, m.Validate(), "%s must be rejected", name)
		})
	}
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
