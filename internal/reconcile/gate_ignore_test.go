package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ignoreReview writes a two-finding review (a HIGH on internal/auth/session.go
// and a LOW on internal/store/db.go) into a fresh review dir and returns it
// with a git repo holding both files.
func ignoreReview(t *testing.T) (root, reviewDir string) {
	t.Helper()
	root = gitRepoWithFiles(t, "internal/auth/session.go", "internal/store/db.go")
	reviewDir = t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/auth/session.go:3|`RefreshToken` uses unsafe.Pointer|drop it|security|10|ev|greta\n"+
			"LOW|internal/store/db.go:7|`Open` is slow|cache it|perf|5|ev|greta\n")
	return root, reviewDir
}

// writeIgnoreYAML writes body to root/.atcr/ignore.yaml.
func writeIgnoreYAML(t *testing.T, root, body string) string {
	t.Helper()
	p := filepath.Join(root, ".atcr", IgnoreFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

func runIgnoreReconcile(t *testing.T, root, reviewDir string) (Result, error) {
	t.Helper()
	withFakeTier4(t, &fakeTier4{})
	return RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
}

// TestRunReconcile_IgnoreRuleSuppressesFinding is the T3 success criterion: a
// matching rule moves the finding out of Findings and JSONFindings and into
// Suppressed, and the gate (CountAtOrAbove over Findings, as cli/reconcile.go
// gateFindings calls it) no longer sees it.
func TestRunReconcile_IgnoreRuleSuppressesFinding(t *testing.T) {
	root, reviewDir := ignoreReview(t)
	writeIgnoreYAML(t, root, "rules:\n"+
		"  - file: internal/auth/session.go\n"+
		"    category: security\n"+
		"    match: unsafe\\.Pointer\n"+
		"    reason: this package needs unsafe for the mmap view\n")

	res, err := runIgnoreReconcile(t, root, reviewDir)
	require.NoError(t, err)

	require.Len(t, res.Suppressed, 1)
	assert.Equal(t, "internal/auth/session.go", res.Suppressed[0].File)
	assert.Equal(t, "HIGH", res.Suppressed[0].Severity)
	assert.NotEmpty(t, res.Suppressed[0].ID, "suppressed records carry their stamped wire id")

	require.Len(t, res.Findings, 1)
	assert.Equal(t, "internal/store/db.go", res.Findings[0].File)
	jf := res.JSONFindings()
	require.Len(t, jf, 1)
	assert.Equal(t, "internal/store/db.go", jf[0].File)

	assert.Equal(t, 1, res.Summary.TotalFindings)
	assert.Zero(t, CountAtOrAbove(res.Findings, SevHigh, false),
		"a suppressed HIGH must not fail the gate")

	js, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, js, 1, "findings.json omits the suppressed finding")
	assert.Equal(t, "internal/store/db.go", js[0].File)
}

// TestRunReconcile_IgnoreKeepsFindingsInLockstep guards the index alignment
// between Findings and JSONFindings() that internal/mcp's failingFindings
// walks, when the suppressed finding sits in the middle of the set.
func TestRunReconcile_IgnoreKeepsFindingsInLockstep(t *testing.T) {
	root := gitRepoWithFiles(t, "a/a.go", "b/b.go", "c/c.go")
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"CRITICAL|a/a.go:1|`A` leaks|fix|security|5|ev|greta\n"+
			"HIGH|b/b.go:1|`B` leaks|fix|security|5|ev|greta\n"+
			"LOW|c/c.go:1|`C` is slow|fix|perf|5|ev|greta\n")
	writeIgnoreYAML(t, root, "rules:\n  - package: b\n    reason: generated code\n")

	res, err := runIgnoreReconcile(t, root, reviewDir)
	require.NoError(t, err)

	require.Len(t, res.Suppressed, 1)
	assert.Equal(t, "b/b.go", res.Suppressed[0].File)
	jsons := res.JSONFindings()
	require.Len(t, res.Findings, len(jsons))
	for i := range res.Findings {
		assert.Equal(t, res.Findings[i].File, jsons[i].File, "index %d drifted", i)
		assert.Equal(t, res.Findings[i].Severity, jsons[i].Severity, "index %d drifted", i)
	}
}

