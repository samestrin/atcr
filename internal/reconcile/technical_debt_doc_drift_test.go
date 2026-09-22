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

// skillResolveDocPath is the agent-facing resolve route. It is the PRODUCER
// side of the status vocabulary: `docs/technical-debt.md` tells an operator the
// statuses exist, but this file is what tells the agent that actually closes
// findings when to use them.
//
// It is covered here because a status nothing writes is a ground-truth signal
// that stays permanently empty — and the emptiness is indistinguishable from a
// reviewer that simply never produced that outcome. cli/debt_exhaustive_test.go
// guards the same failure one layer down, at the CLI vocabulary; this guards it
// at the layer that decides to call the CLI at all.
const skillResolveDocPath = "../../skills/atcr/debt-resolve.md"

// TestSkillResolveDocDocumentsEveryWritableStatus locks the producer side: every
// status `atcr debt resolve` accepts must be documented in the route that drives
// it.
func TestSkillResolveDocDocumentsEveryWritableStatus(t *testing.T) {
	b, err := os.ReadFile(skillResolveDocPath)
	require.NoError(t, err, "the agent-facing resolve route must be readable from this package")
	doc := string(b)

	// The writable set, minus `deferred` (written by other paths, not by resolve)
	// and `resolved` (the bare, statusless default this route already documents).
	for _, status := range localdebtTerminalStatuses(t) {
		if status == "deferred" || status == "resolved" {
			continue
		}
		assert.Contains(t, doc, "--status "+status,
			"%s never tells the resolving agent how to write %q, so nothing will ever "+
				"produce it and the ground-truth signal stays empty", skillResolveDocPath, status)
	}
}

// TestSkillResolveDocStatesTheGeneralizedReasonRule guards the half an agent is
// most likely to get wrong: it must not learn "--reason is for wontfix".
func TestSkillResolveDocStatesTheGeneralizedReasonRule(t *testing.T) {
	b, err := os.ReadFile(skillResolveDocPath)
	require.NoError(t, err)
	assert.Contains(t, string(b), "`--reason` is required for every status except",
		"the route must state the generalized --reason rule, not only the wontfix case")
}

// backfillSourcePath is the command whose printed summary the catalog quotes as
// a worked example.
const backfillSourcePath = "../../cli/debt_backfill.go"

// TestTechnicalDebtDoc_BackfillSampleMatchesThePrintedLabel closes the drift
// that actually bit during Story 36.0: the backfill summary's "skipped (…)"
// label existed in THREE places — the format string, a test assertion, and a
// sample block in this catalog — and renaming it in one left the other two
// wrong. The test assertion failed loudly (it was the red suite); the doc sample
// failed silently and would have rotted indefinitely, because the other guards
// in this file cover status tables and flag prose, not quoted output.
//
// The expected text is extracted from the AST, not by scanning the file's bytes.
// A byte scan takes the first "skipped (" anywhere in the source, COMMENTS
// INCLUDED — so a package comment quoting the old wording would keep this guard
// green while the printed label drifted, which is precisely the silent
// false-negative it exists to prevent. Parsing with a nil comment map makes
// comments unreachable by construction rather than by care.
func TestTechnicalDebtDoc_BackfillSampleMatchesThePrintedLabel(t *testing.T) {
	label := backfillSkippedLabel(t)
	assert.Contains(t, technicalDebtDoc(t), label,
		"the catalog's backfill sample output must quote the label the command actually prints (%q); "+
			"this is the third copy of that string and the one nothing else guards", label)
}

