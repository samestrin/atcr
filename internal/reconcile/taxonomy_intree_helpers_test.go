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

// inTreeCategoryBlocks returns the vocabulary at dir partitioned into the blocks
// its const declaration marks with a leading comment ("Defect classes …",
// "Contract and interface …", …), in declared order.
//
// It backs the doc table's Group column. Pinning that column's TEXT would mean
// exporting the glosses from the reconcile module — new public API on a module
// external tools embed — so what is pinned instead is the partition: which
// categories share a Group cell, and where the boundaries fall. A category moved
// between blocks, or a block split or merged, then fails the guard, while the
// wording of either side stays free.
//
// Only category.go is read: the table's Group column restates that one file's
// block structure, and walking the whole directory let a cosmetic refactor of
// merge.go (parenthesizing its const, the style it already uses for Sev*/Conf*)
// move the partition and fail the guard with a message naming category.go.
// CategoryOutOfScope is excluded by NAME, wherever and however it is declared —
// it is a routing value whose Group cell is pinned separately.
func inTreeCategoryBlocks(dir string) ([][]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(dir, "category.go"), nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse category.go: %w", err)
	}

	var blocks [][]string

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
			// A leading comment opens a new block; a spec without one
			// continues the block it follows. A constant declared before any
			// comment belongs to no block and is skipped rather than
			// silently opening one.
			if vs.Doc != nil {
				blocks = append(blocks, nil)
			}
			if len(blocks) == 0 {
				continue
			}
			for i, ident := range vs.Names {
				if !strings.HasPrefix(ident.Name, "Category") || i >= len(vs.Values) {
					continue
				}
				if ident.Name == "CategoryOutOfScope" {
					continue
				}
				if value, ok := stringLiteral(vs.Values[i]); ok {
					blocks[len(blocks)-1] = append(blocks[len(blocks)-1], value)
				}
			}
		}
	}

	// Drop blocks a comment opened that hold no category (a note between
	// groups), so an empty run never reaches a caller as a real block.
	out := make([][]string, 0, len(blocks))
	for _, b := range blocks {
		if len(b) > 0 {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no comment-marked Category* blocks declared in %s", dir)
	}

	// Cross-check the partition against the categories slice. The two doc guards
	// read different authorities — inTreeCategories resolves the slice, this
	// function walks the const blocks — so a constant that reaches a block but
	// never the slice leaves them mutually unsatisfiable, both blaming the doc
	// table. Fail here instead, naming the slice as the side at fault.
	vocabulary, err := inTreeCategories(dir)
	if err != nil {
		return nil, err
	}
	inSlice := make(map[string]bool, len(vocabulary))
	for _, value := range vocabulary {
		inSlice[value] = true
	}
	for _, b := range out {
		for _, value := range b {
			if !inSlice[value] {
				return nil, fmt.Errorf("%q is declared in a comment-marked block of %s but absent from the categories slice", value, dir)
			}
		}
	}
	return out, nil
}
