package reconcile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// technical_debt_doc_drift_test.go is the drift guard between the published
// technical-debt catalog and internal/localdebt's terminal-status enum.
//
// It lives in internal/reconcile/ for the reason CLAUDE.md and
// .planning/specifications/coding-standards.md § Testing Standards both give: a
// doc-vs-code drift test cannot live in docs/ (no Go package there) and must not
// live in the published top-level reconcile/ module, which is consumed elsewhere
// and so must not assume this repo's file layout via a relative docs/*.md read.
// The precedent is justification_record_boundary_test.go and
// metrics_doc_drift_test.go, whose exact-row-match technique this file reuses.
//
// The expected status set is derived from internal/localdebt's SOURCE rather
// than from a hand-typed literal list, for metrics_doc_drift_test.go's own
// stated reason: a literal list drifts the moment a status is added and the list
// is not, so the guard silently stops guarding the very next status.

const technicalDebtDocPath = "../../docs/technical-debt.md"
const localdebtRecordSourceDir = "../localdebt"

// technicalDebtDoc returns the published catalog's contents.
func technicalDebtDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(technicalDebtDocPath)
	require.NoError(t, err, "the published technical-debt catalog must be readable from this package")
	return string(b)
}

// localdebtTerminalStatuses returns every terminal status VALUE internal/localdebt
// declares, read from that package's sources.
func localdebtTerminalStatuses(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(localdebtRecordSourceDir)
	require.NoError(t, err, "the localdebt package sources must be readable from this package")

	var out []string
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(localdebtRecordSourceDir, name), nil, 0)
		require.NoError(t, err, "parsing %s", name)
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ident := range vs.Names {
					if !strings.HasPrefix(ident.Name, "Status") || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					v, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					out = append(out, v)
				}
			}
		}
	}
	require.NotEmpty(t, out,
		"the localdebt package must declare terminal-status constants — an empty result "+
			"means the declaration shape drifted from this scan and the guard is pinned "+
			"against nothing, which is the silent-drift failure it exists to close")
	return out
}

// docPassage returns the single line of the catalog that starts with prefix.
//
// Matching on a structural anchor rather than searching the whole file is the
// point: unreproducible and attempts-exhausted are named in several of the five
// passages, so a bare strings.Contains over the file would report every passage
// as correct the moment any one of them named the status.
func docPassage(t *testing.T, doc, prefix string) (string, bool) {
	t.Helper()
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line, true
		}
	}
	return "", false
}

// TestTechnicalDebtDoc_SchemaStatusRowListsEveryStatus covers passage 1 of 5:
// the wire-schema table's `status` row.
func TestTechnicalDebtDoc_SchemaStatusRowListsEveryStatus(t *testing.T) {
	doc := technicalDebtDoc(t)
	row, ok := docPassage(t, doc, "| `status` |")
	require.True(t, ok, "the schema table must document the status field")

	for _, status := range localdebtTerminalStatuses(t) {
		assert.Contains(t, row, "`"+status+"`",
			"the schema `status` row must list %q; a reader decoding a record otherwise "+
				"meets a value the published schema says cannot exist", status)
	}
}

// TestTechnicalDebtDoc_StatusLifetimesTableHasARowPerStatus covers passage 2 of
// 5: the Status lifetimes table, which must carry one row per terminal status.
func TestTechnicalDebtDoc_StatusLifetimesTableHasARowPerStatus(t *testing.T) {
	doc := technicalDebtDoc(t)
	for _, status := range localdebtTerminalStatuses(t) {
		_, ok := docPassage(t, doc, "| `"+status+"` |")
		assert.True(t, ok,
			"the Status lifetimes table has no row of its own for %q, so an operator "+
				"cannot learn whether it survives a re-detection", status)
	}
}

// TestTechnicalDebtDoc_PrecedenceSentenceMatchesTheRankChain covers passage 3 of
// 5, and it is the passage the story flags as most likely to be missed: it is
// PROSE, not a table row, so a row-oriented drift check passes while the
// sentence silently contradicts the code.
//
// The expected chain is sprint-plan.md → Phase 1 Clarifications → C1's flipped
// order, which supersedes plan.md's originally-decided one:
// wontfix > unreproducible > attempts-exhausted > resolved > deferred.
func TestTechnicalDebtDoc_PrecedenceSentenceMatchesTheRankChain(t *testing.T) {
	doc := technicalDebtDoc(t)
	const wantChain = "`wontfix` > `unreproducible` > `attempts-exhausted` > `resolved` > `deferred`"

	assert.Contains(t, doc, wantChain,
		"the published precedence sentence must spell the full rank chain exactly as "+
			"ClosedStatusRank orders it; it is prose rather than a table row, so nothing "+
			"else in this file would catch it drifting")
	assert.NotContains(t, doc, "`wontfix` > `resolved` > `deferred`",
		"the three-term precedence sentence must be gone, not merely added to")
}

// TestTechnicalDebtDoc_ListFlagProseListsEveryFilterValue covers passage 4 of 5:
// the `atcr debt list` flag prose, which publishes the --status filter values.
func TestTechnicalDebtDoc_ListFlagProseListsEveryFilterValue(t *testing.T) {
	doc := technicalDebtDoc(t)
	line, ok := docPassage(t, doc, "`open|deferred|resolved|wontfix")
	require.True(t, ok,
		"the `debt list` flag prose must publish the --status filter values on one line "+
			"starting with the value list")

	for _, status := range localdebtTerminalStatuses(t) {
		assert.Contains(t, line, status,
			"`debt list --status` accepts %q, but the published flag prose omits it", status)
	}
	assert.Contains(t, line, "open", "the open filter value must stay published")
}

// TestTechnicalDebtDoc_ResolveFlagProseListsEveryWritableStatus covers passage 5
// of 5: the `atcr debt resolve` flag prose. It publishes the writable subset —
// deferred is excluded, because deferral is written by other paths — and the
// widened --reason gate.
func TestTechnicalDebtDoc_ResolveFlagProseListsEveryWritableStatus(t *testing.T) {
	doc := technicalDebtDoc(t)
	line, ok := docPassage(t, doc, "`--status` (`resolved|wontfix")
	require.True(t, ok,
		"the `debt resolve` flag prose must publish its --status values starting with "+
			"the backticked flag name")

	for _, status := range []string{"resolved", "wontfix", "unreproducible", "attempts-exhausted"} {
		assert.Contains(t, line, status,
			"`debt resolve --status` accepts %q, but the published flag prose omits it", status)
	}
	assert.NotContains(t, line, "deferred",
		"deferral is not written by an explicit resolve, so it must not be published as a resolve value")
}

// TestTechnicalDebtDoc_ReasonGateProseIsGeneralized is the doc half of AC 01-03
// Edge Case 3. The gate widened from "wontfix requires --reason" to "every
// non-resolved status requires --reason"; a doc left at the wontfix-only
// sentence tells an operator that `--status unreproducible` alone will work,
// which it will not.
func TestTechnicalDebtDoc_ReasonGateProseIsGeneralized(t *testing.T) {
	doc := technicalDebtDoc(t)
	assert.NotContains(t, doc, "`--status wontfix` requires a",
		"the published --reason gate must no longer read as wontfix-only")
	assert.Contains(t, doc, "Every status other than `resolved` requires a",
		"the published --reason gate must state the generalized rule the code enforces")
}
