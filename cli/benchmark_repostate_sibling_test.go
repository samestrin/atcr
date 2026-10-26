package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSiblingPool writes a pipe-row pool under reviewDir/sources/pool, one row per
// entry, each attributed to greta.
func writeSiblingPool(t *testing.T, reviewDir string, rows ...string) {
	t.Helper()
	pool := filepath.Join(reviewDir, "sources", "pool")
	require.NoError(t, os.MkdirAll(pool, 0o755))
	content := "# atcr-findings/v1\n" + strings.Join(rows, "\n") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(pool, "findings.txt"), []byte(content), 0o600))
}

// A finding filed on the edited line but declaring "sibling: <file>:<line>" in its
// EVIDENCE is scored at the sibling. The pool is read through ParseSource, which
// never stamps SiblingLocation, so the reader must stamp it itself; without that the
// finding scores at the edited line, which is exactly what this test refuses.
func TestReadCaseFindingsLocated_MarkedFindingScoresAtTheSibling(t *testing.T) {
	dir := t.TempDir()
	writeSiblingPool(t, dir,
		"HIGH|projects/rename.py:16|transfer lacks the archived guard|f|correctness|15|the guard added here; sibling: projects/transfer.py:7|greta")

	located, categorical, _, _, err := readCaseFindingsLocated(dir, map[string]bool{"greta": true})
	require.NoError(t, err)
	require.Len(t, located["greta"], 1)
	assert.Equal(t, benchmark.ReportedFinding{File: "projects/transfer.py", Line: 7, Category: "correctness"},
		located["greta"][0], "the located projection carries the sibling, with the finding's own category")
	assert.Equal(t, []string{"correctness"}, categorical["greta"], "the category list is untouched by the marker")

	expected := []benchmark.ExpectedFinding{
		{ID: "at-sibling", File: "projects/transfer.py", LineStart: 6, LineEnd: 7, Category: "correctness", OutsideDiff: new(bool)},
		{ID: "at-edited-line", File: "projects/rename.py", LineStart: 16, LineEnd: 16, Category: "correctness", OutsideDiff: new(bool)},
	}
	matches := benchmark.MatchFindings(expected, located["greta"], benchmark.DiffLineMap{})
	require.Len(t, matches, 2)
	assert.True(t, matches[0].Matched, "the marked finding settles the expectation at the sibling location")
	assert.False(t, matches[1].Matched, "the marked finding no longer settles the edited line it is filed on")
}

// A finding without the marker — or with one ParseSiblingLocation rejects — is
// located at its own FILE:LINE, exactly as before.
func TestReadCaseFindingsLocated_UnmarkedFindingScoresAsToday(t *testing.T) {
	dir := t.TempDir()
	writeSiblingPool(t, dir,
		"HIGH|projects/rename.py:16|p|f|correctness|15|no marker here|greta",
		"MEDIUM|projects/rename.py:17|p|f|security|15|sibling: projects/transfer.py|greta",
		"LOW|projects/rename.py:18|p|f|style|15|a nonsibling: projects/transfer.py:7 word|greta")

	located, _, _, _, err := readCaseFindingsLocated(dir, map[string]bool{"greta": true})
	require.NoError(t, err)
	assert.Equal(t, []benchmark.ReportedFinding{
		{File: "projects/rename.py", Line: 16, Category: "correctness"},
		{File: "projects/rename.py", Line: 17, Category: "security"},
		{File: "projects/rename.py", Line: 18, Category: "style"},
	}, located["greta"], "no well-formed marker: each finding stays at its anchored FILE:LINE")

	expected := []benchmark.ExpectedFinding{
		{ID: "edited", File: "projects/rename.py", LineStart: 16, LineEnd: 16, Category: "correctness", OutsideDiff: new(bool)},
		{ID: "sibling", File: "projects/transfer.py", LineStart: 7, LineEnd: 7, Category: "correctness", OutsideDiff: new(bool)},
	}
	matches := benchmark.MatchFindings(expected, located["greta"], benchmark.DiffLineMap{})
	assert.True(t, matches[0].Matched)
	assert.False(t, matches[1].Matched, "a malformed or embedded marker is not read as a sibling")
}

// Every shipped repo-state-v1 case, scored with unmarked findings, scores exactly as
// the pre-marker projection {File, Line, Category} would. For each expected finding
// the pool carries a hit at its range, a near miss just past its tolerance, and a
// row on every head-side line of every hunk of the case's own diff, so the comparison covers matches, misses
// and outside_diff refusals on real case data.
func TestReadCaseFindingsLocated_UnmarkedFindingsScoreIdenticallyOnEveryShippedCase(t *testing.T) {
	cases := shippedRepoStateCases(t)
	require.Len(t, cases, 8, "the shipped suite's case count moved: re-check which cases are sibling-arm-shaped")
	for id, c := range cases {
		t.Run(id, func(t *testing.T) {
			lm, err := loadCaseDiffLineMap(c)
			require.NoError(t, err)

			var rows []string
			for _, e := range c.ExpectedFindings {
				for _, line := range []int{e.LineStart, e.LineEnd, e.LineEnd + e.Tolerance() + 1} {
					rows = append(rows, fmt.Sprintf("HIGH|%s:%d|p|f|%s|15|evidence without a marker|greta", e.File, line, e.Category))
				}
			}
			for _, fl := range diffHeadLines(t, c) {
				rows = append(rows, fmt.Sprintf("MEDIUM|%s|p|f|correctness|15|on the edited hunk|greta", fl))
			}
			dir := t.TempDir()
			writeSiblingPool(t, dir, rows...)

			located, _, _, _, err := readCaseFindingsLocated(dir, map[string]bool{"greta": true})
			require.NoError(t, err)

			parsed, err := stream.ReadPoolFindings(filepath.Join(dir, "sources", "pool"))
			require.NoError(t, err)
			var baseline []benchmark.ReportedFinding
			for _, f := range parsed.Findings {
				baseline = append(baseline, benchmark.ReportedFinding{File: f.File, Line: f.Line, Category: f.Category})
			}
			require.Len(t, baseline, len(rows))
			assert.Equal(t, baseline, located["greta"], "unmarked findings are projected exactly as before")
			assert.Equal(t,
				benchmark.MatchFindings(c.ExpectedFindings, baseline, lm),
				benchmark.MatchFindings(c.ExpectedFindings, located["greta"], lm),
				"unmarked findings score identically")
		})
	}
}

// diffHeadLines returns "file:line" for every head-side line a hunk of c's diff
// spans, read from the hunk headers alone.
func diffHeadLines(t *testing.T, c benchmark.RepoStateCase) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(c.Dir, c.Diff))
	require.NoError(t, err)
	var out []string
	file := ""
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(l, "+++ b/") {
			file = strings.TrimPrefix(l, "+++ b/")
			continue
		}
		if !strings.HasPrefix(l, "@@ ") {
			continue
		}
		var bs, bn, hs, hn int
		_, err := fmt.Sscanf(l, "@@ -%d,%d +%d,%d @@", &bs, &bn, &hs, &hn)
		require.NoError(t, err, "hunk header %q", l)
		for i := 0; i < hn; i++ {
			out = append(out, fmt.Sprintf("%s:%d", file, hs+i))
		}
	}
	require.NotEmpty(t, out)
	return out
}
