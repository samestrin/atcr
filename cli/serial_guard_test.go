package cli

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The package's test seams are its function-valued package vars (debateRun in
// review.go, verifyRun in verify.go, debtGetwdFn in debt.go and the rest), which
// tests swap for a stub. The swaps are unsynchronized: they are safe only
// because Go starts a parallel test after every serial test of its level has
// finished and restored the seam, so a test that swaps one must never call
// t.Parallel. The seam set is derived from the package's non-test files, not
// listed, so a new or renamed seam is guarded without an edit here.

// seamViolation is a function that both swaps a seam and reaches t.Parallel.
type seamViolation struct {
	fn  string
	pos token.Position
}

// importDirFunc resolves an import path to the directory holding its source.
type importDirFunc func(importPath string) (string, error)

// varDecl is one package-level var: its declared type and initializer (either
// may be nil) and the file declaring it, whose imports qualify both.
type varDecl struct {
	typ, value ast.Expr
	file       *ast.File
}

// typeDecl is one package-level type and the file declaring it.
type typeDecl struct {
	typ  ast.Expr
	file *ast.File
}

// declIndex is the top-level declarations of one package's non-test files.
type declIndex struct {
	name    string
	funcs   map[string]bool // functions without a receiver
	methods map[string]bool // method names, on any receiver
	vars    map[string]varDecl
	types   map[string]typeDecl
	order   []string // var names in declaration order
}

func indexDecls(files []*ast.File) *declIndex {
	idx := &declIndex{
		funcs: map[string]bool{}, methods: map[string]bool{},
		vars: map[string]varDecl{}, types: map[string]typeDecl{},
	}
	for _, f := range files {
		idx.name = f.Name.Name
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					idx.funcs[d.Name.Name] = true
				} else {
					idx.methods[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						idx.types[s.Name.Name] = typeDecl{s.Type, f}
					case *ast.ValueSpec:
						if d.Tok != token.VAR {
							continue
						}
						for i, id := range s.Names {
							v := varDecl{typ: s.Type, file: f}
							if i < len(s.Values) {
								v.value = s.Values[i]
							}
							if _, seen := idx.vars[id.Name]; !seen && id.Name != "_" {
								idx.order = append(idx.order, id.Name)
							}
							idx.vars[id.Name] = v
						}
					}
				}
			}
		}
	}
	return idx
}

// funcValueResolver decides whether a package-level var holds a function. The
// guard does not type-check imports, so it follows an identifier or an
// imported selector (os.Getwd, verify.Verify) into the declaring package's
// source instead.
type funcValueResolver struct {
	importDir importDirFunc
	pkgs      map[string]*declIndex // by import path
}

// maxFuncValueDepth bounds how many declarations one var's value is followed
// through, so a cycle such as var a = b; var b = a ends.
const maxFuncValueDepth = 8

func (r *funcValueResolver) pkg(importPath string) (*declIndex, error) {
	if idx, ok := r.pkgs[importPath]; ok {
		return idx, nil
	}
	dir, err := r.importDir(importPath)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	files, err := parsePackageDir(fset, dir, false)
	if err != nil {
		return nil, err
	}
	idx := indexDecls(files)
	r.pkgs[importPath] = idx
	return idx, nil
}

// importOf returns the path of the import that file names local, if any.
func (r *funcValueResolver) importOf(file *ast.File, local string) (string, bool) {
	var unnamed []string
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == local {
				return p, true
			}
			continue
		}
		if defaultImportName(p) == local {
			return p, true
		}
		unnamed = append(unnamed, p)
	}
	// A package whose name is not its last path element.
	for _, p := range unnamed {
		if idx, err := r.pkg(p); err == nil && idx.name == local {
			return p, true
		}
	}
	return "", false
}

var majorVersionElem = regexp.MustCompile(`^v[0-9]+$`)

// defaultImportName is the conventional package name for an import path: its
// last element, skipping a /vN major-version suffix.
func defaultImportName(importPath string) string {
	elem := path.Base(importPath)
	if dir := path.Dir(importPath); majorVersionElem.MatchString(elem) && dir != "." {
		elem = path.Base(dir)
	}
	return elem
}

