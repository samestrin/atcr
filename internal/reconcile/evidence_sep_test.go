package reconcile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	reclib "github.com/samestrin/atcr/reconcile"
)

// TD internal/verify/executor.go:757: joinEvidence joined with a bare " / "
// literal, so nothing tied the producer to the readers that split on it
// (internal/verify's attribution guards, ghaction.FixAttribution). It joins with
// the exported ClusterEvidenceSep now, which those readers consume.
func TestJoinEvidence_UsesClusterEvidenceSep(t *testing.T) {
	got := joinEvidence([]JSONFinding{{Evidence: "Found by kai"}, {Evidence: "fix by opus"}})
	assert.Equal(t, "Found by kai"+ClusterEvidenceSep+"fix by opus", got)
	assert.Equal(t, " / ", ClusterEvidenceSep)
}

// SplitEvidence's documented contract, pinned directly: it splits on both
// ClusterEvidenceSep and reclib.EvidenceSep, never on a bare "/", and returns the
// segments in order, untrimmed, with empty ones kept.
func TestSplitEvidence(t *testing.T) {
	cases := []struct {
		name     string
		evidence string
		want     []string
	}{
		{"empty string is one empty segment", "", []string{""}},
		{"cluster separator only", ClusterEvidenceSep, []string{"", ""}},
		{"pipeline separator only", reclib.EvidenceSep, []string{"", ""}},
		{"cluster merge", "Found by kai" + ClusterEvidenceSep + "fix by opus", []string{"Found by kai", "fix by opus"}},
		{"pipeline join", "Found by kai" + reclib.EvidenceSep + "fix by opus", []string{"Found by kai", "fix by opus"}},
		{
			"mixed separators keep their order",
			"a" + reclib.EvidenceSep + "b" + ClusterEvidenceSep + "c" + reclib.EvidenceSep + "d",
			[]string{"a", "b", "c", "d"},
		},
		{"adjacent cluster separators keep the empty segment", "a" + ClusterEvidenceSep + ClusterEvidenceSep + "b", []string{"a", "", "b"}},
		{"adjacent pipeline separators keep the empty segment", "a" + reclib.EvidenceSep + reclib.EvidenceSep + "b", []string{"a", "", "b"}},
		{"adjacent mixed separators keep the empty segment", "a" + ClusterEvidenceSep + reclib.EvidenceSep + "b", []string{"a", "", "b"}},
		{"segments are untrimmed", "  a " + ClusterEvidenceSep + " b\t" + reclib.EvidenceSep + "c ", []string{"  a ", " b\t", "c "}},
		{"a bare slash is not a separator", "path/to/fix by hand", []string{"path/to/fix by hand"}},
		{"a slash without both spaces is not a separator", "a /b" + ClusterEvidenceSep + "c/ d", []string{"a /b", "c/ d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SplitEvidence(tc.evidence))
		})
	}
}

// ClusterEvidenceSep is a local copy of a literal the published reconcile module
// owns: mergeEvidence joins merged findings' evidence with strings.Join(parts,
// " / "), and the module exports no constant for it. SplitEvidence is only
// right while the two agree; if the module changes its join, every reader that
// splits through SplitEvidence reads a cluster-merged "Found by kai / fix by
// opus" as one segment and drops its attribution, with a green suite.
//
// So read the module's source, as the doc-drift tests read docs, and pin the
// join literal to the const. Both copies are checked: the version go.mod pins
// (what the build links) and the in-tree ./reconcile (what the next release
// ships), so a change in either turns this red before it reaches a reader.
func TestClusterEvidenceSep_MatchesThePublishedModulesJoinLiteral(t *testing.T) {
	for _, src := range []struct{ name, dir string }{
		{"pinned module", releasedReconcileDir(t)},
		{"in-tree module", filepath.Join(repoRootFromHere, "reconcile")},
	} {
		t.Run(src.name, func(t *testing.T) {
			assert.Equal(t, ClusterEvidenceSep, mergeEvidenceJoinLiteral(t, filepath.Join(src.dir, "merge.go")),
				"ClusterEvidenceSep must equal the separator reconcile's mergeEvidence joins with (%s/merge.go)", src.dir)
		})
	}
}

// mergeEvidenceJoinLiteral parses path and returns the string literal passed as
// the separator to the strings.Join call in mergeEvidence. It fails, rather than
// returning "", when the function, the call or a literal separator is missing:
// a moved or rewritten join is a change this pin has to be told about.
func mergeEvidenceJoinLiteral(t *testing.T, path string) string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	require.NoErrorf(t, err, "parsing %s: if mergeEvidence moved, update this drift test to follow it", path)

	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if d, ok := decl.(*ast.FuncDecl); ok && d.Recv == nil && d.Name.Name == "mergeEvidence" {
			fn = d
		}
	}
	require.NotNilf(t, fn, "%s declares no mergeEvidence: if it moved, update this drift test to follow it", path)

	var seps []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Join" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "strings" {
			return true
		}
		lit, ok := call.Args[1].(*ast.BasicLit)
		require.Truef(t, ok && lit.Kind == token.STRING,
			"mergeEvidence in %s joins with a non-literal separator: pin ClusterEvidenceSep to what it resolves to", path)
		sep, err := strconv.Unquote(lit.Value)
		require.NoError(t, err, "unquoting %s", lit.Value)
		seps = append(seps, sep)
		return true
	})
	require.Lenf(t, seps, 1, "mergeEvidence in %s must make exactly one strings.Join call, found %d", path, len(seps))
	return seps[0]
}
