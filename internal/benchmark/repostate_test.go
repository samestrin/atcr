package benchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeRepoStateCase lays down a conforming case directory and returns its path.
// Every field is a parameter of the fixture rather than a constant so a test can
// corrupt exactly one thing and leave the rest valid.
func writeRepoStateCase(t *testing.T, suiteDir, id, caseJSON string) string {
	t.Helper()
	dir := filepath.Join(suiteDir, id)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "base", "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "base", "pkg", "example.py"), []byte("x = 1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "commit-message.txt"), []byte("subject\n\nbody\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "change.diff"), []byte("diff --git a/pkg/example.py b/pkg/example.py\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "case.json"), []byte(caseJSON), 0o600))
	return dir
}

const validCaseJSON = `{
  "id": "good-case",
  "format": "repo-state-v1",
  "base_tree": "base",
  "commit_message": "commit-message.txt",
  "diff": "change.diff",
  "expected_findings": [
    {"id": "f-one", "file": "pkg/example.py", "line_start": 40, "line_end": 42,
     "line_tolerance": 5, "outside_diff": true, "category": "correctness",
     "summary": "A defect."},
    {"id": "f-two", "file": "pkg/example.py", "line_start": 9, "line_end": 9,
     "outside_diff": false, "category": "security", "summary": "Another defect."}
  ]
}`

func writeRepoStateSuite(t *testing.T, caseJSON string) string {
	t.Helper()
	dir := t.TempDir()
	writeRepoStateCase(t, dir, "good-case", caseJSON)
	writeManifest(t, dir, `{"suite":"repo-state-v1","suite_version":"1.0.0","cases":[{"id":"good-case","dir":"good-case"}]}`)
	return dir
}

func TestLoadRepoState_ValidSuite(t *testing.T) {
	m, err := LoadRepoState(writeRepoStateSuite(t, validCaseJSON))
	require.NoError(t, err)
	assert.Equal(t, FormatRepoStateV1, m.Suite)
	assert.Equal(t, "1.0.0", m.SuiteVersion)
	require.Len(t, m.Cases, 1)

	c := m.Cases[0]
	assert.Equal(t, "good-case", c.ID)
	assert.Equal(t, "base", c.BaseTree)
	assert.Equal(t, "commit-message.txt", c.CommitMessage)
	assert.Equal(t, "change.diff", c.Diff)
	assert.Equal(t, "good-case", filepath.Base(c.Dir), "Dir resolves to the case directory")
	require.Len(t, c.ExpectedFindings, 2)
	assert.Equal(t, "pkg/example.py", c.ExpectedFindings[0].File)
	assert.True(t, *c.ExpectedFindings[0].OutsideDiff)
	assert.False(t, *c.ExpectedFindings[1].OutsideDiff)
}

// line_tolerance is per-finding and data-driven: an explicit value is honored and
// an omitted one defaults to 3, so the matcher never hardcodes a window.
func TestExpectedFinding_ToleranceIsPerFindingWithDefault(t *testing.T) {
	m, err := LoadRepoState(writeRepoStateSuite(t, validCaseJSON))
	require.NoError(t, err)
	assert.Equal(t, 5, m.Cases[0].ExpectedFindings[0].Tolerance(), "explicit line_tolerance wins")
	assert.Equal(t, DefaultLineTolerance, m.Cases[0].ExpectedFindings[1].Tolerance(), "omitted line_tolerance defaults")
}

// A case whose base tree is missing must fail LOUDLY at load. Left to run time it
// would materialize an empty tree and score every reviewer zero, which reads as a
// hard benchmark rather than a broken case.
func TestLoadRepoState_MissingBaseTreeFailsAtLoad(t *testing.T) {
	dir := writeRepoStateSuite(t, validCaseJSON)
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "good-case", "base")))

	_, err := LoadRepoState(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "good-case")
	assert.Contains(t, err.Error(), "base")
}