// funcVar reports whether idx declares name as a function-valued var.
func (r *funcValueResolver) funcVar(idx *declIndex, name string, depth int) (bool, error) {
	v, ok := idx.vars[name]
	if !ok {
		return false, nil
	}
	if v.typ != nil {
		return r.funcType(idx, v.file, v.typ, depth)
	}
	if v.value == nil {
		return false, nil
	}
	return r.funcExpr(idx, v.file, v.value, depth)
}

// funcName reports whether name, declared in idx, is a function or a
// function-valued var.
func (r *funcValueResolver) funcName(idx *declIndex, name string, depth int) (bool, error) {
	if idx.funcs[name] {
		return true, nil
	}
	return r.funcVar(idx, name, depth)
}

// funcExpr reports whether the initializer e, in file of package idx, is a
// function value: a literal, a function, a function-valued var, or a method
// value or expression.
func (r *funcValueResolver) funcExpr(idx *declIndex, file *ast.File, e ast.Expr, depth int) (bool, error) {
	if depth > maxFuncValueDepth {
		return false, nil
	}
	switch e := e.(type) {
	case *ast.FuncLit:
		return true, nil
	case *ast.ParenExpr:
		return r.funcExpr(idx, file, e.X, depth)
	case *ast.Ident:
		return r.funcName(idx, e.Name, depth+1)
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			if p, ok := r.importOf(file, x.Name); ok {
				dep, err := r.pkg(p)
				if err != nil {
					return false, fmt.Errorf("classify %s.%s: %w", x.Name, e.Sel.Name, err)
				}
				return r.funcName(dep, e.Sel.Name, depth+1)
			}
		}
		return idx.methods[e.Sel.Name], nil
	}
	return false, nil
}

// funcType reports whether the type expression e, in file of package idx, is
// a function type, directly or through a named type.
func (r *funcValueResolver) funcType(idx *declIndex, file *ast.File, e ast.Expr, depth int) (bool, error) {
	if depth > maxFuncValueDepth {
		return false, nil
	}
	switch e := e.(type) {
	case *ast.FuncType:
		return true, nil
	case *ast.ParenExpr:
		return r.funcType(idx, file, e.X, depth)
	case *ast.Ident:
		if td, ok := idx.types[e.Name]; ok {
			return r.funcType(idx, td.file, td.typ, depth+1)
		}
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			if p, ok := r.importOf(file, x.Name); ok {
				dep, err := r.pkg(p)
				if err != nil {
					return false, fmt.Errorf("classify type %s.%s: %w", x.Name, e.Sel.Name, err)
				}
				if td, ok := dep.types[e.Sel.Name]; ok {
					return r.funcType(dep, td.file, td.typ, depth+1)
				}
			}
		}
	}
	return false, nil
}

// isTestFile reports whether f was parsed from a _test.go file.
func isTestFile(fset *token.FileSet, f *ast.File) bool {
	return strings.HasSuffix(fset.Position(f.Package).Filename, "_test.go")
}

// packageSeams returns the names of the function-valued package-level vars
// declared in the non-test files among files.
func packageSeams(fset *token.FileSet, files []*ast.File, importDir importDirFunc) (map[string]bool, error) {
	var src []*ast.File
	for _, f := range files {
		if !isTestFile(fset, f) {
			src = append(src, f)
		}
	}
	idx := indexDecls(src)
	r := &funcValueResolver{importDir: importDir, pkgs: map[string]*declIndex{}}
	seams := map[string]bool{}
	for _, name := range idx.order {
		ok, err := r.funcVar(idx, name, 0)
		if err != nil {
			return nil, fmt.Errorf("seam %s: %w", name, err)
		}
		if ok {
			seams[name] = true
		}
	}
	return seams, nil
}

// noImports fails every import, so the guard type-checks one package's own
// source without loading its dependencies.
type noImports struct{}

