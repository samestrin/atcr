package fanout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The drift guard is only as good as its parser: a citation it misreads is one
// it either false-alarms on or silently waves through. These cases pin the
// grammar in TestCitationDrift_CitedLineHoldsTheNamedIdentifier's doc comment
// against a synthetic tree, so the guard's verdict on the real tree can be
// trusted in both directions.
func TestCitationDrift_ParserGrammar(t *testing.T) {
	root := t.TempDir()
	target := strings.Join([]string{
		"package fanout",
		"",
		"func keepSmallestEntry() {}",
		"",
		"func budget() {}",
	}, "\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal/fanout"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal/fanout/review.go"), []byte(target), 0o644))
	const citing = "internal/payload/x.go"
	targets := []string{"review.go"}

	for _, tc := range []struct {
		name    string
		src     string
		cites   int
		problem string // substring of the single expected problem; "" means none
	}{
		{"ident before parens", "// keepSmallestEntry (internal/fanout/review.go:3)", 1, ""},
		{"ident before a range", "// keepSmallestEntry (internal/fanout/review.go:3-4)", 1, ""},
		{"ident inside parens", "// the reroute (keepSmallestEntry, internal/fanout/review.go:3)", 1, ""},
		{"markdown backticks", "the reroute to `keepSmallestEntry` (`internal/fanout/review.go:3`).", 1, ""},
		{"backticked lower-case ident", "// the `budget` (internal/fanout/review.go:5) helper", 1, ""},
		{"ident wrapped onto the previous line", "//   then keepSmallestEntry\n//   (internal/fanout/review.go:3) returns", 1, ""},
		{"in-parens form wrapped after the comma", "//   (keepSmallestEntry,\n//   internal/fanout/review.go:3), so", 1, ""},
		{"two citations on one line", "// keepSmallestEntry (internal/fanout/review.go:3) and `budget` (internal/fanout/review.go:5)", 2, ""},
		{"a longer file name is not a citation", "// see preview.go:3 and xreview.go:9", 0, ""},

		{"stale line names the site", "// keepSmallestEntry (internal/fanout/review.go:4)", 1, "internal/payload/x.go:1: citation internal/fanout/review.go:4 names keepSmallestEntry, but internal/fanout/review.go:4 does not hold it"},
		{"a range checks its FIRST line", "// keepSmallestEntry (internal/fanout/review.go:2-4)", 1, "does not hold it"},
		{"a prefix of the identifier is stale", "// keepSmallest (internal/fanout/review.go:3)", 1, "does not hold it"},
		{"a suffix of the identifier is stale", "// SmallestEntry (internal/fanout/review.go:3)", 1, "does not hold it"},
		{"no identifier", "// see the reroute (internal/fanout/review.go:3)", 1, "unparseable"},
		{"no parentheses", "// See internal/fanout/review.go:3.", 1, "unparseable"},
		{"TD row form", "// (TD internal/fanout/review.go:3)", 1, "unparseable"},
		{"emphasised prose word", "// actually POPULATES (internal/fanout/review.go:3)", 1, "unparseable"},
		{"capitalised prose word", "// See (internal/fanout/review.go:3)", 1, "unparseable"},
		{"unclosed parenthesis", "// keepSmallestEntry (internal/fanout/review.go:3 and on", 1, "unparseable"},
		{"bare name resolves against the citing dir", "// keepSmallestEntry (review.go:3)", 1, "internal/payload/review.go does not exist"},
		{"line past end of file", "// keepSmallestEntry (internal/fanout/review.go:99)", 1, "range is invalid"},
		{"reversed range", "// keepSmallestEntry (internal/fanout/review.go:3-2)", 1, "range is invalid"},
		{"line zero", "// keepSmallestEntry (internal/fanout/review.go:0)", 1, "range is invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, problems := checkCitations(root, citing, tc.src, targets)
			assert.Equal(t, tc.cites, n, "citations found")
			if tc.problem == "" {
				assert.Empty(t, problems)
				return
			}
			require.Len(t, problems, 1)
			assert.Contains(t, problems[0], tc.problem)
		})
	}
}

// The epic's mutation check in miniature: a one-line insertion above the cited
// function must turn a passing citation into a failure that names the site.
func TestCitationDrift_LineInsertionAboveTargetFails(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "review.go")
	const src = "// keepSmallestEntry (review.go:2-3)"
	require.NoError(t, os.WriteFile(path, []byte("package fanout\nfunc keepSmallestEntry() {\n}\n"), 0o644))
	_, problems := checkCitations(root, "chunker.go", src, []string{"review.go"})
	require.Empty(t, problems)

	require.NoError(t, os.WriteFile(path, []byte("package fanout\n// inserted\nfunc keepSmallestEntry() {\n}\n"), 0o644))
	_, problems = checkCitations(root, "chunker.go", src, []string{"review.go"})
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "chunker.go:1: citation review.go:2-3 names keepSmallestEntry")
}

// judgeCitations holds the two checks that keep the guard from going inert:
// awaitingRepin can only shrink, and a pattern that stops matching anything
// fails. Both decide nothing on the real tree today, so they are pinned here.
func TestCitationDrift_JudgeKeepsTheGuardArmed(t *testing.T) {
	stale := []string{"a.go:1: citation review.go:3 names X, but review.go:3 does not hold it"}
	for _, tc := range []struct {
		name     string
		results  []citationResult
		pending  map[string]string
		errs     []string // substrings, one per expected error, in order
		logCount int
	}{
		{"unlisted file with a problem fails", []citationResult{{"a.go", 1, stale}}, nil,
			[]string{"does not hold it"}, 0},
		{"unlisted clean file passes", []citationResult{{"a.go", 1, nil}}, nil, nil, 0},
		{"pending file's problems are logged, not failed", []citationResult{{"a.go", 1, stale}},
			map[string]string{"a.go": "T2"}, nil, 1},
		{"pending file that passes must leave the list", []citationResult{{"a.go", 1, nil}},
			map[string]string{"a.go": "T2"}, []string{"a.go now passes this guard: remove it from awaitingRepin (T2 re-pinned it)"}, 0},
		{"no citations anywhere fails", []citationResult{{"a.go", 0, nil}, {"b.go", 0, nil}}, nil,
			[]string{"no citations found in any in-scope file"}, 0},
		{"empty scope fails", nil, nil, []string{"no citations found in any in-scope file"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs, logs := judgeCitations(tc.results, tc.pending)
			require.Len(t, errs, len(tc.errs), "errors: %q", errs)
			for i, want := range tc.errs {
				assert.Contains(t, errs[i], want)
			}
			assert.Len(t, logs, tc.logCount)
		})
	}
}
