package fanout

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// xlsxBody is a stand-in for an Office Open XML workbook of n bytes: a ZIP local
// file header (NUL bytes in its first fields) followed by filler. Sized past the
// unknown test model's per-bin budget (~71680 bytes) it is the shape that, before
// Epic 35.16.14 fix F3, was planned as a bin of its own and overflowed the window.
func xlsxBody(n int) string {
	head := "PK\x03\x04\x14\x00\x06\x00\x08\x00\x00\x00!\x00[Content_Types].xml"
	return head + strings.Repeat("\x00\x7f", (n-len(head))/2)
}

// plannedBinFiles collects every file any planned baseline bin names: each slot's
// primary and fallback chunk files, the shipped entries, and the bins a ceiling
// deferred.
func plannedBinFiles(slots []Slot) map[string]bool {
	out := map[string]bool{}
	for _, s := range slots {
		for _, f := range s.Primary.chunkFiles {
			out[f] = true
		}
		for _, fb := range s.Fallbacks {
			for _, f := range fb.chunkFiles {
				out[f] = true
			}
		}
		for _, e := range s.entries {
			out[e.Path] = true
		}
		for _, bin := range s.deferredBins {
			for _, f := range bin {
				out[f] = true
			}
		}
	}
	return out
}

// Epic 35.16.14 fix F3 (AC1): a binary .xlsx-like file in a baseline (--all) scan
// is skipped before sizing. It must not appear in any planned bin, nor in the
// written files payload, and preparing the run must not fail on it, while the text
// files around it are still planned.
func TestPrepareReviewFromRepo_BinaryFileInNoBin(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "review")
	repo := baselineRepo(t, map[string]string{
		"one.go":              "package one\n// FILE:one\n" + strings.Repeat("// line\n", 6_000),
		"two.go":              "package two\n// FILE:two\n" + strings.Repeat("// line\n", 6_000),
		"data/rates.xlsx":     xlsxBody(200_000),
		"data/tariff-q3.xlsx": xlsxBody(1_000),
	})

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, out))
	require.NoError(t, err, "a binary file must not fail the run")
	require.Greater(t, len(prep.Slots), 2, "the text files still fan out across bins")

	files := plannedBinFiles(prep.Slots)
	assert.True(t, files["one.go"], "text file planned")
	assert.True(t, files["two.go"], "text file planned")
	assert.False(t, files["data/rates.xlsx"], "an oversize binary must not become a bin")
	assert.False(t, files["data/tariff-q3.xlsx"], "a small binary must not ride in a text bin")

	payloadText, err := os.ReadFile(filepath.Join(out, "payload", "files.txt"))
	require.NoError(t, err)
	assert.NotContains(t, string(payloadText), "rates.xlsx")
	assert.NotContains(t, string(payloadText), "tariff-q3.xlsx")
}

// Epic 35.16.14 fix F3: a scan where every candidate is binary has nothing to
// review. It names that cause instead of calling the repository empty, and
// scaffolds nothing.
func TestPrepareReviewFromRepo_AllBinaryNoReviewableContent(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "review")
	repo := baselineRepo(t, map[string]string{
		"a.xlsx": xlsxBody(1_000),
		"b.xlsx": xlsxBody(1_000),
	})

	_, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, out))
	require.ErrorIs(t, err, ErrNoReviewableContent)
	assert.Contains(t, err.Error(), "all 2 tracked file(s) in scope are non-text")
	assert.NoDirExists(t, out)
}