func (noImports) Import(p string) (*types.Package, error) {
	return nil, fmt.Errorf("guard does not load imports (%s)", p)
}

// origin maps an instantiated generic function or var to its declaration.
func origin(obj types.Object) types.Object {
	switch o := obj.(type) {
	case *types.Func:
		return o.Origin()
	case *types.Var:
		return o.Origin()
	}
	return obj
}

// receiverName is the base type name of a method receiver.
func receiverName(recv *ast.FieldList) string {
	e := recv.List[0].Type
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return "?"
		}
	}
}

// parallelSeamViolations returns, sorted by name, one entry per function of
// files that both writes a seam and reaches a t.Parallel call. The seams are
// the function-valued package vars of the non-test files. The functions are
// the package's top-level functions and methods and its function-valued
// package vars, each keyed by its declaration, so a method and a function
// sharing a writer's name stay distinct. A write is an assignment to a seam
// or taking its address; a function writes a seam if its body (nested
// function literals included) does or names a function that does, and reaches
// t.Parallel if it calls it or names a function that does, each found to a
// fixed point.
func parallelSeamViolations(fset *token.FileSet, files []*ast.File, importDir importDirFunc) ([]seamViolation, error) {
	if len(files) == 0 {
		return nil, nil
	}
	seamNames, err := packageSeams(fset, files, importDir)
	if err != nil {
		return nil, err
	}

	// Type-check without imports: identifiers still resolve to the package's
	// own declarations, including a method through its receiver's type, and
	// the errors the missing imports cause are expected.
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: noImports{}, Error: func(error) {}}
	pkg, _ := conf.Check(files[0].Name.Name, fset, files, info)

	seams := map[types.Object]bool{}
	for name := range seamNames {
		if obj, ok := pkg.Scope().Lookup(name).(*types.Var); ok {
			seams[obj] = true
		}
	}

	// event is a t.Parallel call (ref nil) or a use of another function.
	type event struct {
		pos token.Pos
		ref types.Object
	}
	type fn struct {
		name   string
		body   ast.Node
		writes bool
		events []event
	}
	fns := map[types.Object]*fn{}
	var objs []types.Object
	add := func(id *ast.Ident, name string, body ast.Node) {
		if obj := info.Defs[id]; obj != nil && fns[obj] == nil {
			fns[obj] = &fn{name: name, body: body}
			objs = append(objs, obj)
		}
	}
	for _, f := range files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				name := d.Name.Name
				if d.Recv != nil && len(d.Recv.List) > 0 {
					name = receiverName(d.Recv) + "." + name
				}
				add(d.Name, name, d.Body)
			case *ast.GenDecl:
				if d.Tok != token.VAR {
					continue
				}
				for _, spec := range d.Specs {
					vs := spec.(*ast.ValueSpec)
					for i, id := range vs.Names {
						if i >= len(vs.Values) {
							continue
						}
						_, lit := vs.Values[i].(*ast.FuncLit)
						obj := info.Defs[id]
						if obj == nil {
							continue
						}
						if _, sig := obj.Type().Underlying().(*types.Signature); lit || sig {
							add(id, id.Name, vs.Values[i])
						}
					}
				}
			}
		}
	}

	isSeam := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && seams[info.Uses[id]]
	}
	for _, obj := range objs {
		f := fns[obj]
		ast.Inspect(f.body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					if isSeam(lhs) {
						f.writes = true
					}
				}
			case *ast.UnaryExpr:
				if n.Op == token.AND && isSeam(n.X) {
					f.writes = true
				}
			case *ast.CallExpr:
				if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Parallel" && len(n.Args) == 0 {
					f.events = append(f.events, event{pos: n.Pos()})
				}
			case *ast.Ident:
				if used := info.Uses[n]; used != nil && fns[origin(used)] != nil {
					f.events = append(f.events, event{n.Pos(), origin(used)})
				}
			}
			return true
		})
	}

	writers, reachers := map[types.Object]bool{}, map[types.Object]bool{}
	for _, obj := range objs {
		writers[obj] = fns[obj].writes
	}
	for changed := true; changed; {
		changed = false
		for _, obj := range objs {
			for _, ev := range fns[obj].events {
				if !writers[obj] && ev.ref != nil && writers[ev.ref] {
					writers[obj], changed = true, true
				}
				if !reachers[obj] && (ev.ref == nil || reachers[ev.ref]) {
					reachers[obj], changed = true, true
				}
			}
		}
	}

	var out []seamViolation
	for _, obj := range objs {
		if !writers[obj] || !reachers[obj] {
			continue
		}
		f := fns[obj]
		for _, ev := range f.events {
			if ev.ref == nil || reachers[ev.ref] {
				out = append(out, seamViolation{f.name, fset.Position(ev.pos)})
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].fn < out[j].fn })
	return out, nil
}