func TestLoadRepoState_RejectsBadCases(t *testing.T) {
	tests := []struct {
		name     string
		caseJSON string
		wantErr  string
	}{
		{"wrong format literal",
			`{"id":"good-case","format":"repo-state-v9","base_tree":"base","commit_message":"commit-message.txt","diff":"change.diff","expected_findings":[{"id":"a","file":"pkg/example.py","line_start":1,"line_end":1,"outside_diff":false,"category":"correctness","summary":"s"}]}`,
			"format"},
		{"no expected findings",
			`{"id":"good-case","format":"repo-state-v1","base_tree":"base","commit_message":"commit-message.txt","diff":"change.diff","expected_findings":[]}`,
			"at least one expected_finding"},
		{"outside_diff omitted",
			`{"id":"good-case","format":"repo-state-v1","base_tree":"base","commit_message":"commit-message.txt","diff":"change.diff","expected_findings":[{"id":"a","file":"pkg/example.py","line_start":1,"line_end":1,"category":"correctness","summary":"s"}]}`,
			"outside_diff"},
		{"duplicate finding id",
			`{"id":"good-case","format":"repo-state-v1","base_tree":"base","commit_message":"commit-message.txt","diff":"change.diff","expected_findings":[{"id":"a","file":"pkg/example.py","line_start":1,"line_end":1,"outside_diff":false,"category":"correctness","summary":"s"},{"id":"a","file":"pkg/example.py","line_start":2,"line_end":2,"outside_diff":false,"category":"correctness","summary":"s"}]}`,
			"duplicate"},
		{"line_end before line_start",
			`{"id":"good-case","format":"repo-state-v1","base_tree":"base","commit_message":"commit-message.txt","diff":"change.diff","expected_findings":[{"id":"a","file":"pkg/example.py","line_start":9,"line_end":4,"outside_diff":false,"category":"correctness","summary":"s"}]}`,
			"line_end"},
		{"zero line_start",
			`{"id":"good-case","format":"repo-state-v1","base_tree":"base","commit_message":"commit-message.txt","diff":"change.diff","expected_findings":[{"id":"a","file":"pkg/example.py","line_start":0,"line_end":0,"outside_diff":false,"category":"correctness","summary":"s"}]}`,
			"line_start"},
		{"negative tolerance",
			`{"id":"good-case","format":"repo-state-v1","base_tree":"base","commit_message":"commit-message.txt","diff":"change.diff","expected_findings":[{"id":"a","file":"pkg/example.py","line_start":1,"line_end":1,"line_tolerance":-2,"outside_diff":false,"category":"correctness","summary":"s"}]}`,
			"line_tolerance"},
		{"case id disagrees with directory",
			`{"id":"other-case","format":"repo-state-v1","base_tree":"base","commit_message":"commit-message.txt","diff":"change.diff","expected_findings":[{"id":"a","file":"pkg/example.py","line_start":1,"line_end":1,"outside_diff":false,"category":"correctness","summary":"s"}]}`,
			"must equal"},
		{"empty summary",
			`{"id":"good-case","format":"repo-state-v1","base_tree":"base","commit_message":"commit-message.txt","diff":"change.diff","expected_findings":[{"id":"a","file":"pkg/example.py","line_start":1,"line_end":1,"outside_diff":false,"category":"correctness","summary":"  "}]}`,
			"summary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadRepoState(writeRepoStateSuite(t, tt.caseJSON))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// AC7: a case must not reference a path outside its own case directory. The guard
// covers every file-reference field, not just the one an author is most likely to
// get wrong.
func TestLoadRepoState_RejectsPathEscape(t *testing.T) {
	for _, field := range []string{"base_tree", "commit_message", "diff"} {
		t.Run(field, func(t *testing.T) {
			body := `{"id":"good-case","format":"repo-state-v1","base_tree":"base","commit_message":"commit-message.txt","diff":"change.diff","expected_findings":[{"id":"a","file":"pkg/example.py","line_start":1,"line_end":1,"outside_diff":false,"category":"correctness","summary":"s"}]}`
			var escaped string
			switch field {
			case "base_tree":
				escaped = `"base_tree":"../../etc"`
				body = replaceField(body, `"base_tree":"base"`, escaped)
			case "commit_message":
				escaped = `"commit_message":"/etc/passwd"`
				body = replaceField(body, `"commit_message":"commit-message.txt"`, escaped)
			case "diff":
				escaped = `"diff":"../escape.diff"`
				body = replaceField(body, `"diff":"change.diff"`, escaped)
			}
			_, err := LoadRepoState(writeRepoStateSuite(t, body))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "within the case directory")
		})
	}
}

// expected_findings[].file is the deliberate exception to the case-relative rule:
// it is repository-relative in the HEAD state. It still must not escape.
func TestLoadRepoState_RejectsEscapingFindingFile(t *testing.T) {
	body := `{"id":"good-case","format":"repo-state-v1","base_tree":"base","commit_message":"commit-message.txt","diff":"change.diff","expected_findings":[{"id":"a","file":"../../../etc/passwd","line_start":1,"line_end":1,"outside_diff":false,"category":"correctness","summary":"s"}]}`
	_, err := LoadRepoState(writeRepoStateSuite(t, body))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file")
}

// FORMAT.md: base/ must not contain a .git directory — the loader creates the
// repository, the case supplies only the working tree. A vendored .git would make
// materialization depend on whatever history the author happened to copy in.
func TestLoadRepoState_RejectsGitDirInBaseTree(t *testing.T) {
	dir := writeRepoStateSuite(t, validCaseJSON)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "good-case", "base", ".git"), 0o755))

	_, err := LoadRepoState(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".git")
}

