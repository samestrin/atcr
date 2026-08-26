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
// WHICH vocabulary is pinned, precisely: reconcile is a separate published module
// and this repository consumes the RELEASED version pinned in go.mod. CI builds with
// GOWORK=off (.github/workflows/ci.yml) deliberately, so reclib.Categories() here is
// the shipped vocabulary, not the in-tree reconcile/category.go. That is the right
// target for a doc that describes what atcr actually renders into prompts: the table
// can never claim a member users do not yet have. An in-tree addition is therefore
// caught at the moment the pin is bumped — and immediately, before the release, for
// any developer whose go.work builds against the in-tree module. The doc's own
// wording states this boundary rather than promising more.
//
// What is machine-checked is the Category column and its order. The Group cell is
// checked only for the two routing values below; the prose column is not checked at
// all — both are hand-maintained restatements of category.go's comments, so treat
// this guard as covering membership and order, not glosses.
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

// taxonomySectionLines returns the lines of the doc's taxonomy section, from the
// heading to the next heading of any level, with fenced blocks dropped. Both tests
// read that section rather than the whole document so no table, example, or appendix
// elsewhere in findings-format.md can satisfy or break a claim about this one.
func taxonomySectionLines(t *testing.T) []string {
	t.Helper()

	b, err := os.ReadFile(findingsFormatDoc)
	require.NoError(t, err)

	section := taxonomySection(strings.Split(string(b), "\n"))
	require.NotNil(t, section,
		"%s must carry a %q section — it is the taxonomy table's declared home", findingsFormatDoc, taxonomyHeading)
	return section
}

// taxonomySection is the pure half of taxonomySectionLines: it locates the
// taxonomy heading and collects the section's lines, dropping fenced blocks, so
// probe documents can be fed to it directly.
func taxonomySection(lines []string) []string {
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == taxonomyHeading {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return nil
	}

	var section []string
	fenced := false
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if strings.HasPrefix(trimmed, "#") || isSetextUnderline(trimmed) {
			break
		}
		section = append(section, line)
	}
	if section == nil {
		section = []string{}
	}
	return section
}

// isSetextUnderline reports whether a trimmed line is a setext heading underline
// (a run of only = or only -), which marks the preceding text as a heading —
// and therefore the end of the taxonomy section — without starting with #.
func isSetextUnderline(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	c := trimmed[0]
	if c != '=' && c != '-' {
		return false
	}
	for i := 1; i < len(trimmed); i++ {
		if trimmed[i] != c {
			return false
		}
	}
	return true
}

// taxonomyRowCells splits a taxonomy table row into its cells, reporting false for
// any line that is not such a row. A row is identified by a backticked first cell,
// which the header and separator rows do not have.
func taxonomyRowCells(line string) ([]string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return nil, false
	}
	cells := strings.Split(trimmed, "|")
	if len(cells) < 3 {
		return nil, false
	}
	first := strings.TrimSpace(cells[1])
	if len(first) < 3 || !strings.HasPrefix(first, "`") || !strings.HasSuffix(first, "`") {
		return nil, false
	}
	return cells, true
}

// A setext heading (text followed by a line of = or -) does not start with #, and
// a ~~~ fence is not a backtick fence — both are valid CommonMark that an ordinary
// future edit could introduce, and both must not let rows outside the taxonomy
// table leak into the section.
func TestTaxonomySection_SetextHeadingAndTildeFence(t *testing.T) {
	doc := []string{
		"## CATEGORY vocabulary",
		"",
		"| Category | Group | What it labels |",
		"|----------|-------|----------------|",
		"| `correctness` | Defect class | Wrong result. |",
		"",
		"~~~",
		"| `tildebogus` | Defect class | inside a tilde fence |",
		"~~~",
		"",
		"Next Section",
		"------------",
		"| `setextbogus` | Defect class | from a table under a setext heading |",
	}

	var rows []string
	for _, line := range taxonomySection(doc) {
		if cells, ok := taxonomyRowCells(line); ok {
			rows = append(rows, strings.Trim(strings.TrimSpace(cells[1]), "`"))
		}
	}
	assert.Equal(t, []string{"correctness"}, rows,
		"a setext heading must terminate the section and a ~~~ fence must hide its rows")
}

// Adding a category to the constant without adding its row — or removing one, or
// reordering the offer order the prompt renders — must fail here rather than leave
// the doc quietly describing a vocabulary that no longer exists.
func TestFindingsFormatDoc_TaxonomyTableTracksCategories(t *testing.T) {
	var got []string
	for _, line := range taxonomySectionLines(t) {
		if cells, ok := taxonomyRowCells(line); ok {
			got = append(got, strings.Trim(strings.TrimSpace(cells[1]), "`"))
		}
	}

	assert.Equal(t, reclib.Categories(), got,
		"the %q table in %s must list exactly the members reconcile.Categories() returns, in the same order — offer order is part of the rendered prompt, not a presentation choice. If the constant is the side that changed, update the table; if the go.mod pin has not been bumped yet, the table describes the released vocabulary and must wait for it",
		taxonomyHeading, findingsFormatDoc)
}

// `other` and `out-of-scope` are not defect classes: `other` is the escape hatch
// that keeps the set closed rather than lossy, and `out-of-scope` is a control token
// whose findings are annotated instead of promoted. A reader who meets them as
// ordinary categories will misuse both, so the table's Group cell must mark them as
// routing values.
func TestFindingsFormatDoc_TaxonomyTableMarksRoutingValues(t *testing.T) {
	lines := taxonomySectionLines(t)

	for _, routing := range []string{reclib.CategoryOutOfScope, reclib.CategoryOther} {
		found := false
		for _, line := range lines {
			cells, ok := taxonomyRowCells(line)
			if !ok || strings.Trim(strings.TrimSpace(cells[1]), "`") != routing {
				continue
			}
			found = true
			assert.Contains(t, cells[2], taxonomyRoutingMarker,
				"the Group cell of the %q row in %s must say %q — it routes a finding rather than describing a defect", routing, findingsFormatDoc, taxonomyRoutingMarker)
		}
		assert.True(t, found, "the %q table in %s must carry a row for the routing value %q", taxonomyHeading, findingsFormatDoc, routing)
	}
}