// TestPackageSeamSwapsStaySerial keeps the precondition every seam-swapping test
// in this package relies on: none of them runs in parallel.
func TestPackageSeamSwapsStaySerial(t *testing.T) {
	fset, files, dir := guardPackageFiles(t)
	seams, err := packageSeams(fset, files, moduleImportDir(t, dir))
	require.NoError(t, err)
	require.NotEmpty(t, seams, "no function-valued package var found in %s", dir)

	violations, err := parallelSeamViolations(fset, files, moduleImportDir(t, dir))
	require.NoError(t, err)
	for _, v := range violations {
		t.Errorf("%s (%s): a test that swaps a package seam (a function-valued package var such as debateRun, "+
			"directly or through a helper) reaches t.Parallel, so it races every other test that reads or swaps the seam",
			v.pos, v.fn)
	}
}

// guardPackageDir is the directory holding this file. It comes from the file's
// own source position, not the working directory, which tests here move.
func guardPackageDir(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller")
	require.True(t, filepath.IsAbs(self), "source path %q is not absolute (built with -trimpath?)", self)
	return filepath.Dir(self)
}

// parsePackageDir parses the .go files of dir that build for this platform,
// skipping _test.go files unless withTests.
func parsePackageDir(fset *token.FileSet, dir string, withTests bool) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || (!withTests && strings.HasSuffix(name, "_test.go")) {
			continue
		}
		if ok, err := build.Default.MatchFile(dir, name); err != nil || !ok {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// guardPackageFiles parses this package's files, test and non-test, leaving
// out the external cli_test package, and returns them with the directory.
func guardPackageFiles(t *testing.T) (*token.FileSet, []*ast.File, string) {
	t.Helper()
	dir := guardPackageDir(t)
	fset := token.NewFileSet()
	all, err := parsePackageDir(fset, dir, true)
	require.NoError(t, err)
	var self *ast.File
	for _, f := range all {
		if filepath.Base(fset.Position(f.Package).Filename) == "serial_guard_test.go" {
			self = f
		}
	}
	require.NotNil(t, self, "serial_guard_test.go not found in %s", dir)
	var files []*ast.File
	for _, f := range all {
		if f.Name.Name == self.Name.Name {
			files = append(files, f)
		}
	}
	return fset, files, dir
}

// moduleImportDir resolves an import path to its source directory: this
// module's packages under the module root above dir, and the standard library
// under GOROOT. Any other import cannot be resolved, so a seam initialized
// from one fails the guard rather than being skipped.
func moduleImportDir(t *testing.T, dir string) importDirFunc {
	t.Helper()
	root := dir
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		require.NotEqual(t, root, parent, "no go.mod above %s", dir)
		root = parent
	}
	mod := goModulePath(t, filepath.Join(root, "go.mod"))
	return func(p string) (string, error) {
		if p == mod || strings.HasPrefix(p, mod+"/") {
			return filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(p, mod))), nil
		}
		if first, _, _ := strings.Cut(p, "/"); !strings.Contains(first, ".") {
			std := filepath.Join(build.Default.GOROOT, "src", filepath.FromSlash(p))
			if fi, err := os.Stat(std); err == nil && fi.IsDir() {
				return std, nil
			}
		}
		return "", fmt.Errorf("import %q is neither in module %s nor the standard library", p, mod)
	}
}