// A repo-state case reaches CorroborationRate through the SAME category-recall
// scorer standard-v1 uses, so it needs the same guard standard-v1's Validate
// applies: a case expecting both a coarse category and a member of that category's
// equivalence family lets ONE raised finding satisfy BOTH and inflate recall.
//
// Without this, the guard held for standard-v1 and had a hole in the tier added
// beside it — and a repo-state case naturally carries one category per finding,
// so an author pairing "maintainability" with "style" across two findings would
// trip it without ever seeing a list of categories to check.
func TestLoadRepoState_RejectsRecallInflatingCategoryPairs(t *testing.T) {
	body := `{"id":"good-case","format":"repo-state-v1","base_tree":"base","commit_message":"commit-message.txt","diff":"change.diff","expected_findings":[
	  {"id":"a","file":"pkg/example.py","line_start":1,"line_end":1,"outside_diff":false,"category":"maintainability","summary":"s"},
	  {"id":"b","file":"pkg/example.py","line_start":40,"line_end":40,"outside_diff":false,"category":"style","summary":"s"}]}`

	_, err := LoadRepoState(writeRepoStateSuite(t, body))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "equivalence family")
}

// Two findings sharing ONE category is ordinary and must stay legal: a case may
// plant two correctness defects. Only the coarse-plus-family-member pairing
// inflates recall.
func TestLoadRepoState_AllowsTwoFindingsInTheSameCategory(t *testing.T) {
	body := `{"id":"good-case","format":"repo-state-v1","base_tree":"base","commit_message":"commit-message.txt","diff":"change.diff","expected_findings":[
	  {"id":"a","file":"pkg/example.py","line_start":1,"line_end":1,"outside_diff":false,"category":"correctness","summary":"s"},
	  {"id":"b","file":"pkg/example.py","line_start":40,"line_end":40,"outside_diff":true,"category":"correctness","summary":"s"}]}`

	_, err := LoadRepoState(writeRepoStateSuite(t, body))
	require.NoError(t, err)
}

// AC7 again, by the route the declared-path check cannot see. `base_tree` may say
// "base" — perfectly safe as a string — while `base` is a SYMLINK to somewhere
// else entirely. Stat follows it and reports a directory, so the case would
// materialize a tree from outside its own directory. The declared-path guard and
// the on-disk guard have to both hold, because an author controls both.
func TestLoadRepoState_RejectsSymlinkedBaseTree(t *testing.T) {
	dir := writeRepoStateSuite(t, validCaseJSON)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o600))

	base := filepath.Join(dir, "good-case", "base")
	require.NoError(t, os.RemoveAll(base))
	require.NoError(t, os.Symlink(outside, base))

	_, err := LoadRepoState(dir)
	require.Error(t, err, "a symlinked base tree escapes the case directory and must be refused")
	assert.Contains(t, err.Error(), "symlink")
}

// The same escape as the symlinked base tree, one level up: `dir` may say
// "good-case" — a perfectly safe string — while `good-case/` is a symlink out of
// the suite. Closing the hole for base_tree and leaving it open here would let a
// case escape by BEING a link rather than by containing one.
func TestLoadRepoState_RejectsSymlinkedCaseDirectory(t *testing.T) {
	dir := writeRepoStateSuite(t, validCaseJSON)
	outside := t.TempDir()
	writeRepoStateCase(t, outside, "good-case", validCaseJSON)

	real := filepath.Join(dir, "good-case")
	require.NoError(t, os.RemoveAll(real))
	require.NoError(t, os.Symlink(filepath.Join(outside, "good-case"), real))

	_, err := LoadRepoState(dir)
	require.Error(t, err, "a symlinked case directory escapes the suite and must be refused")
	assert.Contains(t, err.Error(), "symlink")
}

