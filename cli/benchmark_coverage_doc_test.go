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

// A Go doc comment binds to whatever declaration follows it, so inserting a function
// between an existing comment and its own declaration silently REASSIGNS the
// documentation — `go doc -u` then prints one function's rationale under another's
// name, and the documented function has none at all. It compiles, vets and tests
// clean, which is exactly why it needs a mechanical pin.
//
// The rule asserted here is the weakest one that catches it: every gate in this file
// opens its doc comment with its own name, the convention gofmt and `go doc` are built
// around.
func TestBenchmarkCoverage_GatesDocumentThemselves(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "benchmark_coverage.go", nil, parser.ParseComments)
	require.NoError(t, err)

	want := map[string]bool{
		"validateScrubbedCaseIDs":             false,
		"validateSuiteIdentityForPublication": false,
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if _, tracked := want[fn.Name.Name]; !tracked {
			continue
		}
		want[fn.Name.Name] = true
		require.NotNil(t, fn.Doc, "%s has no doc comment: a sibling was likely inserted between its comment and its declaration", fn.Name.Name)
		assert.True(t, strings.HasPrefix(strings.TrimSpace(fn.Doc.Text()), fn.Name.Name),
			"%s's doc comment opens with %q — a doc comment that names another function is documentation the reader will attribute to the wrong gate",
			fn.Name.Name, strings.SplitN(strings.TrimSpace(fn.Doc.Text()), "\n", 2)[0])
	}
	for name, seen := range want {
		assert.True(t, seen, "%s was not found in benchmark_coverage.go — update this test with the gate's new home", name)
	}
}