// goModulePath reads the module directive of a go.mod file.
func goModulePath(t *testing.T, goMod string) string {
	t.Helper()
	data, err := os.ReadFile(goMod)
	require.NoError(t, err)
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	t.Fatalf("no module directive in %s", goMod)
	return ""
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
	got := fixtureViolations(t, map[string]string{"seams.go": fixtureSeams, "fixture_test.go": src})
	assert.Equal(t, []string{
		"TestDirect", "TestParallelSubtest", "TestSubtestValue", "TestViaAddress",
		"TestViaHelper", "TestViaMethod", "TestViaNestedHelper",
	}, got)
}

// fixtureViolations runs the guard over fixture sources keyed by file name and
// returns the reported function names.
func fixtureViolations(t *testing.T, srcs map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for name, src := range srcs {
		file, err := parser.ParseFile(fset, name, src, 0)
		require.NoError(t, err)
		files = append(files, file)
	}
	violations, err := parallelSeamViolations(fset, files, moduleImportDir(t, guardPackageDir(t)))
	require.NoError(t, err)
	var got []string
	for _, v := range violations {
		got = append(got, v.fn)
	}
	return got
}

// fixtureSeams declares the fixture's seams the way package cli does: as
// function-valued package vars in a non-test file.
const fixtureSeams = `package cli

var debateRun = func() error { return nil }

var verifyRun = func() error { return nil }

var debtGetwdFn = func() (string, error) { return "", nil }
`

// TestParallelSeamViolations_HelperShapes proves the guard follows a seam write
// through a function-valued package var, and a t.Parallel call through a
// helper (a plain function or a function-valued var), in both directions.
func TestParallelSeamViolations_HelperShapes(t *testing.T) {
	const src = `package cli

var swapDebateVar = func() { debateRun = nil }

var swapViaVarAlias = stubVerify

func stubVerify(t *testing.T) { verifyRun = nil }

func parallelize(t *testing.T) { t.Parallel() }

func parallelizeTwice(t *testing.T) { parallelize(t) }

var parallelizeVar = func(t *testing.T) { t.Parallel() }

func TestViaFuncVar(t *testing.T) {
	t.Parallel()
	swapDebateVar()
}

func TestViaFuncVarAlias(t *testing.T) {
	t.Parallel()
	swapViaVarAlias(t)
}

func TestViaParallelHelper(t *testing.T) {
	parallelize(t)
	stubVerify(t)
}

func TestViaNestedParallelHelper(t *testing.T) {
	debtGetwdFn = nil
	parallelizeTwice(t)
}

func TestViaParallelFuncVar(t *testing.T) {
	parallelizeVar(t)
	debateRun = nil
}

func TestParallelHelperOnly(t *testing.T) {
	parallelize(t)
	_ = verifyRun
}

func TestSerialFuncVar(t *testing.T) {
	swapDebateVar()
}
`
	assert.Equal(t, []string{
		"TestViaFuncVar", "TestViaFuncVarAlias", "TestViaNestedParallelHelper",
		"TestViaParallelFuncVar", "TestViaParallelHelper",
	}, fixtureViolations(t, map[string]string{"seams.go": fixtureSeams, "fixture_test.go": src}))
}

