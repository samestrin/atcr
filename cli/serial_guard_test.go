package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serialSeams are the package-level function vars tests swap for a stub
// (debateRun in review.go, verifyRun in verify.go, debtGetwdFn in debt.go).
// The swaps are unsynchronized: they are safe only because Go starts a
// parallel test after every serial test of its level has finished and
// restored the seam, so a test that swaps one must never call t.Parallel.
var serialSeams = map[string]bool{
	"debateRun":   true,
	"verifyRun":   true,
	"debtGetwdFn": true,
}

// seamViolation is a function that both swaps a seam and calls t.Parallel.
type seamViolation struct {
	fn  string
	pos token.Position
}

// parallelSeamViolations returns, sorted by name, one entry per t.Parallel
// call in a top-level function of files (nested function literals included)
// that also writes a seam. A write is an assignment to the seam, taking its
// address, or a use of a package function that writes one, such as
// stubVerifyRun; writers are found to a fixed point, so a helper that calls a
// seam-writing helper is itself a writer.
func parallelSeamViolations(fset *token.FileSet, files []*ast.File) []seamViolation {
	type fn struct {
		name string
		decl *ast.FuncDecl
	}
	var fns []fn
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				fns = append(fns, fn{fd.Name.Name, fd})
			}
		}
	}

	// uses reports whether body writes a seam directly or names a function
	// already known to write one.
	uses := func(body *ast.BlockStmt, writers map[string]bool) bool {
		found := false
		ast.Inspect(body, func(n ast.Node) bool {
			if found {
				return false
			}
			switch n := n.(type) {
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && serialSeams[id.Name] {
						found = true
					}
				}
			case *ast.UnaryExpr:
				if id, ok := n.X.(*ast.Ident); ok && n.Op == token.AND && serialSeams[id.Name] {
					found = true
				}
			case *ast.Ident:
				// A call or a function value. Method selectors land here too
				// (Inspect visits Sel), matched by name since receiver types
				// are not resolved without type checking.
				if writers[n.Name] {
					found = true
				}
			}
			return !found
		})
		return found
	}

	writers := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, f := range fns {
			if !writers[f.name] && uses(f.decl.Body, writers) {
				writers[f.name] = true
				changed = true
			}
		}
	}

	var out []seamViolation
	for _, f := range fns {
		if !writers[f.name] {
			continue
		}
		ast.Inspect(f.decl.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Parallel" && len(call.Args) == 0 {
				out = append(out, seamViolation{f.name, fset.Position(call.Pos())})
				return false
			}
			return true
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].fn < out[j].fn })
	return out
}

// TestPackageSeamSwapsStaySerial keeps the precondition every seam-swapping test
// in this package relies on: none of them runs in parallel.
func TestPackageSeamSwapsStaySerial(t *testing.T) {
	paths, err := filepath.Glob("*_test.go")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	fset := token.NewFileSet()
	var files []*ast.File
	for _, path := range paths {
		file, err := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, err, "parse %s", path)
		files = append(files, file)
	}
	for _, v := range parallelSeamViolations(fset, files) {
		t.Errorf("%s (%s): a test that swaps debateRun, verifyRun or debtGetwdFn (directly or through a helper) "+
			"calls t.Parallel, so it races every other test that reads or swaps the seam", v.pos, v.fn)
	}
}

// TestParallelSeamViolations_Fixture proves the guard catches a parallel seam
// swap in every shape the package uses, and passes the shapes that are safe.
func TestParallelSeamViolations_Fixture(t *testing.T) {
	const src = `package cli

func stubVerify(t *testing.T) {
	orig := verifyRun
	verifyRun = nil
	t.Cleanup(func() { verifyRun = orig })
}

func stubViaHelper(t *testing.T) { stubVerify(t) }

func swap[T any](p *T, v T) { *p = v }

type seamFixture struct{}

func (seamFixture) stubDebate() { debateRun = nil }

func TestDirect(t *testing.T) {
	t.Parallel()
	debateRun = nil
}

func TestViaHelper(t *testing.T) {
	t.Parallel()
	stubVerify(t)
}

func TestViaNestedHelper(t *testing.T) {
	t.Parallel()
	stubViaHelper(t)
}

func TestViaAddress(t *testing.T) {
	t.Parallel()
	swap(&debtGetwdFn, nil)
}

func TestViaMethod(t *testing.T) {
	t.Parallel()
	seamFixture{}.stubDebate()
}

func TestParallelSubtest(t *testing.T) {
	t.Run("x", func(t *testing.T) {
		t.Parallel()
		stubVerify(t)
	})
}

func TestSubtestValue(t *testing.T) {
	t.Parallel()
	t.Run("x", stubVerify)
}

func TestSerialSwap(t *testing.T) {
	stubVerify(t)
	debtGetwdFn = nil
}

func TestParallelReadOnly(t *testing.T) {
	t.Parallel()
	_ = verifyRun
	other = nil
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture_test.go", src, 0)
	require.NoError(t, err)

	var got []string
	for _, v := range parallelSeamViolations(fset, []*ast.File{file}) {
		got = append(got, v.fn)
	}
	assert.Equal(t, []string{
		"TestDirect", "TestParallelSubtest", "TestSubtestValue", "TestViaAddress",
		"TestViaHelper", "TestViaMethod", "TestViaNestedHelper",
	}, got)
}