// backfillSkippedLabel returns the literal "skipped (…)" clause from the backfill
// summary's format string, read out of the AST as a string literal. It requires
// exactly one match so an ambiguous source — two format strings carrying the
// clause — fails loudly instead of silently pinning the wrong one.
func backfillSkippedLabel(t *testing.T) string {
	t.Helper()
	fset := token.NewFileSet()
	// parser.ParseComments is deliberately NOT passed: comments must not be
	// searchable, or a stale quotation in prose satisfies the guard.
	f, err := parser.ParseFile(fset, backfillSourcePath, nil, 0)
	require.NoError(t, err, "the backfill command source must parse")

	const marker = "skipped ("
	var found []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		i := strings.Index(v, marker)
		if i < 0 {
			return true
		}
		rest := v[i:]
		j := strings.Index(rest, ")")
		if j < 0 {
			return true
		}
		found = append(found, rest[:j+1])
		return true
	})

	require.Len(t, found, 1,
		"expected exactly one string literal carrying a %q clause in %s; "+
			"zero means the summary was reworded and this guard needs a new anchor, "+
			"more than one means the anchor is ambiguous and could pin the wrong copy",
		marker, backfillSourcePath)
	require.NotContains(t, found[0], "%",
		"the extracted clause must be literal text, not a format verb, or this guard compares nothing")
	return found[0]
}

// TestTechnicalDebtDoc_CompactGuaranteeIsNotAnAbsolute pins the `atcr debt
// compact` retention guarantee, which has now stated a falsifiable absolute
// twice in two review rounds.
//
// The passage is prose, not a table row, so nothing else in this file touches
// it. Both wrong versions said some form of "the --reason text is never
// destroyed"; retainForCompaction keeps exactly ONE superseded rationale per id
// — the highest-ranked — so several distinct reasons on one id collapse to one.
// A doc that promises more than the bound delivers is how an operator loses an
// attempt round and only finds out afterwards.
func TestTechnicalDebtDoc_CompactGuaranteeIsNotAnAbsolute(t *testing.T) {
	doc := technicalDebtDoc(t)

	assert.NotContains(t, doc, "never destroyed",
		"the compact guarantee must not promise that no --reason is ever destroyed; "+
			"only the highest-ranked superseded rationale is retained")
	assert.Contains(t, doc, "only that highest-ranked one survives",
		"the compact passage must state the one-deep bound explicitly")
	assert.Contains(t, doc, "`wontfix` > `unreproducible` > `attempts-exhausted` > `resolved`",
		"and must spell the rank that decides which reason survives, in ClosedStatusRank's order")
}

// TestBackfillSkipSetProseKeysOnRationaleNotSettledness pins both prose surfaces
// that describe which records `backfill-justifications` declines to scan.
//
// The gate is bearsRationale (internal/localdebt/record.go), not IsSettledStatus:
// Story 36.0 is exactly where those two stopped selecting the same records.
// `attempts-exhausted` is deliberately NOT settled — it is unfinished work that
// must stay closeable — yet `--reason` is mandatory for it, so it always carries
// the operator text the skip exists to protect. Both surfaces still described the
// skip set as "a resolved or wontfix record is settled", which names two of the
// four skipped statuses and gives a criterion the code no longer uses. An
// operator reading either one concludes their unreproducible and
// attempts-exhausted rationales are in scope for overwrite.
//
// Asserted on BOTH sides, the way this file's siblings are: pinning only the
// catalog catches a doc edit and not a help-text edit, and the help text is what
// an operator reads at the moment they decide to run the repair.
func TestBackfillSkipSetProseKeysOnRationaleNotSettledness(t *testing.T) {
	doc := technicalDebtDoc(t)
	b, err := os.ReadFile(backfillSourcePath)
	require.NoError(t, err, "the backfill command source must be readable from this package")
	help := string(b)

	for _, tc := range []struct {
		name string
		text string
	}{
		{"published catalog", doc},
		{"command help text", help},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.NotContains(t, tc.text, "is settled, and its justification may be",
				"the skip set must not be justified by settledness: attempts-exhausted is skipped and is NOT settled")
			assert.NotContains(t, tc.text, "record is settled and is never scanned",
				"same — the criterion the code uses is bearsRationale, not IsSettledStatus")
			for _, status := range []string{"resolved", "wontfix", "unreproducible", "attempts-exhausted"} {
				assert.Contains(t, tc.text, status,
					"the skip set must name every status bearsRationale covers, including %q", status)
			}
			assert.Contains(t, tc.text, "cannot be replayed",
				"and must give the reason the skip exists — the operator text exists nowhere else")
		})
	}
}
