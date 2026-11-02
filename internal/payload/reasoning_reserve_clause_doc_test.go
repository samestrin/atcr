package payload

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReasoningReserveClause_DocNamesEnclosingFunction keeps the doc comment on
// ReasoningReserveClause naming where review calls it. The comment once called
// both zero-budget warnings "warnOversized arms"; warnOversized is a bool
// parameter, not an arm or a function, so the reader had nothing to search for.
// The test checks the code side too: every call in internal/fanout/review.go sits
// inside buildSlotsFor, and buildSlotsFor takes warnOversized, so the comment
// fails here if a call moves out of it.
func TestReasoningReserveClause_DocNamesEnclosingFunction(t *testing.T) {
	fset := token.NewFileSet()
	sizing, err := parser.ParseFile(fset, "sizing.go", nil, parser.ParseComments)
	require.NoError(t, err)
	var doc string
	for _, d := range sizing.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "ReasoningReserveClause" {
			doc = fn.Doc.Text()
		}
	}
	require.NotEmpty(t, doc, "ReasoningReserveClause must keep its doc comment")
	assert.Contains(t, doc, "buildSlotsFor", "the comment must name the function both warnings live in")
	assert.Contains(t, doc, "internal/fanout/review.go")
	assert.NotContains(t, doc, "(warnOversized,", "warnOversized is a parameter, not an arm or a function")

	review, err := parser.ParseFile(fset, filepath.Join("..", "fanout", "review.go"), nil, 0)
	require.NoError(t, err)
	var enclosing *ast.FuncDecl
	for _, d := range review.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "buildSlotsFor" {
			enclosing = fn
		}
	}
	require.NotNil(t, enclosing, "buildSlotsFor must exist in internal/fanout/review.go")

	hasParam := false
	for _, f := range enclosing.Type.Params.List {
		for _, n := range f.Names {
			if n.Name == "warnOversized" {
				hasParam = true
			}
		}
	}
	assert.True(t, hasParam, "buildSlotsFor must gate the warnings with its warnOversized parameter")

	calls := 0
	ast.Inspect(review, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "ReasoningReserveClause" {
			return true
		}
		calls++
		assert.True(t, sel.Pos() >= enclosing.Pos() && sel.End() <= enclosing.End(),
			"call at %s is outside buildSlotsFor", fset.Position(sel.Pos()))
		return true
	})
	assert.Equal(t, 2, calls, "the comment describes exactly two zero-budget warnings")
}
