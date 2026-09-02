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
// outOfScopeConstValue is NOT a second authority — the exemption resolves from
// the anchor NAME alone, and no value literal grants it. It is the tripwire that
// keeps the two halves from drifting apart in the other direction: renaming the
// constant while keeping its value would otherwise slip the routing value into
// the partition silently, because the name skip misses it and the resolved
// exemption no longer exists. When the value reaches the vocabulary with no
// constant of the anchor name behind it, inTreeCategoryBlocks reports that as an
// error instead of absorbing it.
//
// It is nonetheless a second, hand-maintained literal, even though it is not a
// second authority: its sole job is the rename tripwire, never exemption. What
// keeps it honest is not a comment but a test:
// TestInTreeCategoryBlocks_AnchorPairMatchesTheRealModule asserts that the
// constant named outOfScopeConstName in the real module actually holds
// outOfScopeConstValue, so changing reconcile/merge.go's value without updating
// this pair reds the suite instead of quietly disarming the tripwire.
//
// This pair is the one place the exemption RULE is stated: the constant named
// CategoryOutOfScope is the only member of the vocabulary permitted to sit
// outside a comment-marked block of category.go SPECIFICALLY — a comment-marked
// block in any other file does not count, because inTreeCategoryBlocks builds
// its partition from category.go alone. Every other Category* constant that
// reaches the `categories` slice must be declared in a block there. A Category*
// constant that reaches a comment-marked block of category.go while the slice
// never lists it IS the block guard's business: the forward cross-check is
// keyed on constant identity, so it fails a member the slice lists under no
// name — whether its value is unlisted ("absent from the categories slice") or
// collides with a listed member's value (an unlisted alias holding a listed
// value would fold silently under a value-keyed check, which is why identity
// is what is keyed).
// Every other mention of this rule in this file points
// back here rather than restating it.
const (
	outOfScopeConstName  = "CategoryOutOfScope"
	outOfScopeConstValue = "out-of-scope"
)

