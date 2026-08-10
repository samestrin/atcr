package reconcile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestCategories_EveryDeclaredConstantIsAMember closes the drift hole the file
// header at category.go:22-24 claims is impossible.
//
// TestCategories_LockedSet pins the CONTENTS of the categories slice, so
// removing or adding a slice entry fails. It cannot see a new exported
// Category* CONSTANT that was never appended to the slice: that compiles, passes
// every other test in this package, and publishes a value to every consumer of
// this module that no reviewer is ever offered. This test walks the package's
// own source with go/ast — stdlib, so the module stays dependency-free — and
// requires each declared Category* constant to be an actual member.
//
// It parses the whole package directory rather than category.go alone, so a
// constant added in merge.go (where CategoryOutOfScope already lives) is caught
// by the same guard.
func TestCategories_EveryDeclaredConstantIsAMember(t *testing.T) {
	members := categorySet()

	// Walk the directory and ParseFile each source file rather than calling
	// parser.ParseDir, which is deprecated as of Go 1.25. The documented
	// replacement is golang.org/x/tools/go/packages, which this module may not
	// use: reconcile is stdlib-only by contract (doc.go — `go mod tidy` must yield
	// an empty require block), and a test-only dependency would still land in
	// go.mod for every external embedder.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()
	declared := map[string]string{} // const name -> its string value
	parsed := 0

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed++

		for _, decl := range file.Decls {
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
					if !strings.HasPrefix(ident.Name, "Category") || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Errorf("%s: constant %s is not a plain string literal, so its membership cannot be checked", name, ident.Name)
						continue
					}
					declared[ident.Name] = strings.Trim(lit.Value, `"`)
				}
			}
		}
	}

	// Non-vacuous: a walk that parsed nothing means the filter or the directory
	// is wrong, and every membership assertion below would pass on an empty set.
	if parsed == 0 {
		t.Fatal("parsed no non-test source files in the package directory")
	}

	// Non-vacuous: a parse that finds no constants (wrong directory, changed
	// naming) must fail rather than pass silently.
	if len(declared) < len(Categories()) {
		t.Fatalf("found only %d Category* constants in package source, fewer than the %d vocabulary members — the AST walk is not seeing the declarations", len(declared), len(Categories()))
	}

	for name, value := range declared {
		if !members[value] {
			t.Errorf("constant %s = %q is declared and exported but is not a member of Categories() — it is offered to no reviewer while every embedder of this module can see it", name, value)
		}
	}
}
