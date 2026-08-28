package reconcile

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// inTreeCategories returns the CATEGORY vocabulary declared by the reconcile
// module source at dir, in its declared offer order.
//
// It exists because reclib.Categories() is AMBIENT: reconcile is a separate
// published module, so which vocabulary that call resolves to depends on the
// build environment — the in-tree module under a local go.work, the version
// pinned in go.mod under GOWORK=off (what CI sets). Pinning the doc table
// against it made the guard unsatisfiable during the release window: an in-tree
// addition demanded one table locally and a different table in CI, and no table
// content satisfied both. Reading the source by path resolves identically in
// both environments, so the table has exactly one authority and an in-tree
// addition fails the adding PR's own CI run rather than a later bystander's.
//
// The walk mirrors reconcile/category_ast_test.go: parse every non-test file in
// the package directory (CategoryOutOfScope is declared in merge.go, not
// category.go), collect the Category* string constants, then resolve the
// `categories` slice literal through them. parser.ParseDir is deprecated as of
// Go 1.25, so files are read individually.
func inTreeCategories(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	fset := token.NewFileSet()
	declared := map[string]string{} // const name -> its string value
	var order []ast.Expr            // the categories slice's elements, once found

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}

		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				switch gen.Tok {
				case token.CONST:
					for i, ident := range vs.Names {
						if !strings.HasPrefix(ident.Name, "Category") || i >= len(vs.Values) {
							continue
						}
						if value, ok := stringLiteral(vs.Values[i]); ok {
							declared[ident.Name] = value
						}
					}
				case token.VAR:
					for i, ident := range vs.Names {
						if ident.Name != "categories" || i >= len(vs.Values) {
							continue
						}
						lit, ok := vs.Values[i].(*ast.CompositeLit)
						if !ok {
							continue
						}
						order = lit.Elts
					}
				}
			}
		}
	}

	// An absent or empty slice must be an error, never an empty vocabulary: an
	// empty result compared against the doc table would report the TABLE as
	// wrong when the real fault is that the source was never read.
	if len(order) == 0 {
		return nil, fmt.Errorf("no non-empty `categories` slice declared in %s", dir)
	}

	out := make([]string, 0, len(order))
	for _, elt := range order {
		switch e := elt.(type) {
		case *ast.Ident:
			value, ok := declared[e.Name]
			if !ok {
				return nil, fmt.Errorf("categories references %s, which no Category* constant in %s declares", e.Name, dir)
			}
			out = append(out, value)
		default:
			value, ok := stringLiteral(elt)
			if !ok {
				return nil, fmt.Errorf("categories in %s holds an element that is neither a Category* identifier nor a string literal", dir)
			}
			out = append(out, value)
		}
	}
	return out, nil
}

// stringLiteral unquotes a plain string-literal expression, reporting false for
// anything else (a concatenation, a call, a constant of another type).
func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}