// TestRunReconcile_IgnoreRecountsOutOfScope pins the OutOfScope recount: the
// library counts it over the pre-suppression set, so suppressing an
// out-of-scope finding must drop it.
func TestRunReconcile_IgnoreRecountsOutOfScope(t *testing.T) {
	root := gitRepoWithFiles(t, "a/a.go", "b/b.go")
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"LOW|a/a.go:1|`A` predates this change|ignore it|out-of-scope|5|ev|greta\n"+
			"LOW|b/b.go:1|`B` predates this change|ignore it|out-of-scope|5|ev|greta\n")
	writeIgnoreYAML(t, root, "rules:\n  - file: a/a.go\n    reason: known\n")

	res, err := runIgnoreReconcile(t, root, reviewDir)
	require.NoError(t, err)

	require.Len(t, res.Suppressed, 1)
	assert.Equal(t, 1, res.Summary.OutOfScope, "out_of_scope describes the post-suppression set")
	assert.Equal(t, 1, res.Summary.TotalFindings)
}

// TestRunReconcile_MalformedIgnoreIsHardError: a rule file that does not parse
// or validate fails the reconcile with an error naming the file, and nothing
// is emitted.
func TestRunReconcile_MalformedIgnoreIsHardError(t *testing.T) {
	for name, body := range map[string]string{
		"bad yaml":       "rules: [\n",
		"no reason":      "rules:\n  - file: a.go\n",
		"unknown key":    "rules:\n  - file: a.go\n    line: 3\n    reason: x\n",
		"bad glob":       "rules:\n  - file: \"a[.go\"\n    reason: x\n",
		"escaping scope": "rules:\n  - file: ../x.go\n    reason: x\n",
	} {
		t.Run(name, func(t *testing.T) {
			root, reviewDir := ignoreReview(t)
			p := writeIgnoreYAML(t, root, body)

			_, err := runIgnoreReconcile(t, root, reviewDir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), p)

			_, statErr := os.Stat(filepath.Join(reviewDir, "reconciled", FindingsJSON))
			assert.True(t, os.IsNotExist(statErr), "a rejected rule file emits nothing")
		})
	}
}

// TestRunReconcile_NoIgnoreFileIsByteIdentical: with no .atcr/ignore.yaml,
// with an empty rule list, and with a rule that matches nothing, the run
// suppresses nothing and every reconciled artifact is byte-for-byte the same.
func TestRunReconcile_NoIgnoreFileIsByteIdentical(t *testing.T) {
	run := func(ignore string) map[string]string {
		root, reviewDir := ignoreReview(t)
		if ignore != "" {
			writeIgnoreYAML(t, root, ignore)
		}
		res, err := runIgnoreReconcile(t, root, reviewDir)
		require.NoError(t, err)
		assert.Nil(t, res.Suppressed)
		assert.Len(t, res.Findings, 2)
		return readTree(t, filepath.Join(reviewDir, "reconciled"))
	}

	base := run("")
	require.NotEmpty(t, base)
	for name, body := range map[string]string{
		"empty rules":  "rules: []\n",
		"no match":     "rules:\n  - file: elsewhere/x.go\n    reason: unrelated\n",
		"comment only": "# nothing yet\n",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, base, run(body))
		})
	}
}

// TestRunReconcile_NoRootSkipsIgnore: with no checkout root there is no
// .atcr/ to read, so the rule file is never consulted.
func TestRunReconcile_NoRootSkipsIgnore(t *testing.T) {
	_, reviewDir := ignoreReview(t)
	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
	})
	require.NoError(t, err)
	assert.Nil(t, res.Suppressed)
	assert.Len(t, res.Findings, 2)
}

// readTree returns every regular file under dir, keyed by its relative path.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[rel] = string(b)
		return nil
	}))
	return out
}