// TestParallelSeamViolations_SameNameHelpers proves writers are keyed by their
// receiver-qualified identity: a method or function that shares a writer's
// name but writes no seam does not make its parallel caller a violation.
func TestParallelSeamViolations_SameNameHelpers(t *testing.T) {
	const src = `package cli

type seamFixture struct{}

func (seamFixture) stubDebate() { debateRun = nil }

type quietFixture struct{}

func (quietFixture) stubDebate() {}

func stubDebate() {}

func TestWriterMethod(t *testing.T) {
	t.Parallel()
	seamFixture{}.stubDebate()
}

func TestWriterMethodViaVar(t *testing.T) {
	t.Parallel()
	var f seamFixture
	f.stubDebate()
}

func TestQuietMethod(t *testing.T) {
	t.Parallel()
	quietFixture{}.stubDebate()
}

func TestQuietMethodViaPointer(t *testing.T) {
	t.Parallel()
	q := &quietFixture{}
	q.stubDebate()
}

func TestQuietFunc(t *testing.T) {
	t.Parallel()
	stubDebate()
}
`
	assert.Equal(t, []string{"TestWriterMethod", "TestWriterMethodViaVar"},
		fixtureViolations(t, map[string]string{"seams.go": fixtureSeams, "fixture_test.go": src}))
}

// TestGuardPackageFiles_IgnoresWorkingDirectory proves the guard reads this
// package's own directory even when a test has moved the working directory
// somewhere holding other _test.go files.
func TestGuardPackageFiles_IgnoresWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "decoy_test.go"), []byte("package decoy\n"), 0o600))
	t.Chdir(dir)

	fset, files, _ := guardPackageFiles(t)
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(fset.Position(f.Package).Filename))
	}
	assert.Contains(t, names, "serial_guard_test.go")
	assert.NotContains(t, names, "decoy_test.go")
}

// TestPackageSeams_Fixture proves the seam set is every function-valued
// package var of the non-test files, whatever shape its declaration takes,
// and nothing else.
func TestPackageSeams_Fixture(t *testing.T) {
	const src = `package cli

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	debatepkg "github.com/samestrin/atcr/internal/debate"
	"github.com/samestrin/atcr/internal/verify"
)

type clockFn func() time.Time

type server struct{}

func (server) Serve() error { return nil }

func localHelper() error { return nil }

var (
	litSeam             = func() error { return nil }
	localFuncSeam       = localHelper
	aliasSeam           = litSeam
	stdFuncSeam         = os.Getwd
	moduleFuncSeam      = verify.Verify
	renamedImportSeam   = debatepkg.Debate
	declaredSeam        func() error
	namedTypeSeam       clockFn
	importedNamedSeam   filepath.WalkFunc
	methodValueSeam     = server{}.Serve
	methodExprSeam      = server.Serve
	parenSeam           = (localHelper)
	stdVar              = os.Stdout
	stdConst            = filepath.Separator
	errVar              = errors.New("x")
	callResult          = time.Now()
	mapVar              = map[string]bool{}
	intVar              = 3
	typedInt        int = 4
	_                   = localHelper
)
`
	const testSrc = `package cli

var testOnlyFuncVar = func() {}
`
	fset := token.NewFileSet()
	var files []*ast.File
	for name, s := range map[string]string{"seams.go": src, "seams_test.go": testSrc} {
		file, err := parser.ParseFile(fset, name, s, 0)
		require.NoError(t, err)
		files = append(files, file)
	}
	seams, err := packageSeams(fset, files, moduleImportDir(t, guardPackageDir(t)))
	require.NoError(t, err)
	var got []string
	for name := range seams {
		got = append(got, name)
	}
	sort.Strings(got)
	assert.Equal(t, []string{
		"aliasSeam", "declaredSeam", "importedNamedSeam", "litSeam", "localFuncSeam", "methodExprSeam",
		"methodValueSeam", "moduleFuncSeam", "namedTypeSeam", "parenSeam", "renamedImportSeam", "stdFuncSeam",
	}, got)
}

// TestPackageSeams_UnresolvableImport proves a seam initialized from a
// package the guard cannot read fails the guard instead of being skipped.
func TestPackageSeams_UnresolvableImport(t *testing.T) {
	const src = `package cli

import "example.com/elsewhere"

var remoteSeam = elsewhere.Run
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "seams.go", src, 0)
	require.NoError(t, err)
	_, err = packageSeams(fset, []*ast.File{file}, moduleImportDir(t, guardPackageDir(t)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remoteSeam")
	assert.Contains(t, err.Error(), "example.com/elsewhere")
}
