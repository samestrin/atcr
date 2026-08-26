package reconcile

import (
	"os"
	"strings"
	"testing"

	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The taxonomy table in docs/findings-format.md is the only place a reader learns
// the actual member list without opening Go source, and nothing in markdown can
// couple it to the constant it documents. The drift this test exists to prevent is
// the one that already happened once: the doc described CATEGORY as a free-text tag
// long after reconcile had closed the vocabulary, and no test noticed. So the doc's
// table is pinned against reclib.Categories() itself — never against a second
// literal list here, which would be a third copy free to drift from both.
//
// It lives in internal/reconcile rather than in the published reconcile module for
// the same reason justification_record_boundary_test.go does: that module is
// embedded by external tools and must not assume this repository's file layout, and
// docs/ holds no Go package of its own.
const (
	findingsFormatDoc     = "../../docs/findings-format.md"
	taxonomyHeading       = "## CATEGORY vocabulary"
	taxonomyRoutingMarker = "Routing value"
)

// taxonomyTableCategories returns the category words listed in the doc's taxonomy
// table, in the order the table lists them. The first cell of each row is the
// category, spelled in backticks; the header and separator rows carry none and are
// skipped by that same rule.
func taxonomyTableCategories(t *testing.T, doc string) []string {
	t.Helper()

	lines := strings.Split(doc, "\n")

	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == taxonomyHeading {
			start = i + 1
			break
		}
	}
	require.NotEqual(t, -1, start,
		"docs/findings-format.md must carry a %q section — it is the table's declared home", taxonomyHeading)

	var got []string
	for _, line := range lines[start:] {
		if strings.HasPrefix(line, "## ") {
			break
		}
		word, ok := taxonomyRowCategory(line)
		if ok {
			got = append(got, word)
		}
	}
	return got
}

// taxonomyRowCategory extracts the backticked category from a table row's first
// cell, reporting false for any line that is not such a row.
func taxonomyRowCategory(line string) (string, bool) {
	if !strings.HasPrefix(strings.TrimSpace(line), "|") {
		return "", false
	}
	cells := strings.Split(strings.TrimSpace(line), "|")
	if len(cells) < 2 {
		return "", false
	}
	first := strings.TrimSpace(cells[1])
	if len(first) < 3 || !strings.HasPrefix(first, "`") || !strings.HasSuffix(first, "`") {
		return "", false
	}
	return strings.Trim(first, "`"), true
}

// Adding a category to the constant without adding its row — or removing one, or
// reordering the offer order the prompt renders — must fail here rather than leave
// the doc quietly describing a vocabulary that no longer exists.
func TestFindingsFormatDoc_TaxonomyTableTracksCategories(t *testing.T) {
	b, err := os.ReadFile(findingsFormatDoc)
	require.NoError(t, err)
	doc := string(b)

	assert.Equal(t, reclib.Categories(), taxonomyTableCategories(t, doc),
		"the taxonomy table must list exactly the members reconcile.Categories() returns, in the same order — offer order is part of the rendered prompt, not a presentation choice")
}

// `other` and `out-of-scope` are not defect classes: `other` is the escape hatch
// that keeps the set closed rather than lossy, and `out-of-scope` is a control token
// whose findings are annotated instead of promoted. A reader who meets them as
// ordinary categories will misuse both, so the table must mark them as routing
// values.
func TestFindingsFormatDoc_TaxonomyTableMarksRoutingValues(t *testing.T) {
	b, err := os.ReadFile(findingsFormatDoc)
	require.NoError(t, err)
	lines := strings.Split(string(b), "\n")

	for _, routing := range []string{reclib.CategoryOutOfScope, reclib.CategoryOther} {
		found := false
		for _, line := range lines {
			word, ok := taxonomyRowCategory(line)
			if ok && word == routing {
				found = true
				assert.Contains(t, line, taxonomyRoutingMarker,
					"the %q row must be marked %q — it routes a finding rather than describing a defect", routing, taxonomyRoutingMarker)
			}
		}
		assert.True(t, found, "the taxonomy table must carry a row for the routing value %q", routing)
	}
}
