package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResumeRequireCoverageCarveOutSitsBesideCheck keeps the --require-coverage
// carve-out comment in resume.go directly above the requireCoverageThreshold(cmd)
// call it explains, rather than above the fail-on/verify rejection loop where a
// reader of the coverage check cannot see why --resume accepts the flag.
func TestResumeRequireCoverageCarveOutSitsBesideCheck(t *testing.T) {
	const marker = "--require-coverage is deliberately NOT"

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "resume.go", nil, parser.ParseComments)
	require.NoError(t, err)

	checkLine := 0
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "requireCoverageThreshold" {
			checkLine = fset.Position(as.Pos()).Line
			return false
		}
		return true
	})
	require.NotZero(t, checkLine, "resume.go no longer calls requireCoverageThreshold")

	var carveOuts []*ast.CommentGroup
	for _, cg := range f.Comments {
		if strings.Contains(cg.Text(), marker) {
			carveOuts = append(carveOuts, cg)
		}
	}
	require.Len(t, carveOuts, 1, "resume.go must carry exactly one --require-coverage carve-out comment")
	assert.Equal(t, checkLine-1, fset.Position(carveOuts[0].End()).Line,
		"the --require-coverage carve-out comment must end on the line directly above requireCoverageThreshold(cmd)")
}
