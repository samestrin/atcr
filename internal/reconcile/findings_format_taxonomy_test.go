package reconcile

import (
	"os"
	"slices"
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
// table is pinned against the vocabulary the reconcile module itself declares —
// never against a second literal list here, which would be a third copy free to
// drift from both.
//
// WHICH vocabulary is pinned, precisely: the IN-TREE reconcile/category.go, read
// from source by inTreeCategories rather than through reclib.Categories(). That call
// is ambient — reconcile is a separate published module, so it resolves to the
// in-tree module under a local go.work and to the go.mod pin under GOWORK=off, which
// is what CI sets (.github/workflows/ci.yml). Pinning the table against it made this
// guard unsatisfiable during the release window: adding a member in-tree demanded a
// 33-row table locally and a 32-row table in CI, and no table content satisfied both,
// so the developer had either a red local suite or a red CI until the module was
// tagged and the pin bumped. Reading the source by path resolves identically in both
// environments, which also means the PR that adds the member fails its OWN CI run
// instead of the next unrelated PR that happens to bump the pin.
//
// The released pin is still checked, but against the in-tree list and never against
// the doc — see TestReconcileModule_ReleasedVocabularyMatchesInTree below, which
// reports a pending release rather than failing the table.
//
// What is machine-checked is the Category column, its order, and the PARTITION the
// Group column describes: which categories share a Group cell and where the
// boundaries fall are pinned against the comment-marked blocks of category.go's
// const declaration. The Group cell's WORDING is not pinned, and the "What it
// labels" prose is not checked at all — pinning either would mean exporting the
// glosses from a module external tools embed. Treat this guard as covering
// membership, order, and grouping structure, not the glosses themselves.
//
// It lives in internal/reconcile rather than in the published reconcile module for
// the same reason justification_record_boundary_test.go does: that module is
// embedded by external tools and must not assume this repository's file layout, and
// docs/ holds no Go package of its own.
const (
	findingsFormatDoc     = "../../docs/findings-format.md"
	taxonomyHeading       = "## CATEGORY vocabulary"
	taxonomyRoutingMarker = "Routing value"
	inTreeReconcileDir    = "../../reconcile"
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
	searchFenced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			searchFenced = !searchFenced
			continue
		}
		if !searchFenced && trimmed == taxonomyHeading {
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
		if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") || isSetextUnderline(trimmed) {
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
// which the header and separator rows do not have. The normalized category name
// (backticks stripped) is returned alongside the cells so no caller re-derives it.
func taxonomyRowCells(line string) (cells []string, name string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return nil, "", false
	}
	cells = strings.Split(trimmed, "|")
	if len(cells) < 3 {
		return nil, "", false
	}
	first := strings.TrimSpace(cells[1])
	if len(first) < 3 || !strings.HasPrefix(first, "`") || !strings.HasSuffix(first, "`") {
		return nil, "", false
	}
	return cells, strings.Trim(first, "`"), true
}

// taxonomyRow is one parsed taxonomy table row: its cells and the normalized
// category name from the first cell.
type taxonomyRow struct {
	cells []string
	name  string
}

// taxonomyTableRows returns the parsed rows of the taxonomy table within the
// section: collection starts at the table's header line and stops at the first
// blank line after it, so a second table under the same heading can neither
// contribute rows nor truncate the section.
func taxonomyTableRows(section []string) []taxonomyRow {
	var rows []taxonomyRow
	inTable := false
	for _, line := range section {
		trimmed := strings.TrimSpace(line)
		if !inTable {
			if isTaxonomyTableHeader(trimmed) {
				inTable = true
			}
			continue
		}
		if trimmed == "" {
			break
		}
		if cells, name, ok := taxonomyRowCells(line); ok {
			rows = append(rows, taxonomyRow{cells: cells, name: name})
		}
	}
	return rows
}

// isTaxonomyTableHeader reports whether a trimmed line is the taxonomy table's
// header row (its first cell is "Category").
func isTaxonomyTableHeader(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	cells := strings.Split(trimmed, "|")
	if len(cells) < 3 {
		return false
	}
	return strings.TrimSpace(cells[1]) == "Category"
}

// A second table under the same heading (a categoryMerges alias legend is the
// likely future addition) must not contribute rows to the taxonomy assertion,
// and a #-prefixed prose line ("#1 note:") must not truncate the section.
func TestTaxonomyTableRows_SecondTableAndProseLineIgnored(t *testing.T) {
	section := []string{
		"intro prose",
		"#1 note: a prose line that must not truncate anything",
		"| Category | Group | What it labels |",
		"|----------|-------|----------------|",
		"| `correctness` | Defect class | Wrong result. |",
		"",
		"| Alias | Canonical |",
		"|-------|-----------|",
		"| `legendbogus` | `correctness` |",
	}

	var names []string
	for _, row := range taxonomyTableRows(section) {
		names = append(names, row.name)
	}
	assert.Equal(t, []string{"correctness"}, names,
		"row parsing must be anchored to the taxonomy table's header, not to any pipe line in the section")
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
		if _, name, ok := taxonomyRowCells(line); ok {
			rows = append(rows, name)
		}
	}
	assert.Equal(t, []string{"correctness"}, rows,
		"a setext heading must terminate the section and a ~~~ fence must hide its rows")
}