// categoriesSliceWrap prefixes every inTreeCategoryDecls fault that surfaces out
// of inTreeCategoryBlocks, whose own documented job is reading one file. It is a
// named constant because the message is asserted in taxonomy_intree_test.go and
// quoted in findings_format_taxonomy_test.go as failure context, and a reworded
// literal in one copy would turn the others into lies with a green suite.
const categoriesSliceWrap = "read the categories slice for the block cross-check"

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
// inTreeCategoryBlocks needs them: both its exclusion and its rename tripwire
// are keyed on a constant's identity, not on the string that constant currently
// holds, and neither can resolve that identity from a slice of bare values.
//
// The declared map — the first return value — is the name-keyed half of the
// read: TestInTreeCategoryBlocks_AnchorPairMatchesTheRealModule resolves
// outOfScopeConstName through it rather than re-walking the directory a second
// time, and inTreeRoutingValues resolves both routing values through it so the
// taxonomy doc guards key routing identity on the in-tree module instead of the
// ambient reclib constants.
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
							if _, dup := declared[ident.Name]; dup {
								return nil, nil, fmt.Errorf("%s declared twice in %s", ident.Name, dir)
							}
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
// The PARTITION is built from category.go alone: the table's Group column restates
// that one file's block structure, and building the blocks from the whole directory
// would let a cosmetic refactor of merge.go (parenthesizing its const, the style it
// already uses for Sev*/Conf*) move the partition and fail the guard with a message
// naming category.go. Built-from is narrower than READS: the closing cross-check
// resolves the categories slice through inTreeCategoryDecls — the walk behind
// inTreeCategories — which reads every non-test .go file in dir, so a sibling file's
// fault (an unparseable merge.go, a slice element no Category* constant declares)
// surfaces from this function too, wrapped in categoriesSliceWrap.
// The constant named CategoryOutOfScope is excluded by NAME on both sides — the
// walk below skips that name, and the slice cross-check exempts the element that
// carries it — so a change to the value it holds is followed automatically and a
// rename is an error rather than a silent fold. outOfScopeConstName states the
// rule and why it is the only exemption; this docstring does not restate it.
func inTreeCategoryBlocks(dir string) ([][]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(dir, "category.go"), nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse category.go: %w", err)
	}

	// Block members carry their constant NAME alongside the value: the forward
	// cross-check below is keyed on identity, because a value-keyed check cannot
	// tell a listed member from an unlisted alias holding the same string.
	var blocks [][]categoryElem

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
					blocks[len(blocks)-1] = append(blocks[len(blocks)-1], categoryElem{name: ident.Name, value: value})
				}
			}
		}
	}

	// Drop blocks a comment opened that hold no category (a note between
	// groups), so an empty run never reaches a caller as a real block.
	out := make([][]categoryElem, 0, len(blocks))
	for _, b := range blocks {
		if len(b) > 0 {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no comment-marked Category* blocks declared in %s", dir)
	}

	// Cross-check the partition against the categories slice. The two doc guards
	// read different authorities — inTreeCategoryDecls resolves the slice, this
	// function walks the const blocks — so a constant that reaches a block but
	// never the slice leaves them mutually unsatisfiable, both blaming the doc
	// table. Fail here instead, naming the slice as the side at fault.
	_, vocabulary, err := inTreeCategoryDecls(dir)
	if err != nil {
		// inTreeCategoryDecls walks the whole directory, so its faults — an
		// unparseable sibling file, a missing or empty `categories` slice, an
		// element no Category* constant declares — would otherwise reach the
		// caller indistinguishable from a category.go block fault, out of a
		// function documented as reading one file.
		return nil, fmt.Errorf("%s: %w", categoriesSliceWrap, err)
	}

	// The rename tripwire (see outOfScopeConstName). The exemption below is keyed
	// on the anchor NAME, so the routing value reaching the slice under any other
	// name — or as a bare literal — defeats that exclusion. What happens without
	// this error depends on where the value is declared: in a comment-marked block
	// of category.go it folds into the partition with nothing failing; elsewhere it
	// surfaces through a different cross-check's message instead, one that does not
	// name the rename as the fault. Fire before either can happen. Keying the
	// tripwire on the name too means a stale alias of the anchor does not disarm
	// it, which gating on the anchor's mere presence would have allowed.
	for _, elem := range vocabulary {
		if elem.value != outOfScopeConstValue || elem.name == outOfScopeConstName {
			continue
		}
		under, remedy := elem.name, fmt.Sprintf("restore the name %s", outOfScopeConstName)
		if under == "" {
			under, remedy = "a bare string literal", fmt.Sprintf("replace the literal with the %s constant", outOfScopeConstName)
		}
		return nil, fmt.Errorf("the routing value %q reaches the categories slice of %s as %s, not as %s — the block partition excludes that value by anchor NAME, so a renamed constant declared in a comment-marked block of category.go would fold into the partition with nothing failing; %s, or re-anchor %s in this helper", outOfScopeConstValue, dir, under, outOfScopeConstName, remedy, outOfScopeConstName)
	}

	// The forward cross-check is keyed on constant IDENTITY, not value: an alias
	// holding a listed value under a name the slice never lists would otherwise
	// fold into the partition with nothing failing — the walk's name skip misses
	// it, the tripwire above never sees it (it is not in the slice), and a
	// value-keyed check passes it on the listed member's value.
	listedByName := make(map[string]bool, len(vocabulary))
	listedByValue := make(map[string]bool, len(vocabulary))
	for _, elem := range vocabulary {
		listedByName[elem.name] = true
		listedByValue[elem.value] = true
	}
	for _, b := range out {
		for _, member := range b {
			if listedByName[member.name] {
				continue
			}
			if listedByValue[member.value] {
				return nil, fmt.Errorf("%s declares the value %q in a comment-marked block of %s, but the categories slice lists that value under a different constant — a block member must be listed under its own name, or the name-keyed exclusion folds it in silently", member.name, member.value, dir)
			}
			return nil, fmt.Errorf("%q is declared in a comment-marked block of %s but absent from the categories slice", member.value, dir)
		}
	}

	// The reverse direction: a slice member that reaches no block never gets its
	// Group cell checked, because this function is the block authority and it
	// reads category.go alone while inTreeCategoryDecls walks the whole
	// directory. The constant named by outOfScopeConstName is exempt — see there
	// for the rule and why it is the only one — and its Group cell is pinned
	// separately.
	inBlock := make(map[string]bool, len(out))
	for _, b := range out {
		for _, member := range b {
			inBlock[member.value] = true
		}
	}
	for _, elem := range vocabulary {
		if elem.name == outOfScopeConstName {
			continue
		}
		if !inBlock[elem.value] {
			return nil, fmt.Errorf("%q is listed in the categories slice of %s but appears in no comment-marked block of category.go — declare it there inside a PARENTHESIZED const block opened by a leading comment (the walk descends into parenthesized const blocks only, so an unparenthesized `const CategoryX = ...` opens no block however it is commented), or remove it from the slice; %s is the only exemption, and blocks in other files do not count (the partition is built from category.go alone)", elem.value, dir, outOfScopeConstName)
		}
	}

	// The caller wants values only; the names rode along for the identity-keyed
	// cross-check above.
	partition := make([][]string, 0, len(out))
	for _, b := range out {
		values := make([]string, 0, len(b))
		for _, member := range b {
			values = append(values, member.value)
		}
		partition = append(partition, values)
	}
	return partition, nil
}
