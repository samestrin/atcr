package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

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

// TestIgnore_RoundTripSuppressesOnNextReconcile is the end-to-end success
// criterion: reconcile a fixture review, `atcr ignore` the finding by the id
// stamped into findings.json, and the next RunReconcile drops it into
// Result.Suppressed while the other finding stays.
func TestIgnore_RoundTripSuppressesOnNextReconcile(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		out, err := fixtureGit(root, args...).CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	git("init", "-q")
	git("config", "user.email", "t@t.t")
	git("config", "user.name", "t")
	files := map[string]string{
		"internal/mem/pool.go": "package mem\n\nfunc Get() { _ = unsafe.Pointer(nil) }\n",
		"internal/store/db.go": "package store\n\nfunc Open() {}\n",
	}
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(body), 0o644))
	}
	git("add", ".")
	git("commit", "-q", "-m", "init")

	reviewDir := filepath.Join(fanout.ReviewsRoot(root), "r1")
	src := filepath.Join(reviewDir, "sources", "greta", "findings.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("# atcr-findings/v1\n"+
		"HIGH|internal/mem/pool.go:3|`Get` uses unsafe.Pointer|drop it|security|10|ev|greta\n"+
		"LOW|internal/store/db.go:3|`Open` is slow|cache it|perf|5|ev|greta\n"), 0o644))
	require.NoError(t, fanout.WriteLatest(root, "r1"))

	reconcileOnce := func() reconcile.Result {
		t.Helper()
		res, err := reconcile.RunReconcile(context.Background(), reviewDir, nil, reconcile.Options{
			ReconciledAt: time.Unix(1700000000, 0).UTC(),
			Root:         root,
		})
		require.NoError(t, err)
		return res
	}

	first := reconcileOnce()
	require.Empty(t, first.Suppressed)
	js, err := reconcile.ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	var target string
	for _, f := range js {
		if f.File == "internal/mem/pool.go" {
			target = f.ID
		}
	}
	require.NotEmpty(t, target, "findings.json carries the pool.go finding's stamped id")

	t.Chdir(root)
	out, err := runIgnore(t, target, "--reason", "this package needs unsafe for the mmap view")
	require.NoError(t, err, out)

	second := reconcileOnce()
	require.Len(t, second.Suppressed, 1, "the ignored finding is suppressed")
	assert.Equal(t, "internal/mem/pool.go", second.Suppressed[0].File)
	require.Len(t, second.Findings, 1, "the other finding stays")
	assert.Equal(t, "internal/store/db.go", second.Findings[0].File)
	js, err = reconcile.ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, js, 1, "findings.json omits the ignored finding")
	assert.Equal(t, "internal/store/db.go", js[0].File)
}