// A fenced example that quotes the taxonomy heading — findings-format.md is a
// document ABOUT a text format and dense with fenced examples — must not be
// mistaken for the section itself: the guard would then parse the quoted copy
// and could pass while the real table drifted.
func TestTaxonomySection_FencedHeadingIsNotTheSection(t *testing.T) {
	doc := []string{
		"```",
		"## CATEGORY vocabulary",
		"| `fencedbogus` | Defect class | a quoted example, not the real table |",
		"```",
		"",
		"## CATEGORY vocabulary",
		"",
		"| Category | Group | What it labels |",
		"|----------|-------|----------------|",
		"| `correctness` | Defect class | Wrong result. |",
	}

	var rows []string
	for _, line := range taxonomySection(doc) {
		if _, name, ok := taxonomyRowCells(line); ok {
			rows = append(rows, name)
		}
	}
	assert.Equal(t, []string{"correctness"}, rows,
		"a heading inside a fenced code block must not be taken for the taxonomy section")
}

// A ### sub-heading inside the taxonomy section (a "Notes" block is the likely
// future addition) must not truncate extraction — the table after it is still
// part of the section. Only a same-or-higher-level heading ends it.
func TestTaxonomySection_SubHeadingDoesNotTruncate(t *testing.T) {
	doc := []string{
		"## CATEGORY vocabulary",
		"",
		"| Category | Group | What it labels |",
		"|----------|-------|----------------|",
		"| `correctness` | Defect class | Wrong result. |",
		"### Notes",
		"| `aftersub` | Defect class | row after a sub-heading |",
		"",
		"## Next Section",
		"| `nextsec` | Defect class | from the next section |",
	}

	var rows []string
	for _, line := range taxonomySection(doc) {
		if _, name, ok := taxonomyRowCells(line); ok {
			rows = append(rows, name)
		}
	}
	assert.Equal(t, []string{"correctness", "aftersub"}, rows,
		"a ### sub-heading must not truncate the taxonomy section")
}

// The row-detection contract has no negative pin: a mutation that loosens what
// counts as a row (a dropped backtick check, a widened prefix) would turn green
// every assertion that depends on it. These malformed lines must all be rejected.
func TestTaxonomyRowCells_RejectsMalformedLines(t *testing.T) {
	for _, line := range []string{
		"| no-backticks | Defect class | a plain first cell is not a category |",
		"| `unclosed | Defect class | missing closing backtick |",
		"not a table line at all",
	} {
		_, _, ok := taxonomyRowCells(line)
		assert.False(t, ok, "taxonomyRowCells must reject %q", line)
	}
}

// Adding a category to the constant without adding its row — or removing one, or
// reordering the offer order the prompt renders — must fail here rather than leave
// the doc quietly describing a vocabulary that no longer exists.
func TestFindingsFormatDoc_TaxonomyTableTracksCategories(t *testing.T) {
	var got []string
	for _, row := range taxonomyTableRows(taxonomySectionLines(t)) {
		got = append(got, row.name)
	}

	assert.Equal(t, inTreeVocabulary(t), got,
		"the %q table in %s must list exactly the members %s declares, in the same order — offer order is part of the rendered prompt, not a presentation choice. If the constant is the side that changed, update the table",
		taxonomyHeading, findingsFormatDoc, inTreeReconcileDir)
}

