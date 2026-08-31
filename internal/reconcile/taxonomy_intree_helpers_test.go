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

// The routing value the block partition excludes, written down once.
//
// outOfScopeConstName is the anchor. The block walk skips the constant carrying
// this NAME, wherever and however it is declared, and the slice cross-check
// exempts the VALUE that same constant resolves to in the parsed source.
// Resolving that value rather than restating it is what keeps a value change
// satisfiable: a second literal on the cross-check side would demand the new
// value be declared in a comment-marked block of category.go, and the name skip
// then strips it straight back out, leaving no vocabulary content that satisfies
// both halves at once.
//
// outOfScopeConstValue is NOT a second authority. It is the tripwire that keeps
// the two halves from drifting apart in the other direction: renaming the
// constant while keeping its value would otherwise slip the routing value into
// the partition silently, because the name skip misses it and the resolved
// exemption no longer exists. When the value reaches the vocabulary with no
// constant of the anchor name behind it, inTreeCategoryBlocks reports that as an
// error instead of absorbing it.
//
// This pair is also where the exemption RULE lives, stated once: the value
// declared by CategoryOutOfScope is the only member of the vocabulary permitted
// to sit outside a comment-marked block of category.go. Every other Category*
// constant must be declared in one, whichever file it lives in.
const (
	outOfScopeConstName  = "CategoryOutOfScope"
	outOfScopeConstValue = "out-of-scope"
)

// categoryElem is one element of the `categories` slice: the Category* constant
// that names it (empty for a bare string-literal element) alongside the value it
// resolves to. The name is what lets a reader of the slice key on the constant's
// identity rather than on the string it happens to hold.
type categoryElem struct {
	name  string
	value string
}

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
// The walk lives in inTreeCategoryDecls below and mirrors
// reconcile/category_ast_test.go: parse every non-test file in the package
// directory (CategoryOutOfScope is declared in merge.go, not category.go),
// collect the Category* string constants, then resolve the `categories` slice
// literal through them. parser.ParseDir is deprecated as of Go 1.25, so files
// are read individually.
func inTreeCategories(dir string) ([]string, error) {
	_, order, err := inTreeCategoryDecls(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(order))
	for _, elem := range order {
		out = append(out, elem.value)
	}
	return out, nil
}

// inTreeCategoryDecls is inTreeCategories' reader, returning both halves of what
// it read: every Category* string constant the directory declares, keyed by
// constant NAME, and the `categories` slice's elements in declared order with
// each element's constant name preserved alongside its value.
//
// inTreeCategories discards the names because the doc table compares values.
// inTreeCategoryBlocks needs them: its exclusion is keyed on a constant's
// identity, not on the string that constant currently holds, and it cannot
// resolve that identity from a slice of bare values.
func inTreeCategoryDecls(dir string) (map[string]string, []categoryElem, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", dir, err)
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
			return nil, nil, fmt.Errorf("parse %s: %w", name, err)
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
		return nil, nil, fmt.Errorf("no non-empty `categories` slice declared in %s", dir)
	}

	out := make([]categoryElem, 0, len(order))
	for _, elt := range order {
		switch e := elt.(type) {
		case *ast.Ident:
			value, ok := declared[e.Name]
			if !ok {
				return nil, nil, fmt.Errorf("categories references %s, which no Category* constant in %s declares", e.Name, dir)
			}
			out = append(out, categoryElem{name: e.Name, value: value})
		default:
			value, ok := stringLiteral(elt)
			if !ok {
				return nil, nil, fmt.Errorf("categories in %s holds an element that is neither a Category* identifier nor a string literal", dir)
			}
			out = append(out, categoryElem{value: value})
		}
	}
	return declared, out, nil
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
// The constant named CategoryOutOfScope is excluded by NAME, wherever and
// however it is declared — it is a routing value whose Group cell is pinned
// separately. Both halves of that exemption resolve from that one constant: the
// walk below skips the name, and the slice cross-check exempts the value the
// same constant holds. Declaring the routing value under a different name is not
// silently followed; it is an error, because the walk would otherwise fold the
// value into the partition with nothing failing. See outOfScopeConstName.
func inTreeCategoryBlocks(dir string) ([][]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(dir, "category.go"), nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse category.go: %w", err)
	}

	var blocks [][]string

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST || !gen.Lparen.IsValid() {
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
				if ident.Name == outOfScopeConstName {
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
	declared, vocabulary, err := inTreeCategoryDecls(dir)
	if err != nil {
		// inTreeCategoryDecls walks the whole directory, so its faults — an
		// unparseable sibling file, a missing or empty `categories` slice, an
		// element no Category* constant declares — would otherwise reach the
		// caller indistinguishable from a category.go block fault, out of a
		// function documented as reading one file.
		return nil, fmt.Errorf("read the categories slice for the block cross-check: %w", err)
	}

	// Resolve the exemption from the anchor constant rather than from a literal.
	// exempt is the value CategoryOutOfScope actually holds, so changing that
	// value leaves the guard satisfiable; anchored is false when no constant of
	// that name exists at all, which most synthetic fixtures are.
	exempt, anchored := declared[outOfScopeConstName]
	if !anchored {
		for _, elem := range vocabulary {
			if elem.value != outOfScopeConstValue {
				continue
			}
			return nil, fmt.Errorf("the routing value %q is listed in the categories slice of %s but no constant named %s declares it — the block partition excludes that value by anchor name, so a rename keeping the value would fold it into the partition with nothing failing; restore the name or re-anchor outOfScopeConstName", outOfScopeConstValue, dir, outOfScopeConstName)
		}
	}

	inSlice := make(map[string]bool, len(vocabulary))
	for _, elem := range vocabulary {
		inSlice[elem.value] = true
	}
	for _, b := range out {
		for _, value := range b {
			if !inSlice[value] {
				return nil, fmt.Errorf("%q is declared in a comment-marked block of %s but absent from the categories slice", value, dir)
			}
		}
	}

	// The reverse direction: a slice member that reaches no block never gets its
	// Group cell checked, because this function is the block authority and it
	// reads category.go alone while inTreeCategoryDecls walks the whole
	// directory. The value CategoryOutOfScope holds is exempt — it is a routing
	// value declared outside category.go (merge.go) by design, and its Group
	// cell is pinned separately.
	inBlock := make(map[string]bool, len(out))
	for _, b := range out {
		for _, value := range b {
			inBlock[value] = true
		}
	}
	for _, elem := range vocabulary {
		if anchored && elem.value == exempt {
			continue
		}
		if !inBlock[elem.value] {
			return nil, fmt.Errorf("%q is listed in the categories slice of %s but appears in no comment-marked block of category.go — declare it there inside a PARENTHESIZED const block opened by a leading comment (an unparenthesized `const CategoryX = ...` carries its comment on the declaration rather than the spec, so it opens no block), or remove it from the slice; the value declared by %s is the only exemption", elem.value, dir, outOfScopeConstName)
		}
	}
	return out, nil
}
