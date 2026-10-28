package fanout

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// treeFiles lists every file under root, relative and slash-separated.
func treeFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	}))
	sort.Strings(out)
	return out
}

// T1 success criterion: two screenshots, one named with "../", land as
// safe-named files under visual/ and nothing is written outside it.
func TestPersistVisualEvidence_WritesSafeNamesUnderVisualOnly(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "review")
	require.NoError(t, os.Mkdir(dir, 0o755))
	res := BrowserResult{
		Screenshots: []BrowserScreenshot{
			{Name: "Home Page", Path: "test-results/home.png", Data: []byte("home")},
			{Name: "../../escape", Path: "../x.png", Data: []byte("evil")},
		},
		Console:         []ConsoleEntry{{Level: "error", Text: "TypeError: x is undefined", Location: "app.js:3"}},
		NetworkFailures: []NetworkFailure{{Method: "GET", URL: "http://localhost:3000/api", Status: 500}},
	}

	got, err := persistVisualEvidence(secretGuardWarn, dir, res)
	require.NoError(t, err)

	assert.Equal(t, []string{"visual/home-page.png", "visual/escape.png"}, got.Screenshots)
	assert.Equal(t, "visual/console.txt", got.Console)
	assert.Equal(t, "visual/network.txt", got.Network)
	assert.Equal(t, []string{"review/visual/console.txt", "review/visual/escape.png", "review/visual/home-page.png", "review/visual/network.txt"},
		treeFiles(t, parent), "nothing is written outside visual/")

	for _, p := range append(got.Screenshots, got.Console, got.Network) {
		assert.Regexp(t, `^visual/[a-z0-9._-]+$`, p)
		assert.NotContains(t, p, "..")
	}
	b, err := os.ReadFile(filepath.Join(dir, "visual", "escape.png"))
	require.NoError(t, err)
	assert.Equal(t, "evil", string(b))
	b, err = os.ReadFile(filepath.Join(dir, "visual", "console.txt"))
	require.NoError(t, err)
	assert.Equal(t, "- [error] TypeError: x is undefined (app.js:3)\n", string(b))
	b, err = os.ReadFile(filepath.Join(dir, "visual", "network.txt"))
	require.NoError(t, err)
	assert.Equal(t, "- GET http://localhost:3000/api -> HTTP 500\n", string(b))
}

// T1 success criterion: a result with no screenshots writes no visual/.
func TestPersistVisualEvidence_NoScreenshotsWritesNothing(t *testing.T) {
	dir := t.TempDir()
	for name, res := range map[string]BrowserResult{
		"empty":   {},
		"no data": {Screenshots: []BrowserScreenshot{{Name: "home", Path: "home.png"}}, Console: []ConsoleEntry{{Level: "error", Text: "boom"}}},
	} {
		got, err := persistVisualEvidence(secretGuardWarn, dir, res)
		require.NoError(t, err, name)
		assert.Equal(t, VisualArtifacts{}, got, name)
		_, statErr := os.Stat(filepath.Join(dir, visualEvidenceDir))
		assert.True(t, os.IsNotExist(statErr), "%s: no visual/ directory", name)
	}
}

// No review dir (a stage run outside a review) persists nothing.
func TestPersistVisualEvidence_NoReviewDirIsANoOp(t *testing.T) {
	got, err := persistVisualEvidence(secretGuardWarn, "", BrowserResult{Screenshots: []BrowserScreenshot{{Name: "a", Data: []byte("x")}}})
	require.NoError(t, err)
	assert.Equal(t, VisualArtifacts{}, got)
}