// inTreeVocabulary reads the vocabulary the reconcile module declares in THIS
// tree. Resolving it by path rather than through reclib keeps the doc guard's
// answer identical under go.work and under GOWORK=off.
func inTreeVocabulary(t *testing.T) []string {
	t.Helper()

	got, err := inTreeCategories(inTreeReconcileDir)
	require.NoError(t, err, "the taxonomy guard cannot run without the in-tree vocabulary at %s", inTreeReconcileDir)
	require.NotEmpty(t, got, "the in-tree vocabulary must not be empty")
	return got
}

// The wiring is only worth anything if the path resolves to the real module, so
// pin two members the vocabulary cannot lose: the routing values, which the
// table below asserts against independently.
func TestInTreeVocabulary_ResolvesTheRealReconcileModule(t *testing.T) {
	got := inTreeVocabulary(t)

	assert.Contains(t, got, reclib.CategoryOutOfScope)
	assert.Contains(t, got, reclib.CategoryOther)
}

// The doc tracks the in-tree vocabulary, so a member added here is documented
// immediately — but atcr renders the RELEASED module into its prompts, and until
// the module is tagged and the go.mod pin bumped, users are offered the older
// list. That gap is a real, expected, temporary state, not a defect in the doc,
// so this reports it and skips instead of failing: a failure here would put the
// deadlock back one step from where it was.
func TestReconcileModule_ReleasedVocabularyMatchesInTree(t *testing.T) {
	inTree := inTreeVocabulary(t)
	released := reclib.Categories()

	if !slices.Equal(released, inTree) {
		t.Skipf("the in-tree reconcile vocabulary is ahead of the released module pinned in go.mod — "+
			"release reconcile and bump the pin.\n  in-tree  (%d): %s\n  released (%d): %s",
			len(inTree), strings.Join(inTree, ", "), len(released), strings.Join(released, ", "))
	}

	assert.Equal(t, inTree, released,
		"released and in-tree vocabularies agree, so this must hold — a mismatch reaching here means the skip above stopped detecting the release window")
}

// `other` and `out-of-scope` are not defect classes: `other` is the escape hatch
// that keeps the set closed rather than lossy, and `out-of-scope` is a control token
// whose findings are annotated instead of promoted. A reader who meets them as
// ordinary categories will misuse both, so the table's Group cell must mark them as
// routing values.
func TestFindingsFormatDoc_TaxonomyTableMarksRoutingValues(t *testing.T) {
	rows := taxonomyTableRows(taxonomySectionLines(t))

	for _, routing := range []string{reclib.CategoryOutOfScope, reclib.CategoryOther} {
		found := false
		for _, row := range rows {
			if row.name != routing {
				continue
			}
			found = true
			assert.Equal(t, "**"+taxonomyRoutingMarker+"**", strings.TrimSpace(row.cells[2]),
				"the Group cell of the %q row in %s must be exactly %q — a bare mention (even a negation like %q) does not mark the routing role", routing, findingsFormatDoc, "**"+taxonomyRoutingMarker+"**", "Not a "+taxonomyRoutingMarker)
		}
		assert.True(t, found, "the %q table in %s must carry a row for the routing value %q", taxonomyHeading, findingsFormatDoc, routing)
	}

	// Distinguishing is a two-sided claim: the marker must appear on exactly the
	// routing rows. A table in which every Group cell reads "Routing value" passes
	// the loop above — this walk is what fails it.
	for _, row := range rows {
		isRouting := row.name == reclib.CategoryOutOfScope || row.name == reclib.CategoryOther
		assert.Equal(t, isRouting, strings.Contains(row.cells[2], taxonomyRoutingMarker),
			"the Group cell of the %q row in %s must contain %q if and only if the category is a routing value", row.name, findingsFormatDoc, taxonomyRoutingMarker)
	}
}

