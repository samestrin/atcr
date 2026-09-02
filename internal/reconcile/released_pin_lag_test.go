package reconcile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reclibModulePath is the import path of the extracted reconcile library. The
// root module PINS a released version of it in go.mod; go.work substitutes the
// in-tree ./reconcile directory for local development.
const reclibModulePath = "github.com/samestrin/atcr/reconcile"

// repoRootFromHere is this package's path back to the root module. The guards
// below scan the whole root module, not just this package: internal/benchmark
// imports the library too, so a lag caught only here would be a partial answer.
const repoRootFromHere = "../.."

// docShieldConstName is the name the reconcile module declares the doc-shield
// routing reason under. unresolvedReasonDocShield (validate.go) is a local copy
// held while the pin lags behind the in-tree declaration, and the copy is only
// safe while the two agree — that is what TestDocShieldLocalCopy_MatchesInTree
// enforces.
const docShieldConstName = "UnresolvedReasonDocShield"

// TestReleasedReconcileModule_ProvidesEverySymbolTheRootModuleReferences is the
// release-lag tripwire for the whole reconcile surface, not just its category
// vocabulary.
//
// The root module builds against the reconcile version go.mod pins. go.work
// masks that locally by substituting the in-tree ./reconcile, so a symbol added
// in-tree and referenced from the root module compiles for the author and fails
// for everyone else — CI sets GOWORK=off for the entire Go job, and
// .githooks/pre-push runs `GOWORK=off go build ./...`, so such a branch can
// neither be pushed nor pass CI.
//
// TestReconcileModule_ReleasedVocabularyMatchesInTree covers only Categories(),
// and only warns. That is correct for the vocabulary — the doc tracks the in-tree
// list and a release window is an expected, temporary state — but it leaves every
// other exported name unguarded, which is how `reclib.UnresolvedReasonDocShield`
// reached the branch. This guard FAILS rather than skips: an unresolvable symbol
// is not a documentation lag, it is a build the branch cannot pass.
func TestReleasedReconcileModule_ProvidesEverySymbolTheRootModuleReferences(t *testing.T) {
	released := exportedTopLevelNames(t, releasedReconcileDir(t))
	require.NotEmpty(t, released, "the released reconcile module declares no exported names — the parse, not the pin, is at fault")

	referenced := reclibSelectorsInRootModule(t)
	require.NotEmpty(t, referenced, "the root module references no reconcile symbols — the scan, not the pin, is at fault")

	var missing []string
	for name, sites := range referenced {
		if _, ok := released[name]; ok {
			continue
		}
		missing = append(missing, name+" ("+strings.Join(sites, ", ")+")")
	}
	sort.Strings(missing)

	assert.Empty(t, missing,
		"the root module references %d reconcile symbol(s) the PINNED module does not export.\n"+
			"  go.work masks this locally; `GOWORK=off go build ./...` is what CI and .githooks/pre-push run.\n"+
			"  Fix by releasing the reconcile module and bumping the go.mod pin (docs/release-process.md),\n"+
			"  or by not depending on the unreleased symbol until the pin moves.\n"+
			"  missing: %s",
		len(missing), strings.Join(missing, "; "))
}

// TestDocShieldLocalCopy_MatchesInTree keeps the local stand-in honest.
//
// unresolvedReasonDocShield exists only because the pin lags; it is a duplicate
// of a value the published module owns, and "doc_shield" is a WIRE value — it is
// written into reconciled/unresolved.json and documented as a public artifact
// field. A silent divergence between the copy and the declaration would emit a
// reason no consumer recognizes, which is worse than the build break the copy
// was introduced to fix.
func TestDocShieldLocalCopy_MatchesInTree(t *testing.T) {
	inTree := stringConstValue(t, filepath.Join(repoRootFromHere, "reconcile"), docShieldConstName)
	assert.Equal(t, inTree, unresolvedReasonDocShield,
		"unresolvedReasonDocShield must equal reconcile.%s. When the go.mod pin catches up, delete the local copy and use reclib.%s directly.",
		docShieldConstName, docShieldConstName)
}