func TestLoadRepoState_RejectsSuiteLevelDefects(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		wantErr  string
	}{
		{"wrong suite discriminator",
			`{"suite":"standard-v1","suite_version":"1.0.0","cases":[{"id":"good-case","dir":"good-case"}]}`,
			"repo-state-v1"},
		{"no cases",
			`{"suite":"repo-state-v1","suite_version":"1.0.0","cases":[]}`,
			"at least one case"},
		{"blank suite_version",
			`{"suite":"repo-state-v1","suite_version":"  ","cases":[{"id":"good-case","dir":"good-case"}]}`,
			"suite_version"},
		{"manifest id disagrees with case.json id",
			`{"suite":"repo-state-v1","suite_version":"1.0.0","cases":[{"id":"mismatch","dir":"good-case"}]}`,
			"mismatch"},
		{"escaping case dir",
			`{"suite":"repo-state-v1","suite_version":"1.0.0","cases":[{"id":"good-case","dir":"../good-case"}]}`,
			"within the suite directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeRepoStateSuite(t, validCaseJSON)
			writeManifest(t, dir, tt.manifest)
			_, err := LoadRepoState(dir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// DetectSuiteFormat is the dispatcher seam: it reads only the discriminator, so
// the CLI can route a suite to the right loader without parsing it twice under the
// wrong shape.
func TestDetectSuiteFormat(t *testing.T) {
	assert.Equal(t, FormatRepoStateV1, mustDetect(t, writeRepoStateSuite(t, validCaseJSON)))
	assert.Equal(t, "fixture-mini", mustDetect(t, "testdata/suite-valid"))

	_, err := DetectSuiteFormat(t.TempDir())
	require.Error(t, err, "a directory with no suite.json has no format to detect")
}

// The SHIPPED suite is the thing this epic exists to make runnable. A fixture-only
// test would stay green while benchmarks/repo-state-v1/ rotted.
func TestLoadRepoState_ShippedSuiteLoads(t *testing.T) {
	m, err := LoadRepoState("../../benchmarks/repo-state-v1")
	require.NoError(t, err, "the bundled repo-state-v1 suite must load")
	assert.Equal(t, FormatRepoStateV1, m.Suite)
	require.NotEmpty(t, m.Cases)
	for _, c := range m.Cases {
		assert.NotEmpty(t, c.ExpectedFindings, "case %q plants nothing", c.ID)
	}
}

// AC2: standard-v1 behavior is untouched. Load still parses the shipped suite, and
// the repo-state discriminator still refuses to come back through the standard-v1
// loader — that loader cannot express a repo-state case, so returning one is not an
// option. What changed is the SUITE being loadable at all, via LoadRepoState.
func TestLoad_StandardV1Unaffected(t *testing.T) {
	m, err := Load("../../benchmarks/standard-v1")
	require.NoError(t, err)
	assert.Equal(t, "standard-v1", m.Suite)

	_, err = Load(writeRepoStateSuite(t, validCaseJSON))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LoadRepoState",
		"the error must name the loader that DOES handle this suite, not a document")
}

// replaceField swaps one JSON field in a fixture body so a test corrupts exactly
// one thing and leaves the rest conforming.
func replaceField(body, old, new string) string {
	return strings.Replace(body, old, new, 1)
}

func mustDetect(t *testing.T, dir string) string {
	t.Helper()
	got, err := DetectSuiteFormat(dir)
	require.NoError(t, err)
	return got
}

// LoadRepoState's own doc promises "every filesystem precondition is checked at
// load". A diff ParseDiffLineMap rejects — a body line outside any hunk, an
// unparseable header, a negative range — used to escape that contract: the parse
// happened per-case inside the run loop, AFTER every earlier case's panel had
// been paid for, so one malformed diff aborted the run and discarded the
// completed cases. The loader must reject it at load, where the remedy is free.
func TestLoadRepoState_RejectsAMalformedDiff(t *testing.T) {
	dir := writeRepoStateSuite(t, validCaseJSON)
	malformed := "diff --git a/pkg/example.py b/pkg/example.py\n" +
		"--- a/pkg/example.py\n+++ b/pkg/example.py\n" +
		"@@ -1,2 +1,2 @@\n-x = 1\n+ctx\nbody line outside any hunk\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "good-case", "change.diff"), []byte(malformed), 0o600))

	_, err := LoadRepoState(dir)
	require.Error(t, err, "a diff ParseDiffLineMap rejects must be a LOAD error, not a mid-run abort")
	assert.Contains(t, err.Error(), "good-case", "the error must name the broken case")
}

// The case-directory symlink guard must not fail OPEN: a non-nil Lstat error used
// to silently SKIP the AC7 check — the one place a discarded syscall error
// disabled a security control rather than merely losing a diagnostic. A case dir
// that cannot even be Lstat'd (locked parent, dangling mount) must fail the load
// with the reason named, the way checkFiles returns its Lstat errors.
func TestLoadRepoState_LstatFailureOnCaseDirIsNotSwallowed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not constrain root, so the Lstat cannot be made to fail")
	}
	dir := writeRepoStateSuite(t, validCaseJSON)
	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.MkdirAll(filepath.Join(locked, "good-case"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(locked, "good-case", "case.json"), []byte(validCaseJSON), 0o600))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	writeManifest(t, dir, `{"suite":"repo-state-v1","suite_version":"1.0.0","cases":[{"id":"good-case","dir":"locked/good-case"}]}`)
	_, err := LoadRepoState(dir)
	require.Error(t, err, "a case dir whose Lstat fails must error, not silently skip the symlink check")
	assert.Contains(t, err.Error(), "checking case directory",
		"the error must come from the directory check itself, not from a downstream read whose failure would be mistaken for a missing case.json")
}
