package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/history"
	"github.com/samestrin/atcr/internal/reconcile"
)

// ignoreFixtureFinding is the reconciled finding the ignore tests suppress. Its
// Problem carries the "(symbol) " anchor and regex metacharacters, so a match
// that was not regex-quoted would either fail to compile or miss the finding.
func ignoreFixtureFinding() reconcile.JSONFinding {
	f := reconcile.JSONFinding{
		Severity: "HIGH",
		File:     "internal/mem/pool.go",
		Line:     42,
		Problem:  "(Pool.Get) avoid unsafe.Pointer (*T)(p) here [cast]",
		Category: "safety",
	}
	f.ID = history.FindingID(f.File, f.Line, f.Problem)
	return f
}

// writeIgnoreFixtureReview makes root/.atcr/reviews/<id>/reconciled/findings.json
// holding findings, points .atcr/latest at it, and returns the review dir.
func writeIgnoreFixtureReview(t *testing.T, root, id string, findings ...reconcile.JSONFinding) string {
	t.Helper()
	dir := filepath.Join(fanout.ReviewsRoot(root), id)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
	data, err := json.Marshal(findings)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reconciled", reconcile.FindingsJSON), data, 0o644))
	require.NoError(t, fanout.WriteLatest(root, id))
	return dir
}

func runIgnore(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newIgnoreCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func ignorePath() string { return filepath.Join(".atcr", reconcile.IgnoreFileName) }

func TestIgnore_AppendsRuleDerivedFromLatestFinding(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	f := ignoreFixtureFinding()
	other := reconcile.JSONFinding{Severity: "LOW", File: "a.go", Line: 1, Problem: "(a) other", Category: "style"}
	other.ID = history.FindingID(other.File, other.Line, other.Problem)
	writeIgnoreFixtureReview(t, ".", "r1", other, f)

	out, err := runIgnore(t, f.ID, "--reason", "this package requires unsafe")
	require.NoError(t, err, out)
	assert.Contains(t, out, f.ID)

	rules, err := reconcile.ReadIgnoreFile(ignorePath())
	require.NoError(t, err)
	require.Len(t, rules, 1)
	r := rules[0]
	assert.Equal(t, f.File, r.File)
	assert.Equal(t, f.Category, r.Category)
	assert.Equal(t, "this package requires unsafe", r.Reason)
	assert.Empty(t, r.Package, "a file-scoped rule needs no package")
	re := regexp.MustCompile(r.Match)
	assert.True(t, re.MatchString(f.Problem), "match must hit the finding's own Problem")
	assert.False(t, re.MatchString("(Pool.Put) avoid unsafe.Pointer (*T)(p) here [cast]"),
		"match must be quoted literally: a different symbol anchor does not match")
	assert.False(t, re.MatchString("(Pool.Get) avoid unsafeXPointer (*T)(p) here [cast]"),
		"metacharacters are escaped, not live")
}

func TestIgnore_AppendsToExistingFileKeepingRules(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	f := ignoreFixtureFinding()
	writeIgnoreFixtureReview(t, ".", "r1", f)
	existing := "# team conventions\nrules:\n  - file: gen/*.go\n    reason: generated\n"
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(ignorePath(), []byte(existing), 0o644))

	out, err := runIgnore(t, f.ID, "--reason", "intentional")
	require.NoError(t, err, out)

	data, err := os.ReadFile(ignorePath())
	require.NoError(t, err)
	assert.Contains(t, string(data), "# team conventions", "hand-written comments survive")
	rules, err := reconcile.ParseIgnore(data)
	require.NoError(t, err)
	require.Len(t, rules, 2)
	assert.Equal(t, "gen/*.go", rules[0].File)
	assert.Equal(t, f.File, rules[1].File)
}

func TestIgnore_ReviewFlagSelectsAnotherReview(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	f := ignoreFixtureFinding()
	writeIgnoreFixtureReview(t, ".", "old", f)
	writeIgnoreFixtureReview(t, ".", "new") // latest holds no findings

	_, err := runIgnore(t, f.ID, "--reason", "x")
	require.Error(t, err, "the id is not in the latest review")

	out, err := runIgnore(t, f.ID, "--reason", "x", "--review", "old")
	require.NoError(t, err, out)
	rules, err := reconcile.ReadIgnoreFile(ignorePath())
	require.NoError(t, err)
	require.Len(t, rules, 1)
}

func TestIgnore_AlreadyIgnoredIsNotDuplicated(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	f := ignoreFixtureFinding()
	writeIgnoreFixtureReview(t, ".", "r1", f)

	_, err := runIgnore(t, f.ID, "--reason", "first")
	require.NoError(t, err)
	before, err := os.ReadFile(ignorePath())
	require.NoError(t, err)

	out, err := runIgnore(t, f.ID, "--reason", "second")
	require.NoError(t, err, out)
	assert.Contains(t, out, "already")
	after, err := os.ReadFile(ignorePath())
	require.NoError(t, err)
	assert.Equal(t, before, after, "a second ignore of the same finding leaves the file unchanged")
}

func TestIgnore_RefusesWithoutTouchingFile(t *testing.T) {
	f := ignoreFixtureFinding()
	seed := "rules:\n  - file: gen/*.go\n    reason: generated\n"
	cases := []struct {
		name string
		args []string
	}{
		{"unknown id", []string{"0000000000000000", "--reason", "x"}},
		{"missing reason", []string{f.ID}},
		{"blank reason", []string{f.ID, "--reason", "  "}},
		{"blank id", []string{" ", "--reason", "x"}},
		{"no id", []string{"--reason", "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			writeIgnoreFixtureReview(t, ".", "r1", f)
			require.NoError(t, os.WriteFile(ignorePath(), []byte(seed), 0o644))

			_, err := runIgnore(t, tc.args...)
			require.Error(t, err)
			assert.NotZero(t, exitCode(err), "must exit non-zero")
			data, rerr := os.ReadFile(ignorePath())
			require.NoError(t, rerr)
			assert.Equal(t, seed, string(data), "the ignore file is untouched")
		})
	}
}

func TestIgnore_UnknownIdCreatesNoFile(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeIgnoreFixtureReview(t, ".", "r1", ignoreFixtureFinding())

	_, err := runIgnore(t, "0000000000000000", "--reason", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "0000000000000000")
	_, serr := os.Stat(ignorePath())
	assert.True(t, os.IsNotExist(serr), "no ignore file is created for an unknown id")
}

func TestIgnore_NoReconciledFindings(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	require.NoError(t, os.MkdirAll(filepath.Join(fanout.ReviewsRoot("."), "r1"), 0o755))
	require.NoError(t, fanout.WriteLatest(".", "r1"))

	_, err := runIgnore(t, "abc", "--reason", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "atcr reconcile")
}

func TestIgnore_RegisteredAndListedInHelp(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})
	require.NoError(t, root.Execute())
	assert.Regexp(t, `(?m)^\s+ignore\s+\S`, out.String(), "atcr --help lists ignore")
}