// releasedReconcileDir resolves the source directory of the reconcile version
// go.mod PINS. GOWORK=off is what makes the answer the pinned module rather than
// the in-tree one go.work substitutes — without it every guard here is vacuous
// in local development, which is exactly the blind spot they exist to close.
func releasedReconcileDir(t *testing.T) string {
	t.Helper()

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", reclibModulePath)
	cmd.Dir = repoRootFromHere
	cmd.Env = append(os.Environ(), "GOWORK=off")

	out, err := cmd.Output()
	require.NoError(t, err, "resolving the pinned %s module (GOWORK=off go list -m)", reclibModulePath)

	dir := strings.TrimSpace(string(out))
	require.NotEmpty(t, dir, "the pinned %s module resolved to an empty directory", reclibModulePath)
	require.NotEqual(t, mustAbs(t, filepath.Join(repoRootFromHere, "reconcile")), mustAbs(t, dir),
		"the pinned module resolved to the IN-TREE reconcile directory — GOWORK=off did not take effect, so this guard would pass unconditionally")
	return dir
}

// exportedTopLevelNames collects every exported top-level declaration (const,
// var, type, func) in dir's non-test Go files. That is the surface another module
// can reference, which is precisely what a lag check needs to compare against.
func exportedTopLevelNames(t *testing.T, dir string) map[string]struct{} {
	t.Helper()

	names := map[string]struct{}{}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "reading %s", dir)

	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", e.Name())

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() {
					names[d.Name.Name] = struct{}{}
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							names[s.Name.Name] = struct{}{}
						}
					case *ast.ValueSpec:
						for _, id := range s.Names {
							if id.IsExported() {
								names[id.Name] = struct{}{}
							}
						}
					}
				}
			}
		}
	}
	return names
}

// reclibSelectorsInRootModule maps each reconcile symbol the root module names to
// the files that name it. Test files are included deliberately: CI runs `go test`
// with GOWORK=off too, so a test-only reference breaks the build just as a
// production one does.
func reclibSelectorsInRootModule(t *testing.T) map[string][]string {
	t.Helper()

	root := mustAbs(t, repoRootFromHere)
	inTreeModule := mustAbs(t, filepath.Join(repoRootFromHere, "reconcile"))

	found := map[string][]string{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// ./reconcile is the library's OWN module — it does not build against
			// the pin — and the rest are not Go source the root module compiles.
			if path == inTreeModule || strings.HasPrefix(d.Name(), ".") || d.Name() == "bin" || d.Name() == "testdata" {
				if path != root {
					return fs.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return nil // not compiled by the root module either; nothing to guard
		}
		alias, ok := importAlias(file, reclibModulePath)
		if !ok {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != alias {
				return true
			}
			found[sel.Sel.Name] = appendUnique(found[sel.Sel.Name], rel)
			return true
		})
		return nil
	})
	require.NoError(t, err, "walking the root module")
	return found
}

// importAlias returns the local name file binds importPath to, resolving the
// default (the package name, which for this module is its last path element).
func importAlias(file *ast.File, importPath string) (string, bool) {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != importPath {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				return "", false
			}
			return imp.Name.Name, true
		}
		return filepath.Base(importPath), true
	}
	return "", false
}

// stringConstValue reads the value of a top-level string constant declared in
// dir's non-test Go files. It reads the SOURCE rather than importing the package
// so the answer is the in-tree declaration regardless of which module version
// the ambient build resolves.
func stringConstValue(t *testing.T, dir, name string) string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "reading %s", dir)

	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", e.Name())

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
				for i, id := range vs.Names {
					if id.Name != name || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					require.True(t, ok, "%s in %s is not a literal constant", name, dir)
					v, err := strconv.Unquote(lit.Value)
					require.NoError(t, err, "unquoting %s", name)
					return v
				}
			}
		}
	}
	t.Fatalf("no top-level string constant %s declared in %s", name, dir)
	return ""
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	require.NoError(t, err, "resolving %s", path)
	return abs
}

func appendUnique(list []string, v string) []string {
	for _, existing := range list {
		if existing == v {
			return list
		}
	}
	return append(list, v)
}