// T1 success criterion: a console line holding a token is redacted under
// redact and blocks the write under block.
func TestPersistVisualEvidence_GuardsCapturedText(t *testing.T) {
	res := BrowserResult{
		Screenshots:     []BrowserScreenshot{{Name: "home", Path: "home.png", Data: []byte("png")}},
		Console:         []ConsoleEntry{{Level: "log", Text: "token=" + guardAWSKey}},
		NetworkFailures: []NetworkFailure{{Method: "GET", URL: "http://localhost/?key=" + guardAWSKey, Error: "net::ERR_FAILED"}},
	}

	t.Run("redact", func(t *testing.T) {
		dir := t.TempDir()
		got, err := persistVisualEvidence(secretGuardRedact, dir, res)
		require.NoError(t, err)
		for _, p := range []string{got.Console, got.Network} {
			b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
			require.NoError(t, err)
			assert.NotContains(t, string(b), guardAWSKey)
			assert.Contains(t, string(b), payload.SecretPlaceholder)
		}
	})

	t.Run("block", func(t *testing.T) {
		dir := t.TempDir()
		_, err := persistVisualEvidence(secretGuardBlock, dir, res)
		require.ErrorIs(t, err, ErrSecretGuardBlocked)
		_, statErr := os.Stat(filepath.Join(dir, visualEvidenceDir))
		assert.True(t, os.IsNotExist(statErr), "nothing is written when the text is blocked")
	})
}

// A visual/ that is not a plain directory (a symlink planted in the review
// dir) is refused rather than followed.
func TestPersistVisualEvidence_RefusesASymlinkedVisualDir(t *testing.T) {
	dir, elsewhere := t.TempDir(), t.TempDir()
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(dir, visualEvidenceDir)))
	_, err := persistVisualEvidence(secretGuardWarn, dir, BrowserResult{Screenshots: []BrowserScreenshot{{Name: "a", Data: []byte("x")}}})
	require.Error(t, err)
	assert.Empty(t, treeFiles(t, elsewhere))
}

func TestVisualEvidenceName(t *testing.T) {
	used := map[string]bool{"console.txt": true, "network.txt": true}
	cases := []struct {
		sc   BrowserScreenshot
		want string
	}{
		{BrowserScreenshot{Name: "home", Path: "test-results/home.png"}, "home.png"},
		{BrowserScreenshot{Name: "home", Path: "other/home.png"}, "home-2.png"},
		{BrowserScreenshot{Name: "../../etc/passwd"}, "etc-passwd.png"},
		{BrowserScreenshot{Name: "....", Path: "a.jpeg"}, "screenshot.jpeg"},
		{BrowserScreenshot{Name: "shot.JPG"}, "shot.jpg"},
		{BrowserScreenshot{Name: "a..b", Path: "x.webp"}, "a.b.webp"},
		{BrowserScreenshot{Name: "console", Path: "c.txt"}, "console.png"},
		{BrowserScreenshot{Name: strings.Repeat("x", 200)}, strings.Repeat("x", visualEvidenceNameMax-len(".png")) + ".png"},
		{BrowserScreenshot{Name: "ünïcode/é"}, "n-code.png"},
	}
	for _, c := range cases {
		got := visualEvidenceName(c.sc, used)
		assert.Equal(t, c.want, got, c.sc.Name)
		assert.Regexp(t, `^[a-z0-9][a-z0-9._-]*$`, got)
		assert.NotContains(t, got, "..")
		assert.LessOrEqual(t, len(got), visualEvidenceNameMax)
	}
}

// T1: runVisualStage hands the stage the review's directory.
func TestRunVisualStage_SetsTheReviewDir(t *testing.T) {
	f := &fakeVisual{log: &callLog{}, reply: "NO FINDINGS"}
	p := &PreparedReview{ID: "r", Dir: "/repo/.atcr/reviews/r", visual: f}
	runVisualStage(context.Background(), p)
	assert.Equal(t, "/repo/.atcr/reviews/r", f.in.ReviewDir)
}

// T1: the stage persists the browser's evidence under the review dir and
// returns the relative paths with its result.
func TestVisualStage_PersistsTheBrowserEvidence(t *testing.T) {
	stage, _, _, _, _ := newOrderStage(t)
	reviewDir := t.TempDir()

	res, err := stage.RunVisual(context.Background(), VisualStageInput{RepoDir: t.TempDir(), RunID: "r1", ReviewDir: reviewDir})
	require.NoError(t, err)
	assert.Equal(t, []string{"visual/home.png"}, res.Artifacts.Screenshots)
	assert.Equal(t, []string{"visual/home.png"}, treeFiles(t, reviewDir))
}