// The Group column is the second hand-maintained restatement of category.go, and
// a reworded or misfiled gloss there is invisible to the membership assertion
// above. What is pinnable without exporting the glosses is the SHAPE the column
// describes: categories declared in one commented block of the const declaration
// must share a Group cell, and separate blocks must not share one. Moving
// `naming` out of "Structure and design", or merging two blocks in the doc that
// the constant keeps apart, fails here.
//
// out-of-scope is absent from the partition because inTreeCategoryBlocks excludes
// that constant by NAME wherever it is declared — not because of where merge.go
// happens to declare it. Its Group cell is pinned as a routing value by
// TestFindingsFormatDoc_TaxonomyTableMarksRoutingValues above instead.
func TestFindingsFormatDoc_GroupColumnFollowsDeclaredBlocks(t *testing.T) {
	group := map[string]string{}
	for _, row := range taxonomyTableRows(taxonomySectionLines(t)) {
		require.GreaterOrEqual(t, len(row.cells), 3, "row %q must have a Group cell", row.name)
		group[row.name] = strings.TrimSpace(row.cells[2])
	}

	blocks, err := inTreeCategoryBlocks(inTreeReconcileDir)
	require.NoError(t, err,
		"this is a doc-table test, but inTreeCategoryBlocks fails for reasons that are not the doc table's: a category.go block fault, or — wrapped in %q — a fault in the categories slice that inTreeCategoryDecls read. Read the wrapped error before editing %s",
		categoriesSliceWrap, findingsFormatDoc)
	require.Greater(t, len(blocks), 1, "a single block would make this guard vacuous")

	seen := map[string]int{} // Group cell -> index of the block that claimed it
	for i, block := range blocks {
		var want string
		for j, category := range block {
			got, ok := group[category]
			require.True(t, ok, "%s declares %q but the %q table in %s has no row for it",
				inTreeReconcileDir, category, taxonomyHeading, findingsFormatDoc)
			if j == 0 {
				want = got
				if claimed, dup := seen[want]; dup {
					assert.Failf(t, "two declared blocks share a Group cell",
						"block %d and block %d both use Group cell %q in %s — separate blocks in %s/category.go must be distinguishable in the table",
						claimed, i, want, findingsFormatDoc, inTreeReconcileDir)
				}
				seen[want] = i
				continue
			}
			assert.Equal(t, want, got,
				"%q and %q are declared in the same block of %s/category.go, so their Group cells in %s must match",
				block[0], category, inTreeReconcileDir, findingsFormatDoc)
		}
	}
}

// The epic's Risks table mitigates "T1 option (ii) changes what inTreeCategoryBlocks
// returns for the real module" with "Assert the real-module partition is
// byte-identical before and after (6 blocks: ...)". The structural pin above
// (require.Greater(t, len(blocks), 1)) is deliberately weak — it only keeps the
// guard from going vacuous — so without this test the mitigation was prose with
// nothing behind it: a mutation of reconcile/category.go that moved a category
// between blocks left the whole suite green. Pin the partition itself: the exact
// 6-block shape the real module declares today, members in declared order.
// A block added, removed, split, merged, or reordered reds here.
func TestFindingsFormatDoc_RealModulePartitionIsPinned(t *testing.T) {
	blocks, err := inTreeCategoryBlocks(inTreeReconcileDir)
	require.NoError(t, err,
		"this is a partition pin, but inTreeCategoryBlocks fails for reasons that are not the pin's: a category.go block fault, or — wrapped in %q — a fault in the categories slice that inTreeCategoryDecls read",
		categoriesSliceWrap)

	assert.Equal(t, [][]string{
		{"correctness", "logic", "security", "secret", "performance", "concurrency", "race", "error-handling", "state", "invariant", "type"},
		{"api-contract", "contract", "validation", "input-validation"},
		{"resource-leak", "leak", "dependency", "configuration"},
		{"coupling", "complexity", "bloat", "duplication", "extensibility", "maintainability", "naming", "style"},
		{"observability", "testing", "docs"},
		{"other"},
	}, blocks,
		"the real module's block partition moved — if category.go's restructure is intended, update this pin and the %q Group column together; if it is not, restore the block boundaries",
		taxonomyHeading)
}
